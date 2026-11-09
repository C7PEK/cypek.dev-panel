package alerts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/db"
	"github.com/C7PEK/cypek.dev-panel/internal/secrets"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

const hook = "https://discord.com/api/webhooks/123456789012345678/abcdefghijklmnopqrstuvwxyz_ABCDEF-1234"

func TestValidateWebhook(t *testing.T) {
	good := []string{hook, strings.Replace(hook, "discord.com", "discordapp.com", 1), strings.Replace(hook, "discord.com", "canary.discord.com", 1), hook + "?thread_id=123456789012"}
	for _, g := range good {
		if _, err := ValidateWebhook(g); err != nil {
			t.Errorf("%s: %v", g, err)
		}
	}
	bad := []string{"", "http://discord.com/api/webhooks/1234567/abcdefghijklmnopqrstuvwxyz", strings.Replace(hook, "discord.com", "evil.com", 1),
		strings.Replace(hook, "discord.com", "discord.com.evil.com", 1), "https://user@discord.com/api/webhooks/123456/abcdefghijklmnopqrstuvwxyz",
		"https://discord.com/api/users/@me", hook + "/../../x", hook + "?thread_id=abc"}
	for _, b := range bad {
		if _, err := ValidateWebhook(b); err == nil {
			t.Errorf("%q accepted", b)
		}
	}
	if h := WebhookHint(hook); h != "…/1234" {
		t.Errorf("hint %q", h)
	}
}

func TestParseMention(t *testing.T) {
	m, err := ParseMention(" @everyone <@&111111111111> 222222222222 <@!333333333333> ")
	if err != nil || m.Text != "@everyone <@&111111111111> <@222222222222> <@333333333333>" || !m.Everyone || len(m.Roles) != 1 || len(m.Users) != 2 {
		t.Fatalf("%+v %v", m, err)
	}
	for _, bad := range []string{"@admins", "<#123456789>", "hello", "<@&abc>"} {
		if _, err := ParseMention(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

type fakeSender struct {
	mu   sync.Mutex
	msgs []Message
	hook []string
	fail error
}

func (f *fakeSender) Send(_ context.Context, webhook string, msg Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg)
	f.hook = append(f.hook, webhook)
	return f.fail
}

func (f *fakeSender) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.msgs) }

type harness struct {
	t      *testing.T
	m      *Monitor
	sender *fakeSender
	now    time.Time
	events map[string][]shim.Event
	srv    ServerInfo
	store  *Store
}

