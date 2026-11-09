package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/C7PEK/cypek.dev-panel/internal/audit"
	"github.com/C7PEK/cypek.dev-panel/internal/auth"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

// consoleMsg is the browser-facing console protocol, a thin view of shim frames.
type consoleMsg struct {
	Type      string `json:"type"`
	Data      string `json:"data,omitempty"`
	State     string `json:"state,omitempty"`
	PID       int    `json:"pid,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	Message   string `json:"message,omitempty"`
	Cols      uint16 `json:"cols,omitempty"`
	Rows      uint16 `json:"rows,omitempty"`
}

const (
	consoleReadLimit   = 16 * 1024
	consoleWriteWait   = 10 * time.Second
	consoleAuditChars  = 200
	consoleAttachRetry = time.Second
)

// handleConsole bridges the browser WebSocket and the server's shim socket.
// The browser connection outlives the game process: while the server is
// stopped the handler sends a state frame and keeps trying to attach, so a
// console tab left open follows restarts without any client logic.
// console.read is enforced by withServer; console.write per input message.
func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	_, user := sessionFromContext(r.Context())
	srv := serverFromContext(r.Context())
	canWrite, err := s.store.Require(r.Context(), user, auth.PermConsoleWrite, srv.ID)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if key := apiKeyFromContext(r); key != nil && !key.AllowsPermission(auth.PermConsoleWrite) {
		canWrite = false
	}

	// coder/websocket rejects cross-origin upgrades by default (Origin must
	// match Host): that is the CSRF protection for this endpoint.
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionContextTakeover})
	if err != nil {
		s.log.Debug("console websocket accept", "err", err)
		return
	}
	ws.SetReadLimit(consoleReadLimit)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	send := func(m consoleMsg) error {
		wctx, wcancel := context.WithTimeout(ctx, consoleWriteWait)
		defer wcancel()
		b, _ := json.Marshal(m)
		return ws.Write(wctx, websocket.MessageText, b)
	}

	// The attached shim client changes across restarts; the reader goroutine
	// always talks to the current one.
	var (
		mu      sync.Mutex
		console supervisor.Console
	)
	current := func() supervisor.Console {
		mu.Lock()
		defer mu.Unlock()
		return console
	}

	s.audit.Log(ctx, audit.Entry{UserID: &user.ID, Username: user.Username, Action: "console.attach", Target: srv.ID, IP: clientIP(r)})

	// Browser -> shim.
	go func() {
		defer cancel()
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var m consoleMsg
			if err := json.Unmarshal(data, &m); err != nil {
				_ = send(consoleMsg{Type: "error", Message: "malformed message"})
				continue
			}
			c := current()
			switch m.Type {
			case "input":
				// Re-check on every command: a revoked permission, a disabled
				// account or a logged-out session ends write access at once.
				if canWrite {
					fresh, err := s.store.GetUserByID(ctx, user.ID)
					ok, perr := s.store.Require(ctx, fresh, auth.PermConsoleWrite, srv.ID)
					if err != nil || fresh.Disabled || perr != nil || !ok || !s.sessionAlive(ctx, r) {
						canWrite = false
					}
				}
				if !canWrite {
					_ = send(consoleMsg{Type: "error", Message: "You have permission to view this console but not to send commands."})
					continue
				}
				if len(m.Data) == 0 || len(m.Data) > shim.MaxInputBytes {
					_ = send(consoleMsg{Type: "error", Message: "Command is empty or too long."})
					continue
				}
				if c == nil {
					_ = send(consoleMsg{Type: "error", Message: "The server is not running."})
					continue
				}
				// Control characters (Ctrl+C = SIGINT through the terminal,
				// Ctrl+\ = SIGQUIT) would stop the game without the power
				// permission: only text, tabs and line breaks reach it.
				m.Data = stripControl(m.Data)
				if err := c.Input(m.Data); err != nil {
					_ = send(consoleMsg{Type: "error", Message: "Could not send the command: " + err.Error()})
					continue
				}
				line := m.Data
				if len(line) > consoleAuditChars {
					line = line[:consoleAuditChars] + "…"
				}
				s.audit.Log(ctx, audit.Entry{UserID: &user.ID, Username: user.Username, Action: "console.input", Target: srv.ID, IP: clientIP(r),
					Details: map[string]any{"command": line}})
			case "resize":
				if c != nil {
					_ = c.Send(shim.Frame{Type: shim.TypeResize, Cols: m.Cols, Rows: m.Rows})
				}
			case "ping":
				_ = send(consoleMsg{Type: "pong"})
			default:
				_ = send(consoleMsg{Type: "error", Message: "unknown message type " + m.Type})
			}
		}
	}()

	for ctx.Err() == nil {
		c, err := s.sup.Attach(ctx, srv.ID)
		if err != nil {
			if !errors.Is(err, supervisor.ErrNotRunning) {
				s.log.Warn("console attach", "server", srv.ID, "err", err)
			}
			st := s.statusOf(ctx, srv.ID)
			if err := send(consoleMsg{Type: "state", State: string(st.State), ExitCode: st.ExitCode, Message: st.Message}); err != nil {
				return
			}
			// Poll until the server comes up or the browser leaves.
			if !s.waitForShim(ctx, srv.ID) {
				_ = ws.Close(websocket.StatusNormalClosure, "bye")
				return
			}
			continue
		}
		mu.Lock()
		console = c
		mu.Unlock()
		hello := c.Hello
		if err := send(consoleMsg{Type: "hello", State: string(hello.State), PID: hello.PID, StartedAt: hello.StartedAt}); err != nil {
			c.Close()
			return
		}
		s.pumpConsole(ctx, c, send)
		mu.Lock()
		console = nil
		mu.Unlock()
		c.Close()
		// Give the supervisor a moment to record the exit before we report it.
		select {
		case <-ctx.Done():
		case <-time.After(200 * time.Millisecond):
		}
	}
	_ = ws.Close(websocket.StatusNormalClosure, "bye")
}

// waitForShim returns true once a shim socket answers, false if ctx ends.
func (s *Server) waitForShim(ctx context.Context, id string) bool {
	t := time.NewTicker(consoleAttachRetry)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
			if c, err := s.sup.Attach(ctx, id); err == nil {
				c.Close()
				return true
			}
		}
	}
}

// pumpConsole forwards shim frames to the browser until the shim connection ends.
func (s *Server) pumpConsole(ctx context.Context, c supervisor.Console, send func(consoleMsg) error) {
	for {
		select {
		case <-ctx.Done():
			return
		case f, ok := <-c.Frames():
			if !ok {
				return
			}
			var m consoleMsg
			switch f.Type {
			case shim.TypeOutput:
				m = consoleMsg{Type: "output", Data: f.Data}
			case shim.TypeState:
				m = consoleMsg{Type: "state", State: string(f.State), ExitCode: f.ExitCode}
			case shim.TypeError:
				m = consoleMsg{Type: "error", Message: f.Message}
			default:
				continue
			}
			if err := send(m); err != nil {
				return
			}
		}
	}
}

func stripControl(in string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, in)
}

// sessionAlive reports whether the request's session still exists (API
// keys are checked on each request anyway).
func (s *Server) sessionAlive(ctx context.Context, r *http.Request) bool {
	sess, _ := sessionFromContext(r.Context())
	if sess == nil || sess.APIKey {
		return sess != nil
	}
	c, err := r.Cookie(s.cookieName(r))
	if err != nil {
		return false
	}
	_, _, err = s.store.LookupSession(ctx, c.Value)
	return err == nil
}
