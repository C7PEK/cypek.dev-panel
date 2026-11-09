// Package sftpd is the panel's built-in SFTP server (no system sshd, no
// Linux accounts). Users log in with their panel username and password:
// "name" shows every server they may read as a folder, "name.<server id>"
// opens one server directly. Every path goes through files.ResolveSafe, the
// .homepanel folder is hidden, and writes need the files.write permission.
package sftpd

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ServerRef is one game server a user can reach.
type ServerRef struct {
	ID   string
	Name string
	Dir  string
}

// Backend connects the SFTP server to the panel's accounts and permissions.
type Backend interface {
	// Authenticate checks a password (rate limiting and audit included) and
	// returns the user id and canonical username.
	Authenticate(ctx context.Context, username, password, ip string) (int64, string, error)
	// Servers lists the servers the user may read files of.
	Servers(ctx context.Context, userID int64) ([]ServerRef, error)
	// CanWrite reports files.write on a server.
	CanWrite(ctx context.Context, userID int64, serverID string) bool
	// Audit records an event.
	Audit(userID int64, username, ip, action, target string, details map[string]any)
}

// Server accepts SSH connections and serves only the sftp subsystem.
type Server struct {
	Listen      string
	HostKeyPath string
	Backend     Backend
	Log         *slog.Logger

	signer      ssh.Signer
	fingerprint string
	mu          sync.Mutex
	perIP       map[string]int
	total       int
}

const (
	maxConns      = 64
	maxConnsPerIP = 8
	handshakeTime = 30 * time.Second
)

// Fingerprint is the host key's SHA256 fingerprint, as clients show it.
func (s *Server) Fingerprint() string { return s.fingerprint }

// LoadHostKey creates (once) and loads the ed25519 host key.
func (s *Server) LoadHostKey() error {
	b, err := os.ReadFile(s.HostKeyPath)
	if errors.Is(err, os.ErrNotExist) {
		_, priv, gerr := ed25519.GenerateKey(rand.Reader)
		if gerr != nil {
			return gerr
		}
		block, merr := ssh.MarshalPrivateKey(priv, "panel.cypek.dev sftp host key")
		if merr != nil {
			return merr
		}
		b = pem.EncodeToMemory(block)
		if err := os.MkdirAll(filepath.Dir(s.HostKeyPath), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(s.HostKeyPath, b, 0o600); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		return fmt.Errorf("sftp host key %s: %w", s.HostKeyPath, err)
	}
	s.signer = signer
	s.fingerprint = ssh.FingerprintSHA256(signer.PublicKey())
	return nil
}

// session is what a successful login carries into the connection.
type session struct {
	userID   int64
	username string
	ip       string
	single   string // server id for "name.<id>" logins
}

// Run listens until ctx ends.
func (s *Server) Run(ctx context.Context) error {
	if s.signer == nil {
		if err := s.LoadHostKey(); err != nil {
			return err
		}
	}
	ln, err := net.Listen("tcp", s.Listen)
	if err != nil {
		return fmt.Errorf("sftp listen on %s: %w", s.Listen, err)
	}
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			return err
		}
		go s.handle(ctx, conn)
	}
}

func (s *Server) acquire(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perIP == nil {
		s.perIP = map[string]int{}
	}
	if s.total >= maxConns || s.perIP[ip] >= maxConnsPerIP {
		return false
	}
	s.total++
	s.perIP[ip]++
	return true
}

func (s *Server) release(ip string) {
	s.mu.Lock()
	s.total--
	if s.perIP[ip]--; s.perIP[ip] <= 0 {
		delete(s.perIP, ip)
	}
	s.mu.Unlock()
}

func (s *Server) config(ctx context.Context, sess *session) *ssh.ServerConfig {
	cfg := &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-panel.cypek.dev",
		MaxAuthTries:  3,
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			name, single := splitLogin(meta.User())
			id, canonical, err := s.Backend.Authenticate(ctx, name, string(password), sess.ip)
			if err != nil && single != "" {
				// A username that itself contains a dot.
				id, canonical, err = s.Backend.Authenticate(ctx, meta.User(), string(password), sess.ip)
				single = ""
			}
			if err != nil {
				return nil, errors.New("access denied")
			}
			if single != "" {
				ok := false
				list, _ := s.Backend.Servers(ctx, id)
				for _, sv := range list {
					if sv.ID == single {
						ok = true
					}
				}
				if !ok {
					return nil, errors.New("access denied")
				}
			}
			sess.userID, sess.username, sess.single = id, canonical, single
			return &ssh.Permissions{}, nil
		},
	}
	cfg.AddHostKey(s.signer)
	return cfg
}

// splitLogin turns "name.abc12345" into ("name", "abc12345").
func splitLogin(user string) (string, string) {
	i := strings.LastIndexByte(user, '.')
	if i <= 0 || i == len(user)-1 {
		return user, ""
	}
	id := user[i+1:]
	if len(id) < 4 || len(id) > 32 {
		return user, ""
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return user, ""
		}
	}
	return user[:i], id
}

func (s *Server) handle(ctx context.Context, raw net.Conn) {
	ip, _, _ := net.SplitHostPort(raw.RemoteAddr().String())
	if !s.acquire(ip) {
		raw.Close()
		return
	}
	defer s.release(ip)
	defer raw.Close()
	sess := &session{ip: ip}
	_ = raw.SetDeadline(time.Now().Add(handshakeTime))
	conn, chans, reqs, err := ssh.NewServerConn(raw, s.config(ctx, sess))
	if err != nil {
		return
	}
	_ = raw.SetDeadline(time.Time{})
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	s.Backend.Audit(sess.userID, sess.username, ip, "auth.sftp_login", sess.single, nil)
	stats := &counters{}
	defer func() {
		if d := stats.details(); d != nil {
			s.Backend.Audit(sess.userID, sess.username, ip, "files.sftp_session", sess.single, d)
		}
	}()
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "only sftp is available")
			continue
		}
		ch, chReqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go s.serveSession(ctx, sess, stats, ch, chReqs)
	}
}

// serveSession accepts only the "sftp" subsystem; shells and commands are
// refused.
func (s *Server) serveSession(ctx context.Context, sess *session, stats *counters, ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "subsystem" || len(req.Payload) < 4 || string(req.Payload[4:]) != "sftp" {
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			if req.Type == "shell" || req.Type == "exec" {
				_, _ = io.WriteString(ch.Stderr(), "This server only offers SFTP (use WinSCP, FileZilla or the sftp command).\r\n")
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 1})
				return
			}
			continue
		}
		_ = req.Reply(true, nil)
		fs := &vfs{ctx: ctx, backend: s.Backend, sess: sess, stats: stats}
		srv := sftp.NewRequestServer(ch, sftp.Handlers{FileGet: fs, FilePut: fs, FileCmd: fs, FileList: fs})
		if err := srv.Serve(); err != nil && !errors.Is(err, io.EOF) && s.Log != nil {
			s.Log.Debug("sftp session ended", "user", sess.username, "err", err)
		}
		_ = srv.Close()
		return
	}
}

type counters struct {
	mu                                    sync.Mutex
	uploaded, removed, renamed, mkdir, rd int
}

func (c *counters) add(field *int) {
	c.mu.Lock()
	*field++
	c.mu.Unlock()
}

func (c *counters) details() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.uploaded+c.removed+c.renamed+c.mkdir == 0 {
		return nil
	}
	return map[string]any{"uploaded": c.uploaded, "removed": c.removed, "renamed": c.renamed, "mkdir": c.mkdir, "downloaded": c.rd}
}
