package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cypek019/cypek.dev---panel/internal/audit"
	"github.com/cypek019/cypek.dev---panel/internal/scheduler"
)

const (
	CodeInvalidCron     = "invalid_cron"
	CodeScheduleRunning = "schedule_running"
)

func (s *Server) scheduleFromRequest(w http.ResponseWriter, r *http.Request) (*scheduler.Schedule, bool) {
	srv := serverFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "sid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "Schedule not found.")
		return nil, false
	}
	sch, err := s.schedules.Get(r.Context(), id)
	if errors.Is(err, scheduler.ErrNotFound) || (err == nil && sch.ServerID != srv.ID) {
		writeError(w, http.StatusNotFound, CodeNotFound, "Schedule not found.")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return sch, true
}

func (s *Server) writeScheduleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, scheduler.ErrInvalidCron):
		writeFieldError(w, CodeInvalidCron, "cron", err.Error())
	case errors.Is(err, scheduler.ErrInvalidName):
		writeFieldError(w, CodeValidation, "name", "Name must be 1–80 characters.")
	case errors.Is(err, scheduler.ErrNotFound):
		writeError(w, http.StatusNotFound, CodeNotFound, "Schedule not found.")
	default:
		// Validation errors from the chain are user-facing messages.
		writeFieldError(w, CodeValidation, "actions", err.Error())
	}
}

type scheduleResponse struct {
	*scheduler.Schedule
	Running bool `json:"running"`
}

func (s *Server) scheduleResp(sch *scheduler.Schedule) scheduleResponse {
	return scheduleResponse{Schedule: sch, Running: s.scheduleEngine.IsRunning(sch.ID)}
}

func (s *Server) handleListSchedules(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	list, err := s.schedules.List(r.Context(), srv.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	items := make([]scheduleResponse, 0, len(list))
	for _, sch := range list {
		items = append(items, s.scheduleResp(sch))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "timezone": s.scheduleEngine.Location().String()})
}

type scheduleInput struct {
	Name          *string             `json:"name"`
	Cron          *string             `json:"cron"`
	Enabled       *bool               `json:"enabled"`
	Actions       *[]scheduler.Action `json:"actions"`
	OnlyIfRunning *bool               `json:"onlyIfRunning"`
	SkipIfPlayers *bool               `json:"skipIfPlayers"`
}

func (in *scheduleInput) apply(sch *scheduler.Schedule) {
	if in.Name != nil {
		sch.Name = *in.Name
	}
	if in.Cron != nil {
		sch.CronExpr = *in.Cron
	}
	if in.Enabled != nil {
		sch.Enabled = *in.Enabled
	}
	if in.Actions != nil {
		sch.Actions = *in.Actions
	}
	if in.OnlyIfRunning != nil {
		sch.OnlyIfRunning = *in.OnlyIfRunning
	}
	if in.SkipIfPlayers != nil {
		sch.SkipIfPlayers = *in.SkipIfPlayers
	}
}

func (s *Server) handleCreateSchedule(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	var in scheduleInput
	if !decodeJSON(w, r, &in) {
		return
	}
	sch := &scheduler.Schedule{ServerID: srv.ID, Enabled: true, OnlyIfRunning: true, CreatedBy: &user.ID}
	in.apply(sch)
	created, err := s.schedules.Create(r.Context(), sch)
	if err != nil {
		s.writeScheduleError(w, r, err)
		return
	}
	s.scheduleEngine.Reload(r.Context(), created.ID)
	created, _ = s.schedules.Get(r.Context(), created.ID)
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "schedule.create", Target: srv.ID, IP: clientIP(r), Details: map[string]any{"name": created.Name, "cron": created.CronExpr}})
	writeJSON(w, http.StatusCreated, s.scheduleResp(created))
}

func (s *Server) handleUpdateSchedule(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	sch, ok := s.scheduleFromRequest(w, r)
	if !ok {
		return
	}
	var in scheduleInput
	if !decodeJSON(w, r, &in) {
		return
	}
	in.apply(sch)
	if err := s.schedules.Update(r.Context(), sch); err != nil {
		s.writeScheduleError(w, r, err)
		return
	}
	s.scheduleEngine.Reload(r.Context(), sch.ID)
	sch, _ = s.schedules.Get(r.Context(), sch.ID)
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "schedule.update", Target: sch.ServerID, IP: clientIP(r), Details: map[string]any{"name": sch.Name}})
	writeJSON(w, http.StatusOK, s.scheduleResp(sch))
}

func (s *Server) handleDeleteSchedule(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	sch, ok := s.scheduleFromRequest(w, r)
	if !ok {
		return
	}
	s.scheduleEngine.Cancel(sch.ID)
	if err := s.schedules.Delete(r.Context(), sch.ID); err != nil {
		s.writeScheduleError(w, r, err)
		return
	}
	s.scheduleEngine.Reload(r.Context(), sch.ID)
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "schedule.delete", Target: sch.ServerID, IP: clientIP(r), Details: map[string]any{"name": sch.Name}})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRunSchedule(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	sch, ok := s.scheduleFromRequest(w, r)
	if !ok {
		return
	}
	if err := s.scheduleEngine.RunNow(sch.ID); err != nil {
		writeError(w, http.StatusConflict, CodeScheduleRunning, "This schedule is already running.")
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "schedule.run", Target: sch.ServerID, IP: clientIP(r), Details: map[string]any{"name": sch.Name}})
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handleCancelSchedule(w http.ResponseWriter, r *http.Request) {
	sch, ok := s.scheduleFromRequest(w, r)
	if !ok {
		return
	}
	if !s.scheduleEngine.Cancel(sch.ID) {
		writeError(w, http.StatusConflict, CodeConflict, "This schedule is not running.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleScheduleRuns(w http.ResponseWriter, r *http.Request) {
	sch, ok := s.scheduleFromRequest(w, r)
	if !ok {
		return
	}
	runs, err := s.schedules.Runs(r.Context(), sch.ID, 50)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

// handleCronPreview returns the next fire times for an expression, so the
// UI can show "next: Tuesday 04:00" while the user types.
func (s *Server) handleCronPreview(w http.ResponseWriter, r *http.Request) {
	expr := r.URL.Query().Get("expr")
	loc := s.scheduleEngine.Location()
	next, err := scheduler.NextRuns(expr, loc, 5)
	if err != nil {
		writeFieldError(w, CodeInvalidCron, "cron", "This is not a valid cron expression (minute hour day month weekday).")
		return
	}
	out := make([]string, 0, len(next))
	for _, t := range next {
		out = append(out, t.Format(time.RFC3339))
	}
	writeJSON(w, http.StatusOK, map[string]any{"next": out, "timezone": loc.String()})
}
