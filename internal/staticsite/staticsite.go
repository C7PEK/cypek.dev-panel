package staticsite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

type Options struct {
	Dir     string
	Listen  string
	SPA     bool
	Log     io.Writer
	NoCache bool
}

func Handler(fsys fs.FS, spa bool, noCache bool) http.Handler {
	files := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if noCache {
			h.Set("Cache-Control", "no-cache")
		}
		clean := path.Clean("/" + r.URL.Path)
		if hidden(clean) {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(clean, "/")
		if name == "" {
			name = "."
		}
		st, err := fs.Stat(fsys, name)
		switch {
		case err == nil && st.IsDir():
			if _, ierr := fs.Stat(fsys, path.Join(name, "index.html")); ierr != nil {
				if name == "." {
					placeholder(w)
					return
				}
				http.NotFound(w, r)
				return
			}
		case err != nil && spa && path.Ext(name) == "":
			if _, ierr := fs.Stat(fsys, "index.html"); ierr == nil {
				r = r.Clone(r.Context())
				r.URL.Path = "/"
			}
		case err != nil && name == ".":
			placeholder(w)
			return
		}
		files.ServeHTTP(w, r)
	})
}

func hidden(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ".") && seg != "." && seg != ".well-known" {
			return true
		}
	}
	return false
}

func placeholder(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.WriteString(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Site is ready</title>
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#050506;color:#f4f4f6;font:16px system-ui,sans-serif}main{max-width:32rem;padding:2rem;text-align:center}h1{font-size:1.4rem;letter-spacing:.06em;text-transform:uppercase}p{color:#a1a1aa;line-height:1.5}code{color:#8b93ff}</style></head>
<body><main><h1>The site is running</h1><p>Upload your files (with an <code>index.html</code>) to the <code>public</code> folder in the panel's Files tab or over SFTP, then refresh this page.</p></main></body></html>`)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func clientIP(r *http.Request) string {
	if ip := r.Header.Get("Cf-Connecting-Ip"); ip != "" && net.ParseIP(ip) != nil {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func Serve(ctx context.Context, o Options) error {
	if o.Dir == "" {
		o.Dir = "public"
	}
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", o.Dir, err)
	}
	root, err := os.OpenRoot(o.Dir)
	if err != nil {
		return fmt.Errorf("open %s: %w", o.Dir, err)
	}
	defer root.Close()
	logger := log.New(o.Log, "", log.LstdFlags)
	inner := Handler(root.FS(), o.SPA, o.NoCache)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		inner.ServeHTTP(sw, r)
		logger.Printf("%s %s %d %s %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond), clientIP(r))
	})
	srv := &http.Server{
		Addr:              o.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	ln, err := net.Listen("tcp", o.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", o.Listen, err)
	}
	logger.Printf("Serving %s on http://%s", o.Dir, ln.Addr())
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		logger.Printf("Stopped")
		return nil
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
