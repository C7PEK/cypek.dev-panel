package api

import (
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

func spaHandler(dist fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(dist))
	_, indexErr := fs.Stat(dist, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if indexErr != nil {
			writeError(w, http.StatusServiceUnavailable, CodeFrontendMissing,
				"The web UI has not been built into this binary. Run `make build`, or `make dev` for development.")
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if info, err := fs.Stat(dist, p); err == nil && !info.IsDir() {
			if strings.HasPrefix(p, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}

func devHandler(target string, fallback http.Handler) (http.Handler, error) {
	u, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(u)
	var (
		mu        sync.Mutex
		checkedAt time.Time
		reachable bool
	)
	isUp := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(checkedAt) < 2*time.Second {
			return reachable
		}
		conn, err := net.DialTimeout("tcp", u.Host, 300*time.Millisecond)
		if err == nil {
			conn.Close()
		}
		reachable = err == nil
		checkedAt = time.Now()
		return reachable
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isUp() {
			proxy.ServeHTTP(w, r)
			return
		}
		fallback.ServeHTTP(w, r)
	}), nil
}