func newHarness(t *testing.T) *harness {
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	if err := db.Migrate(context.Background(), sqlDB, "", slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	box, _ := secrets.New(bytes.Repeat([]byte{7}, 32))
	h := &harness{t: t, sender: &fakeSender{}, now: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC), events: map[string][]shim.Event{}, store: NewStore(sqlDB, box)}
	insertServer(t, sqlDB, "bot1")
	h.srv = ServerInfo{ID: "bot1", Name: "Music *Bot*", Dir: dir, CleanExitIsDown: true}
	st := DefaultSettings()
	st.Webhook, st.Mention = hook, "<@&111111111111>"
	if err := h.store.SaveSettings(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	h.m = &Monitor{Store: h.store, Sender: h.sender, Now: func() time.Time { return h.now },
		Servers:    func(context.Context) ([]ServerInfo, error) { return []ServerInfo{h.srv}, nil },
		ReadEvents: func(dir string, limit int) ([]shim.Event, error) { return h.events[dir], nil }}
	return h
}

func insertServer(t *testing.T, sqlDB *sql.DB, id string) {
	_, err := sqlDB.Exec(`INSERT INTO servers (id, name, category, template_id, run_config, created_at, updated_at) VALUES (?, ?, 'discord-bot', 'custom', '{}', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`, id, id)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
}

func (h *harness) event(kind string, code int) {
	c := code
	h.events[h.srv.Dir] = append(h.events[h.srv.Dir], shim.Event{At: h.now.Format(time.RFC3339Nano), Kind: kind, ExitCode: &c})
}

// tick advances the clock and waits for queued deliveries.
func (h *harness) tick(d time.Duration, state supervisor.State) {
	h.now = h.now.Add(d)
	h.m.Tick(context.Background(), map[string]supervisor.State{h.srv.ID: state})
	time.Sleep(20 * time.Millisecond)
}

func TestCrashStaysDownThenRecovers(t *testing.T) {
	h := newHarness(t)
	h.tick(0, supervisor.StateRunning) // first sight: history is skipped
	h.event("crash", 1)
	h.tick(3*time.Second, supervisor.StateCrashed)
	h.tick(10*time.Second, supervisor.StateCrashed)
	if h.sender.count() != 0 {
		t.Fatalf("alert before grace: %d", h.sender.count())
	}
	h.tick(10*time.Second, supervisor.StateCrashed)
	if h.sender.count() != 1 {
		t.Fatalf("expected down alert, got %d", h.sender.count())
	}
	down := h.sender.msgs[0]
	if !strings.HasPrefix(down.Content, "<@&111111111111> 🔴 **Music \\*Bot\\***") || len(down.AllowedMentions.Roles) != 1 || down.Embeds[0].Color != colorRed {
		t.Errorf("down message: %+v", down)
	}
	h.tick(30*time.Second, supervisor.StateCrashed)
	if h.sender.count() != 1 {
		t.Fatal("down alert repeated")
	}
	h.tick(5*time.Second, supervisor.StateRunning)
	h.tick(31*time.Second, supervisor.StateRunning)
	if h.sender.count() != 2 || h.sender.msgs[1].Embeds[0].Color != colorGreen || h.sender.msgs[1].Content != "" {
		t.Fatalf("recovered: %+v", h.sender.msgs)
	}
	if hist := h.m.History(); len(hist) != 2 || hist[0].Kind != KindRecovered || !hist[0].OK {
		t.Errorf("history: %+v", hist)
	}
}

func TestCrashAutoRestartIsQuiet(t *testing.T) {
	h := newHarness(t)
	h.tick(0, supervisor.StateRunning)
	h.event("crash", 137)
	h.tick(3*time.Second, supervisor.StateStopped)
	h.tick(5*time.Second, supervisor.StateRunning)
	h.tick(25*time.Second, supervisor.StateRunning) // past grace, alive
	if h.sender.count() != 0 {
		t.Fatal("alerted although running")
	}
	h.tick(10*time.Second, supervisor.StateRunning)
	if h.sender.count() != 1 || h.sender.msgs[0].Embeds[0].Color != colorYellow || len(h.sender.msgs[0].AllowedMentions.Roles) != 0 {
		t.Fatalf("restarted note: %+v", h.sender.msgs)
	}
}

func TestCrashLoopAlertsEvenWhileStarting(t *testing.T) {
	h := newHarness(t)
	h.tick(0, supervisor.StateRunning)
	for i := 0; i < 3; i++ {
		h.event("crash", 1)
		h.tick(6*time.Second, supervisor.StateStarting)
	}
	h.tick(21*time.Second, supervisor.StateStarting)
	if h.sender.count() != 1 || !strings.Contains(h.sender.msgs[0].Embeds[0].Description, "3") {
		t.Fatalf("crash loop: %+v", h.sender.msgs)
	}
}

func TestStopAndConsoleExitAreNotAlerts(t *testing.T) {
	h := newHarness(t)
	h.tick(0, supervisor.StateRunning)
	h.event("stop", 0)
	h.tick(60*time.Second, supervisor.StateStopped)
	// A game server's "stop" typed in the console exits with 0.
	h.srv.CleanExitIsDown = false
	h.event("exit", 0)
	h.tick(60*time.Second, supervisor.StateStopped)
	if h.sender.count() != 0 {
		t.Fatalf("unexpected alerts: %+v", h.sender.msgs)
	}
	// For a bot the same clean exit means it is down.
	h.srv.CleanExitIsDown = true
	h.event("exit", 0)
	h.tick(25*time.Second, supervisor.StateStopped)
	if h.sender.count() != 1 {
		t.Fatalf("bot exit 0: %d", h.sender.count())
	}
}

func TestCatchUpAfterPanelRestartAndServerOverrides(t *testing.T) {
	h := newHarness(t)
	h.event("crash", 1) // happened before the panel (re)started
	h.tick(25*time.Second, supervisor.StateCrashed)
	if h.sender.count() != 1 {
		t.Fatalf("catch-up: %d", h.sender.count())
	}
	// Disabled server: nothing.
	h2 := newHarness(t)
	if _, err := h2.store.SaveServer(context.Background(), "bot1", ServerSettings{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	h2.tick(0, supervisor.StateRunning)
	h2.event("crash", 1)
	h2.tick(25*time.Second, supervisor.StateCrashed)
	if h2.sender.count() != 0 {
		t.Fatal("disabled server alerted")
	}
	// Own webhook + mention win over the globals.
	other := strings.Replace(hook, "123456789012345678", "999999999999999999", 1)
	if _, err := h2.store.SaveServer(context.Background(), "bot1", ServerSettings{Enabled: true, Webhook: other, Mention: "@here"}); err != nil {
		t.Fatal(err)
	}
	h2.event("crash", 1)
	h2.tick(25*time.Second, supervisor.StateCrashed)
	if h2.sender.count() != 1 || h2.sender.hook[0] != other || !strings.HasPrefix(h2.sender.msgs[0].Content, "@here") || h2.sender.msgs[0].AllowedMentions.Parse[0] != "everyone" {
		t.Fatalf("override: %+v %v", h2.sender.msgs, h2.sender.hook)
	}
	// Webhooks are stored sealed.
	var raw string
	h2.store.db.QueryRow(`SELECT webhook FROM server_alerts WHERE server_id = 'bot1'`).Scan(&raw)
	h2.store.db.QueryRow(`SELECT value FROM settings WHERE key = 'alerts'`).Scan(&raw)
	if strings.Contains(raw, "abcdefghijklmnopqrstuvwxyz") {
		t.Error("webhook stored in plaintext")
	}
}

func TestDiscordSender(t *testing.T) {
	var calls int
	var got Message
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("wait") != "true" {
			t.Error("wait=true missing")
		}
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"retry_after": 0.05}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	d := &Discord{Client: srv.Client()}
	// Point the validated discord.com URL at the test server.
	d.Client.Transport = rewriteTransport{base: d.Client.Transport, target: srv.URL}
	msg := Message{Content: "hi"}
	msg.withMention(Mention{})
	if err := d.Send(context.Background(), hook, msg); err != nil || calls != 2 || got.Username != "panel.cypek.dev" || got.AllowedMentions.Parse == nil {
		t.Fatalf("send: %v calls=%d got=%+v", err, calls, got)
	}
	if err := d.Send(context.Background(), "https://example.com/x", msg); !errors.Is(err, ErrWebhook) {
		t.Errorf("non-discord URL: %v", err)
	}
}

type rewriteTransport struct {
	base   http.RoundTripper
	target string
}

func (r rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	t, _ := http.NewRequest(req.Method, r.target, nil)
	u.Scheme, u.Host = t.URL.Scheme, t.URL.Host
	req2 := req.Clone(req.Context())
	req2.URL = &u
	req2.Host = u.Host
	return r.base.RoundTrip(req2)
}

func TestCodeBlockLimits(t *testing.T) {
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, strings.Repeat("x", 30)+"```")
	}
	out := codeBlock(lines)
	if len(out) > logChars+20 || strings.Count(out, "```") != 2 {
		t.Errorf("code block len %d", len(out))
	}
}
