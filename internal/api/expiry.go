package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/alerts"
	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/servers"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

const (
	CodeServerExpired = "server_expired"
	expiryWarnBefore  = 72 * time.Hour
	maxExpiry         = 10 * 365 * 24 * time.Hour
)

// expiryView is what the API shows about a server's end date.
type expiryView struct {
	ExpiresAt string `json:"expiresAt,omitempty"`
	Expired   bool   `json:"expired"`
	ExpiredAt string `json:"expiredAt,omitempty"`
}

func viewExpiry(e *servers.Expiry) expiryView {
	if e == nil {
		return expiryView{}
	}
	v := expiryView{ExpiresAt: e.ExpiresAt.UTC().Format(time.RFC3339), Expired: !e.ExpiredAt.IsZero()}
	if v.Expired {
		v.ExpiredAt = e.ExpiredAt.UTC().Format(time.RFC3339)
	}
	return v
}

// runExpiryLoop stops and locks servers whose end date passed and sends
// the "expires soon" notice. It never deletes anything.
func (s *Server) runExpiryLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		s.checkExpiries(ctx, time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) checkExpiries(ctx context.Context, now time.Time) {
	list, err := s.servers.Expiries(ctx)
	if err != nil {
		s.log.Warn("expiry check", "err", err)
		return
	}
	for _, e := range list {
		if !e.ExpiredAt.IsZero() {
			continue
		}
		srv, err := s.servers.Get(ctx, e.ServerID)
		if err != nil {
			continue
		}
		info := alerts.ServerInfo{ID: srv.ID, Name: srv.Name, Dir: s.servers.Dir(srv.ID)}
		switch {
		case !now.Before(e.ExpiresAt):
			// Lock first so nothing can start it while it is being stopped.
			if err := s.servers.MarkExpired(ctx, srv.ID, now); err != nil {
				s.log.Warn("mark expired", "server", srv.ID, "err", err)
				continue
			}
			s.log.Info("server expired, stopping and locking it", "server", srv.ID, "name", srv.Name)
			s.audit.Log(ctx, audit.Entry{Username: "panel", Action: "server.expired", Target: srv.ID, Details: map[string]any{"name": srv.Name, "expiresAt": e.ExpiresAt.Format(time.RFC3339)}})
			go func(id string, timeout time.Duration) {
				sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
				defer cancel()
				if err := s.sup.Stop(sctx, id); err != nil && !errors.Is(err, supervisor.ErrNotRunning) {
					s.log.Warn("stop expired server", "server", id, "err", err)
				}
			}(srv.ID, time.Duration(srv.Run.Stop.TimeoutSec+30)*time.Second)
			if s.alertMon != nil {
				s.alertMon.Notify(ctx, info, alerts.KindExpired, e.ExpiresAt)
			}
		case e.WarnedAt.IsZero() && e.ExpiresAt.Sub(now) <= expiryWarnBefore:
			_ = s.servers.MarkWarned(ctx, srv.ID, now)
			if s.alertMon != nil {
				s.alertMon.Notify(ctx, info, alerts.KindExpiring, e.ExpiresAt)
			}
		}
	}
}

type expiryInput struct {
	// ExpiresAt sets an exact date (RFC 3339 or YYYY-MM-DD, end of that
	// day); null or "" removes the end date.
	ExpiresAt *string `json:"expiresAt"`
	// ExtendDays adds days to the current end date (or to now when it
	// already passed).
	ExtendDays int `json:"extendDays"`
}

func parseExpiry(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	if d, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		return d.Add(24*time.Hour - time.Second), nil
	}
	return time.Time{}, errors.New("use a date like 2026-12-31")
}

// handleSetExpiry (admin) sets, extends or removes a server's end date. A
// date in the future unlocks an expired server.
func (s *Server) handleSetExpiry(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	var in expiryInput
	if !decodeJSON(w, r, &in) {
		return
	}
	cur, err := s.servers.Expiry(r.Context(), srv.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	now := time.Now()
	var next *time.Time
	switch {
	case in.ExtendDays != 0:
		if in.ExtendDays < 1 || in.ExtendDays > 3650 {
			writeFieldError(w, CodeValidation, "extendDays", "Extend by 1 to 3650 days.")
			return
		}
		base := now
		if cur != nil && cur.ExpiresAt.After(now) {
			base = cur.ExpiresAt
		}
		t := base.Add(time.Duration(in.ExtendDays) * 24 * time.Hour)
		next = &t
	case in.ExpiresAt != nil && strings.TrimSpace(*in.ExpiresAt) != "":
		t, err := parseExpiry(*in.ExpiresAt)
		if err != nil {
			writeFieldError(w, CodeValidation, "expiresAt", err.Error())
			return
		}
		if !t.After(now) {
			writeFieldError(w, CodeValidation, "expiresAt", "The date must be in the future.")
			return
		}
		if t.Sub(now) > maxExpiry {
			writeFieldError(w, CodeValidation, "expiresAt", "At most 10 years ahead.")
			return
		}
		next = &t
	}
	if err := s.servers.SetExpiry(r.Context(), srv.ID, next); err != nil {
		s.internalError(w, r, err)
		return
	}
	_, user := sessionFromContext(r.Context())
	details := map[string]any{"cleared": next == nil}
	if next != nil {
		details["expiresAt"] = next.UTC().Format(time.RFC3339)
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "server.expiry", Target: srv.ID, IP: clientIP(r), Details: details})
	e, _ := s.servers.Expiry(r.Context(), srv.ID)
	writeJSON(w, http.StatusOK, viewExpiry(e))
}

func writeExpired(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, CodeServerExpired, "This server has expired. An admin has to extend it before it can start again.")
}
