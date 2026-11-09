package api

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/alerts"
	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/metrics"
	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

const (
	CodeNoWebhook     = "no_webhook"
	CodeWebhookFailed = "webhook_failed"
)

// startAlerts runs the alert monitor on every metrics tick.
func (s *Server) startAlerts() {
	if s.alertStore == nil || s.metrics == nil {
		return
	}
	if s.alertSender == nil {
		s.alertSender = alerts.NewDiscord()
	}
	s.alertMon = &alerts.Monitor{
		Store:  s.alertStore,
		Sender: s.alertSender,
		Log:    s.log,
		Servers: func(ctx context.Context) ([]alerts.ServerInfo, error) {
			list, err := s.servers.List(ctx, nil)
			if err != nil {
				return nil, err
			}
			out := make([]alerts.ServerInfo, 0, len(list))
			for _, srv := range list {
				out = append(out, alerts.ServerInfo{
					ID: srv.ID, Name: srv.Name, Dir: s.servers.Dir(srv.ID),
					CleanExitIsDown: srv.Category == servers.CategoryDiscord || srv.Category == servers.CategoryOther,
				})
			}
			return out, nil
		},
	}
	var busy atomic.Bool
	s.metrics.Subscribe(func(_ metrics.Host, samples map[string]metrics.Sample) {
		if !busy.CompareAndSwap(false, true) {
			return // previous round still running
		}
		states := make(map[string]supervisor.State, len(samples))
		for id, sm := range samples {
			states[id] = sm.State
		}
		go func() {
			defer busy.Store(false)
			ctx, cancel := context.WithTimeout(s.bgCtx, 30*time.Second)
			defer cancel()
			s.alertMon.Tick(ctx, states)
		}()
	})
}

type alertSettingsView struct {
	alerts.Settings
	WebhookSet  bool   `json:"webhookSet"`
	WebhookHint string `json:"webhookHint"`
}

func viewSettings(st alerts.Settings) alertSettingsView {
	return alertSettingsView{Settings: st, WebhookSet: st.Webhook != "", WebhookHint: alerts.WebhookHint(st.Webhook)}
}

func (s *Server) alertsReady(w http.ResponseWriter) bool {
	if s.alertStore == nil || s.alertMon == nil {
		writeError(w, http.StatusServiceUnavailable, CodeInternal, "Alerts are not available.")
		return false
	}
	return true
}

func writeAlertFieldError(w http.ResponseWriter, err error) {
	var fe *alerts.FieldError
	if errors.As(err, &fe) {
		writeFieldError(w, CodeValidation, fe.Field, fe.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, CodeInternal, err.Error())
}

func (s *Server) handleGetAlerts(w http.ResponseWriter, r *http.Request) {
	if !s.alertsReady(w) {
		return
	}
	st, err := s.alertStore.Settings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": viewSettings(st), "history": s.alertMon.History()})
}

type alertSettingsInput struct {
	Webhook           *string `json:"webhook"`
	Mention           string  `json:"mention"`
	GraceSec          int     `json:"graceSec"`
	NotifyRecovered   bool    `json:"notifyRecovered"`
	NotifyAutoRestart bool    `json:"notifyAutoRestart"`
	NotifyErrors      bool    `json:"notifyErrors"`
	IncludeLogs       bool    `json:"includeLogs"`
	Language          string  `json:"language"`
	PanelURL          string  `json:"panelUrl"`
}

func (s *Server) handlePutAlerts(w http.ResponseWriter, r *http.Request) {
	if !s.alertsReady(w) {
		return
	}
	var in alertSettingsInput
	if !decodeJSON(w, r, &in) {
		return
	}
	cur, err := s.alertStore.Settings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	st := alerts.Settings{Webhook: cur.Webhook, Mention: in.Mention, GraceSec: in.GraceSec, NotifyRecovered: in.NotifyRecovered,
		NotifyAutoRestart: in.NotifyAutoRestart, NotifyErrors: in.NotifyErrors, IncludeLogs: in.IncludeLogs, Language: in.Language, PanelURL: in.PanelURL}
	if in.Webhook != nil {
		st.Webhook = *in.Webhook
	}
	if err := s.alertStore.SaveSettings(r.Context(), st); err != nil {
		writeAlertFieldError(w, err)
		return
	}
	saved, _ := s.alertStore.Settings(r.Context())
	_, user := sessionFromContext(r.Context())
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "alerts.update", Target: "global", IP: clientIP(r),
		Details: map[string]any{"webhookChanged": in.Webhook != nil, "graceSec": saved.GraceSec}})
	writeJSON(w, http.StatusOK, map[string]any{"settings": viewSettings(saved)})
}

