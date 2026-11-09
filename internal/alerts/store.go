package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/C7PEK/cypek.dev-panel/internal/secrets"
)

const settingsKey = "alerts"

// Settings are the panel-wide alert options. Webhook is plaintext in
// memory and sealed in the database.
type Settings struct {
	Webhook           string `json:"-"`
	Mention           string `json:"mention"`
	GraceSec          int    `json:"graceSec"`
	NotifyRecovered   bool   `json:"notifyRecovered"`
	NotifyAutoRestart bool   `json:"notifyAutoRestart"`
	NotifyErrors      bool   `json:"notifyErrors"`
	IncludeLogs       bool   `json:"includeLogs"`
	Language          string `json:"language"`
	PanelURL          string `json:"panelUrl"`
}

// DefaultSettings: alert after 20 s, tell about recoveries.
func DefaultSettings() Settings {
	return Settings{GraceSec: 20, NotifyRecovered: true, NotifyAutoRestart: true, IncludeLogs: true, Language: "pl"}
}

// FieldError names the invalid field.
type FieldError struct {
	Field string
	Err   error
}

func (e *FieldError) Error() string { return e.Err.Error() }

// Validate normalises the settings (webhook, mention, URL).
func (s *Settings) Validate() error {
	if s.Webhook != "" {
		w, err := ValidateWebhook(s.Webhook)
		if err != nil {
			return &FieldError{"webhook", err}
		}
		s.Webhook = w
	}
	m, err := ParseMention(s.Mention)
	if err != nil {
		return &FieldError{"mention", err}
	}
	s.Mention = m.Text
	if s.GraceSec < 10 || s.GraceSec > 600 {
		return &FieldError{"graceSec", errors.New("the delay must be between 10 and 600 seconds")}
	}
	if s.Language != "en" {
		s.Language = "pl"
	}
	if s.PanelURL != "" {
		u, err := url.Parse(s.PanelURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return &FieldError{"panelUrl", errors.New("the panel address must start with http:// or https://")}
		}
		u.Path, u.RawQuery, u.Fragment = "", "", ""
		s.PanelURL = u.String()
	}
	return nil
}

// ServerSettings override the globals for one server.
type ServerSettings struct {
	Enabled bool
	Webhook string
	Mention string
}

// Store keeps the settings in SQLite.
type Store struct {
	db  *sql.DB
	box *secrets.Box
}

func NewStore(db *sql.DB, box *secrets.Box) *Store { return &Store{db: db, box: box} }

type storedSettings struct {
	Settings
	Webhook string `json:"webhook"`
}

func (s *Store) Settings(ctx context.Context) (Settings, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, settingsKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("read alert settings: %w", err)
	}
	st := storedSettings{Settings: DefaultSettings()}
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		return Settings{}, fmt.Errorf("decode alert settings: %w", err)
	}
	out := st.Settings
	if st.Webhook != "" {
		if out.Webhook, err = s.box.Open(st.Webhook); err != nil {
			return Settings{}, fmt.Errorf("decrypt alert webhook: %w", err)
		}
	}
	return out, nil
}

func (s *Store) SaveSettings(ctx context.Context, st Settings) error {
	if err := st.Validate(); err != nil {
		return err
	}
	stored := storedSettings{Settings: st}
	if st.Webhook != "" {
		sealed, err := s.box.Seal(st.Webhook)
		if err != nil {
			return err
		}
		stored.Webhook = sealed
	}
	b, _ := json.Marshal(stored)
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingsKey, string(b))
	if err != nil {
		return fmt.Errorf("save alert settings: %w", err)
	}
	return nil
}

// Server returns a server's overrides (enabled by default).
func (s *Store) Server(ctx context.Context, id string) (ServerSettings, error) {
	var (
		enabled          int
		webhook, mention string
	)
	err := s.db.QueryRowContext(ctx, `SELECT enabled, webhook, mention FROM server_alerts WHERE server_id = ?`, id).Scan(&enabled, &webhook, &mention)
	if errors.Is(err, sql.ErrNoRows) {
		return ServerSettings{Enabled: true}, nil
	}
	if err != nil {
		return ServerSettings{}, fmt.Errorf("read alerts of %s: %w", id, err)
	}
	out := ServerSettings{Enabled: enabled == 1, Mention: mention}
	if webhook != "" {
		if out.Webhook, err = s.box.Open(webhook); err != nil {
			return ServerSettings{}, fmt.Errorf("decrypt webhook of %s: %w", id, err)
		}
	}
	return out, nil
}

func (s *Store) SaveServer(ctx context.Context, id string, st ServerSettings) (ServerSettings, error) {
	if st.Webhook != "" {
		w, err := ValidateWebhook(st.Webhook)
		if err != nil {
			return st, &FieldError{"webhook", err}
		}
		st.Webhook = w
	}
	m, err := ParseMention(st.Mention)
	if err != nil {
		return st, &FieldError{"mention", err}
	}
	st.Mention = m.Text
	sealed := ""
	if st.Webhook != "" {
		if sealed, err = s.box.Seal(st.Webhook); err != nil {
			return st, err
		}
	}
	enabled := 0
	if st.Enabled {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO server_alerts (server_id, enabled, webhook, mention) VALUES (?, ?, ?, ?)
		ON CONFLICT(server_id) DO UPDATE SET enabled = excluded.enabled, webhook = excluded.webhook, mention = excluded.mention`, id, enabled, sealed, st.Mention)
	if err != nil {
		return st, fmt.Errorf("save alerts of %s: %w", id, err)
	}
	return st, nil
}
