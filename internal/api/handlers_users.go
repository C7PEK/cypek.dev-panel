package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/auth"
)

const (
	CodeOwnerImmutable = "owner_immutable"
	CodeSelfAction     = "self_action"
	CodeWrongPassword  = "wrong_password"
)

// Who may touch whom: the owner may manage everyone but cannot be changed;
// admins manage users only; nobody edits their own role or disables themselves.
func canManage(actor, target *auth.User) bool {
	if target.Role == auth.RoleOwner {
		return false
	}
	if actor.Role == auth.RoleOwner {
		return true
	}
	return actor.Role == auth.RoleAdmin && target.Role == auth.RoleUser
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

type createUserRequest struct {
	Username string    `json:"username"`
	Password string    `json:"password"`
	Role     auth.Role `json:"role"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	_, actor := sessionFromContext(r.Context())
	var req createUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Role == "" {
		req.Role = auth.RoleUser
	}
	if req.Role == auth.RoleOwner || !req.Role.Valid() {
		writeFieldError(w, CodeValidation, "role", "Role must be admin or user.")
		return
	}
	if req.Role == auth.RoleAdmin && actor.Role != auth.RoleOwner {
		writeError(w, http.StatusForbidden, CodeForbidden, "Only the owner can create administrators.")
		return
	}
	u, err := s.store.CreateUser(r.Context(), strings.TrimSpace(req.Username), req.Password, req.Role)
	if err != nil {
		if !writeAuthError(w, err) {
			s.internalError(w, r, err)
		}
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &actor.ID, Username: actor.Username, Action: "user.create", Target: u.Username, IP: clientIP(r), Details: map[string]any{"role": u.Role}})
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) userFromRequest(w http.ResponseWriter, r *http.Request) (*auth.User, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, CodeNotFound, "User not found.")
		return nil, false
	}
	u, err := s.store.GetUserByID(r.Context(), id)
	if errors.Is(err, auth.ErrNotFound) {
		writeError(w, http.StatusNotFound, CodeNotFound, "User not found.")
		return nil, false
	}
	if err != nil {
		s.internalError(w, r, err)
		return nil, false
	}
	return u, true
}

type updateUserRequest struct {
	Role     *auth.Role `json:"role"`
	Disabled *bool      `json:"disabled"`
	Password *string    `json:"password"`
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	_, actor := sessionFromContext(r.Context())
	target, ok := s.userFromRequest(w, r)
	if !ok {
		return
	}
	var req updateUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if target.Role == auth.RoleOwner {
		writeError(w, http.StatusForbidden, CodeOwnerImmutable, "The owner account cannot be changed here.")
		return
	}
	if target.ID == actor.ID && (req.Role != nil || req.Disabled != nil) {
		writeError(w, http.StatusForbidden, CodeSelfAction, "You cannot change your own role or disable yourself.")
		return
	}
	if !canManage(actor, target) && target.ID != actor.ID {
		writeError(w, http.StatusForbidden, CodeForbidden, "You cannot manage this account.")
		return
	}
	details := map[string]any{}
	if req.Role != nil {
		if *req.Role == auth.RoleOwner || !req.Role.Valid() {
			writeFieldError(w, CodeValidation, "role", "Role must be admin or user.")
			return
		}
		if *req.Role == auth.RoleAdmin && actor.Role != auth.RoleOwner {
			writeError(w, http.StatusForbidden, CodeForbidden, "Only the owner can grant the administrator role.")
			return
		}
		if err := s.store.SetRole(r.Context(), target.ID, *req.Role); err != nil {
			s.internalError(w, r, err)
			return
		}
		details["role"] = *req.Role
	}
	if req.Disabled != nil {
		if err := s.store.SetDisabled(r.Context(), target.ID, *req.Disabled); err != nil {
			s.internalError(w, r, err)
			return
		}
		details["disabled"] = *req.Disabled
	}
	if req.Password != nil {
		if err := s.store.SetPassword(r.Context(), target.ID, *req.Password); err != nil {
			if !writeAuthError(w, err) {
				s.internalError(w, r, err)
			}
			return
		}
		details["password"] = "changed"
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &actor.ID, Username: actor.Username, Action: "user.update", Target: target.Username, IP: clientIP(r), Details: details})
	updated, _ := s.store.GetUserByID(r.Context(), target.ID)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	_, actor := sessionFromContext(r.Context())
	target, ok := s.userFromRequest(w, r)
	if !ok {
		return
	}
	if target.Role == auth.RoleOwner {
		writeError(w, http.StatusForbidden, CodeOwnerImmutable, "The owner account cannot be deleted.")
		return
	}
	if target.ID == actor.ID {
		writeError(w, http.StatusForbidden, CodeSelfAction, "You cannot delete your own account.")
		return
	}
	if !canManage(actor, target) {
		writeError(w, http.StatusForbidden, CodeForbidden, "You cannot manage this account.")
		return
	}
	if err := s.store.DeleteUser(r.Context(), target.ID); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &actor.ID, Username: actor.Username, Action: "user.delete", Target: target.Username, IP: clientIP(r)})
	w.WriteHeader(http.StatusNoContent)
}

type changePasswordRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

// handleChangePassword lets any user rotate their own password; other
// sessions are logged out, the current one is re-issued.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if ok, err := auth.VerifyPassword(user.PasswordHash, req.Current); err != nil || !ok {
		writeFieldError(w, CodeWrongPassword, "current", "The current password is not correct.")
		return
	}
	if err := s.store.SetPassword(r.Context(), user.ID, req.New); err != nil {
		if !writeAuthError(w, err) {
			s.internalError(w, r, err)
		}
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &user.ID, Username: user.Username, Action: "user.password", Target: user.Username, IP: clientIP(r)})
	s.startSession(w, r, user, "auth.login")
}

// --- per-server permissions ----------------------------------------------

type serverUserGrant struct {
	User        *auth.User        `json:"user"`
	Permissions []auth.Permission `json:"permissions"`
}

func (s *Server) handleListServerUsers(w http.ResponseWriter, r *http.Request) {
	srv := serverFromContext(r.Context())
	grants, err := s.store.ServerGrants(r.Context(), srv.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	out := make([]serverUserGrant, 0, len(grants))
	for uid, perms := range grants {
		u, err := s.store.GetUserByID(r.Context(), uid)
		if err != nil {
			continue
		}
		out = append(out, serverUserGrant{User: u, Permissions: perms})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "all": auth.AllPermissions})
}

type grantRequest struct {
	Permissions []auth.Permission `json:"permissions"`
}

func (s *Server) handleSetServerUser(w http.ResponseWriter, r *http.Request) {
	_, actor := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	target, ok := s.userFromRequest(w, r)
	if !ok {
		return
	}
	if target.Role != auth.RoleUser {
		writeError(w, http.StatusConflict, CodeConflict, "Administrators already have access to every server; grants apply to users only.")
		return
	}
	var req grantRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	for _, p := range req.Permissions {
		if !p.Valid() {
			writeFieldError(w, CodeValidation, "permissions", "Unknown permission: "+string(p))
			return
		}
	}
	if err := s.store.GrantPermissions(r.Context(), srv.ID, target.ID, req.Permissions); err != nil {
		s.internalError(w, r, err)
		return
	}
	s.audit.Log(r.Context(), audit.Entry{UserID: &actor.ID, Username: actor.Username, Action: "permissions.update", Target: srv.ID, IP: clientIP(r), Details: map[string]any{"user": target.Username, "permissions": req.Permissions}})
	if len(req.Permissions) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, serverUserGrant{User: target, Permissions: req.Permissions})
}
