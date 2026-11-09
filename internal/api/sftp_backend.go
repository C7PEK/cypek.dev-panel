package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/auth"
	"github.com/C7PEK/cypek.dev-panel/internal/sftpd"
)

// SFTPBackend gives the built-in SFTP server the panel's accounts,
// permissions, login rate limits and audit log.
func (s *Server) SFTPBackend() sftpd.Backend { return sftpBackend{s} }

type sftpBackend struct{ s *Server }

var errRateLimited = errors.New("too many login attempts")

func (b sftpBackend) Authenticate(ctx context.Context, username, password, ip string) (int64, string, error) {
	s := b.s
	if ok, _ := s.loginLimitIP.Allow(ip); !ok {
		return 0, "", errRateLimited
	}
	key := strings.ToLower(strings.TrimSpace(username))
	if ok, _ := s.loginLimitUser.Allow(key); !ok {
		return 0, "", errRateLimited
	}
	u, err := s.store.Authenticate(ctx, username, password)
	if err != nil {
		s.audit.Log(context.WithoutCancel(ctx), audit.Entry{Username: username, Action: "auth.sftp_login_failed", IP: ip})
		return 0, "", err
	}
	s.loginLimitIP.Reset(ip)
	s.loginLimitUser.Reset(key)
	return u.ID, u.Username, nil
}

func (b sftpBackend) user(ctx context.Context, id int64) (*auth.User, bool) {
	u, err := b.s.store.GetUserByID(ctx, id)
	if err != nil || u.Disabled {
		return nil, false
	}
	return u, true
}

func (b sftpBackend) Servers(ctx context.Context, userID int64) ([]sftpd.ServerRef, error) {
	u, ok := b.user(ctx, userID)
	if !ok {
		return nil, errors.New("account not available")
	}
	list, err := b.s.servers.List(ctx, nil)
	if err != nil {
		return nil, err
	}
	var out []sftpd.ServerRef
	for _, srv := range list {
		if allowed, err := b.s.store.Require(ctx, u, auth.PermFilesRead, srv.ID); err == nil && allowed {
			out = append(out, sftpd.ServerRef{ID: srv.ID, Name: srv.Name, Dir: b.s.servers.Dir(srv.ID)})
		}
	}
	return out, nil
}

func (b sftpBackend) CanWrite(ctx context.Context, userID int64, serverID string) bool {
	u, ok := b.user(ctx, userID)
	if !ok {
		return false
	}
	allowed, err := b.s.store.Require(ctx, u, auth.PermFilesWrite, serverID)
	return err == nil && allowed
}

func (b sftpBackend) Audit(userID int64, username, ip, action, target string, details map[string]any) {
	e := audit.Entry{Username: username, Action: action, Target: target, IP: ip, Details: details}
	if userID != 0 {
		e.UserID = &userID
	}
	b.s.audit.Log(context.Background(), e)
}

// SFTPInfo is what the Files tab needs to show connection details.
type SFTPInfo struct {
	Enabled     bool   `json:"enabled"`
	Port        int    `json:"port"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// handleSFTPInfo tells the Files tab how to connect.
func (s *Server) handleSFTPInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.sftpInfo)
}
