// Package alerts sends Discord webhook notifications when a server crashes
// and does not come back, when it recovers, and (optionally) on console
// error bursts.
package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	webhookHost = regexp.MustCompile(`^((ptb|canary)\.)?discord(app)?\.com$`)
	webhookPath = regexp.MustCompile(`^/api(/v\d+)?/webhooks/(\d{5,25})/([A-Za-z0-9_-]{20,128})$`)
	digitsRe    = regexp.MustCompile(`^\d{5,25}$`)
)

// ErrWebhook is returned for anything that is not a Discord webhook URL.
var ErrWebhook = errors.New("this is not a Discord webhook link (https://discord.com/api/webhooks/...)")

// ValidateWebhook accepts only Discord webhook URLs, so the panel cannot be
// made to POST to arbitrary hosts.
func ValidateWebhook(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.User != nil || !webhookHost.MatchString(u.Host) || !webhookPath.MatchString(u.Path) {
		return "", ErrWebhook
	}
	q := url.Values{}
	if t := u.Query().Get("thread_id"); t != "" {
		if !digitsRe.MatchString(t) {
			return "", ErrWebhook
		}
		q.Set("thread_id", t)
	}
	u.RawQuery = q.Encode()
	u.Fragment = ""
	return u.String(), nil
}

// WebhookHint shows the end of the token, enough to recognise a webhook.
func WebhookHint(webhook string) string {
	if webhook == "" {
		return ""
	}
	u, err := url.Parse(webhook)
	if err != nil {
		return "…"
	}
	m := webhookPath.FindStringSubmatch(u.Path)
	if m == nil || len(m[3]) < 4 {
		return "…"
	}
	return "…/" + m[3][len(m[3])-4:]
}

// Mention is a validated ping target list.
type Mention struct {
	Text     string
	Everyone bool
	Roles    []string
	Users    []string
}

var (
	roleRe = regexp.MustCompile(`^<@&(\d{5,25})>$`)
	userRe = regexp.MustCompile(`^<@!?(\d{5,25})>$`)
)

// ParseMention accepts "@everyone", "@here", "<@&role>", "<@user>" or a bare
// user id, space separated; Text is the normalised form.
func ParseMention(s string) (Mention, error) {
	var m Mention
	fields := strings.Fields(s)
	if len(fields) > 10 {
		return m, errors.New("at most 10 mentions")
	}
	var out []string
	for _, f := range fields {
		switch {
		case f == "@everyone" || f == "@here":
			m.Everyone = true
			out = append(out, f)
		case roleRe.MatchString(f):
			id := roleRe.FindStringSubmatch(f)[1]
			m.Roles = append(m.Roles, id)
			out = append(out, "<@&"+id+">")
		case userRe.MatchString(f):
			id := userRe.FindStringSubmatch(f)[1]
			m.Users = append(m.Users, id)
			out = append(out, "<@"+id+">")
		case digitsRe.MatchString(f):
			m.Users = append(m.Users, f)
			out = append(out, "<@"+f+">")
		default:
			return Mention{}, fmt.Errorf("%q is not a mention; use @everyone, @here, <@&ROLE_ID> or <@USER_ID>", f)
		}
	}
	m.Text = strings.Join(out, " ")
	return m, nil
}

// Message is a Discord webhook payload.
type Message struct {
	Username        string          `json:"username,omitempty"`
	Content         string          `json:"content,omitempty"`
	Embeds          []Embed         `json:"embeds,omitempty"`
	AllowedMentions allowedMentions `json:"allowed_mentions"`
}

type allowedMentions struct {
	Parse []string `json:"parse"`
	Roles []string `json:"roles,omitempty"`
	Users []string `json:"users,omitempty"`
}

type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	URL         string       `json:"url,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type EmbedFooter struct {
	Text string `json:"text"`
}

// withMention puts the ping in content and allows exactly those mentions.
func (m *Message) withMention(men Mention) {
	m.AllowedMentions = allowedMentions{Parse: []string{}}
	if men.Text == "" {
		return
	}
	m.Content = strings.TrimSpace(men.Text + " " + m.Content)
	if men.Everyone {
		m.AllowedMentions.Parse = []string{"everyone"}
	}
	m.AllowedMentions.Roles = men.Roles
	m.AllowedMentions.Users = men.Users
}

// Sender delivers messages; tests replace it.
type Sender interface {
	Send(ctx context.Context, webhook string, msg Message) error
}

// Discord posts to webhooks, honouring rate limits.
type Discord struct{ Client *http.Client }

func NewDiscord() *Discord { return &Discord{Client: &http.Client{Timeout: 15 * time.Second}} }

func (d *Discord) Send(ctx context.Context, webhook string, msg Message) error {
	if _, err := ValidateWebhook(webhook); err != nil {
		return err
	}
	if msg.Username == "" {
		msg.Username = "panel.cypek.dev"
	}
	body, _ := json.Marshal(msg)
	target := webhook
	if strings.Contains(target, "?") {
		target += "&wait=true"
	} else {
		target += "?wait=true"
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "panel.cypek.dev alerts (+https://github.com/C7PEK/cypek.dev-panel)")
		resp, err := d.Client.Do(req)
		if err != nil {
			lastErr = err
			if !sleepCtx(ctx, time.Duration(attempt+1)*2*time.Second) {
				return ctx.Err()
			}
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return nil
		case resp.StatusCode == http.StatusTooManyRequests:
			var rl struct {
				RetryAfter float64 `json:"retry_after"`
			}
			_ = json.Unmarshal(raw, &rl)
			wait := time.Duration(rl.RetryAfter * float64(time.Second))
			if wait <= 0 || wait > 30*time.Second {
				wait = 5 * time.Second
			}
			lastErr = errors.New("Discord rate limit")
			if !sleepCtx(ctx, wait) {
				return ctx.Err()
			}
		case resp.StatusCode >= 500:
			lastErr = fmt.Errorf("Discord answered HTTP %d", resp.StatusCode)
			if !sleepCtx(ctx, time.Duration(attempt+1)*2*time.Second) {
				return ctx.Err()
			}
		default:
			var de struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(raw, &de)
			if de.Message == "" {
				de.Message = strings.TrimSpace(string(raw))
			}
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("Discord rejected the webhook (HTTP %d: %s); it was probably deleted, create a new one", resp.StatusCode, de.Message)
			}
			return fmt.Errorf("Discord rejected the message (HTTP %d: %s)", resp.StatusCode, de.Message)
		}
	}
	return lastErr
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