func (s *Server) sendTest(w http.ResponseWriter, r *http.Request, webhook, mention string, srv *alerts.ServerInfo) {
	if webhook == "" {
		writeError(w, http.StatusConflict, CodeNoWebhook, "No Discord webhook is set; save one first.")
		return
	}
	st, err := s.alertStore.Settings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	if err := s.alertMon.SendTest(ctx, webhook, mention, st, srv); err != nil {
		writeError(w, http.StatusBadGateway, CodeWebhookFailed, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTestAlerts(w http.ResponseWriter, r *http.Request) {
	if !s.alertsReady(w) {
		return
	}
	st, err := s.alertStore.Settings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	s.sendTest(w, r, st.Webhook, st.Mention, nil)
}

type serverAlertsView struct {
	Enabled          bool   `json:"enabled"`
	WebhookSet       bool   `json:"webhookSet"`
	WebhookHint      string `json:"webhookHint"`
	Mention          string `json:"mention"`
	GlobalWebhookSet bool   `json:"globalWebhookSet"`
}

func (s *Server) serverAlertsView(ctx context.Context, ss alerts.ServerSettings) serverAlertsView {
	g, _ := s.alertStore.Settings(ctx)
	return serverAlertsView{Enabled: ss.Enabled, WebhookSet: ss.Webhook != "", WebhookHint: alerts.WebhookHint(ss.Webhook), Mention: ss.Mention, GlobalWebhookSet: g.Webhook != ""}
}

func (s *Server) handleGetServerAlerts(w http.ResponseWriter, r *http.Request) {
	if !s.alertsReady(w) {
		return
	}
	srv := serverFromContext(r.Context())
	ss, err := s.alertStore.Server(r.Context(), srv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.serverAlertsView(r.Context(), ss))
}

func (s *Server) handlePutServerAlerts(w http.ResponseWriter, r *http.Request) {
	if !s.alertsReady(w) {
		return
	}
	srv := serverFromContext(r.Context())
	var in struct {
		Enabled bool    `json:"enabled"`
		Webhook *string `json:"webhook"`
		Mention string  `json:"mention"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	cur, err := s.alertStore.Server(r.Context(), srv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	next := alerts.ServerSettings{Enabled: in.Enabled, Webhook: cur.Webhook, Mention: in.Mention}
	if in.Webhook != nil {
		next.Webhook = *in.Webhook
	}
	saved, err := s.alertStore.SaveServer(r.Context(), srv.ID, next)
	if err != nil {
		writeAlertFieldError(w, err)
		return
	}
	_, user := sessionFromContext(r.Context())
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "alerts.update", Target: srv.ID, IP: clientIP(r),
		Details: map[string]any{"enabled": saved.Enabled, "webhookChanged": in.Webhook != nil}})
	writeJSON(w, http.StatusOK, s.serverAlertsView(r.Context(), saved))
}

func (s *Server) handleTestServerAlerts(w http.ResponseWriter, r *http.Request) {
	if !s.alertsReady(w) {
		return
	}
	srv := serverFromContext(r.Context())
	ss, err := s.alertStore.Server(r.Context(), srv.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, CodeInternal, err.Error())
		return
	}
	g, _ := s.alertStore.Settings(r.Context())
	webhook, mention := ss.Webhook, ss.Mention
	if webhook == "" {
		webhook = g.Webhook
	}
	if mention == "" {
		mention = g.Mention
	}
	s.sendTest(w, r, webhook, mention, &alerts.ServerInfo{ID: srv.ID, Name: srv.Name})
}
