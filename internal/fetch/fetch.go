// Package fetch downloads a file from a link pasted by a user (Google Drive,
// Dropbox, OneDrive, GitHub or any direct link) straight to the server, so a
// big world or modpack does not have to go through the user's upload.
package fetch

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

// Source is a link turned into a direct download.
type Source struct {
	URL  string // what is requested
	Kind string // gdrive | dropbox | onedrive | github | direct
}

// Errors with a message the user can act on.
var (
	ErrUnsupported = errors.New("unsupported link")
	ErrNotAFile    = errors.New("the link leads to a web page, not a file")
	ErrTooLarge    = errors.New("the file is too large")
)

var (
	driveFile = regexp.MustCompile(`^/file/d/([A-Za-z0-9_-]{10,})`)
	driveID   = regexp.MustCompile(`^[A-Za-z0-9_-]{10,}$`)
)

// Resolve turns a share link into a direct download link.
func Resolve(raw string) (Source, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return Source{}, fmt.Errorf("%w: paste a link that starts with https://", ErrUnsupported)
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "drive.google.com" || host == "docs.google.com":
		if strings.HasPrefix(u.Path, "/drive/folders") || strings.Contains(u.Path, "/folders/") {
			return Source{}, fmt.Errorf("%w: this is a Google Drive folder; share a single file (zip the folder first)", ErrUnsupported)
		}
		if host == "docs.google.com" {
			return Source{}, fmt.Errorf("%w: Google Docs/Sheets are not files; upload the file itself to Drive", ErrUnsupported)
		}
		id := ""
		if m := driveFile.FindStringSubmatch(u.Path); m != nil {
			id = m[1]
		} else if v := u.Query().Get("id"); driveID.MatchString(v) {
			id = v
		}
		if id == "" {
			return Source{}, fmt.Errorf("%w: could not find the file id in this Google Drive link", ErrUnsupported)
		}
		// confirm=t skips the "too large to scan for viruses" page.
		// TODO(verify): drive.usercontent.google.com download endpoint (checked 2026-11-10).
		return Source{URL: "https://drive.usercontent.google.com/download?id=" + id + "&export=download&confirm=t", Kind: "gdrive"}, nil
	case host == "drive.usercontent.google.com":
		q := u.Query()
		q.Set("confirm", "t")
		u.RawQuery = q.Encode()
		return Source{URL: u.String(), Kind: "gdrive"}, nil
	case host == "www.dropbox.com" || host == "dropbox.com":
		if strings.HasPrefix(u.Path, "/home") {
			return Source{}, fmt.Errorf("%w: use the file's share link (Share → Copy link)", ErrUnsupported)
		}
		q := u.Query()
		q.Del("raw")
		q.Set("dl", "1")
		u.RawQuery = q.Encode()
		return Source{URL: u.String(), Kind: "dropbox"}, nil
	case host == "1drv.ms" || host == "onedrive.live.com" || strings.HasSuffix(host, ".sharepoint.com"):
		// Public share links through the shares API.
		// TODO(verify): api.onedrive.com shares endpoint for anonymous links.
		enc := strings.TrimRight(base64.URLEncoding.EncodeToString([]byte(u.String())), "=")
		return Source{URL: "https://api.onedrive.com/v1.0/shares/u!" + enc + "/root/content", Kind: "onedrive"}, nil
	case host == "github.com" && strings.Contains(u.Path, "/blob/"):
		u.Host = "raw.githubusercontent.com"
		u.Path = strings.Replace(u.Path, "/blob/", "/", 1)
		return Source{URL: u.String(), Kind: "github"}, nil
	case host == "mega.nz" || host == "mega.co.nz":
		return Source{}, fmt.Errorf("%w: MEGA encrypts files in the browser, so the server cannot download them; use Google Drive or Dropbox", ErrUnsupported)
	case host == "we.tl" || strings.HasSuffix(host, "wetransfer.com"):
		return Source{}, fmt.Errorf("%w: WeTransfer links only work in a browser; use Google Drive or Dropbox", ErrUnsupported)
	case host == "www.mediafire.com" || host == "mediafire.com":
		return Source{}, fmt.Errorf("%w: MediaFire shows a web page first; open it, copy the address of the blue Download button and paste that", ErrUnsupported)
	}
	return Source{URL: u.String(), Kind: "direct"}, nil
}

// Sink receives the download: Create opens a temporary file once the
// response looked like a file, Commit gives it its final name.
type Sink interface {
	Create() (io.Writer, error)
	Commit(name string) (string, error)
	Abort()
}

// Progress reports bytes written and the expected total (-1 = unknown).
type Progress func(done, total int64)

// Download fetches src into sink. client must be a netguard client (public
// addresses only). maxBytes caps the size (free disk space).
func Download(ctx context.Context, client *http.Client, src Source, sink Sink, maxBytes int64, progress Progress) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "panel.cypek.dev file download")
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL may carry tokens; do not echo it
		}
		return "", fmt.Errorf("could not download: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", errors.New("the file was not found (404); check the link")
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
		return "", errors.New("access denied: make the file public (for Google Drive: Share → Anyone with the link)")
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("the server answered HTTP %d", resp.StatusCode)
	}
	name := fileName(resp)
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if ct == "text/html" && !strings.HasSuffix(strings.ToLower(name), ".html") && !strings.HasSuffix(strings.ToLower(name), ".htm") {
		if src.Kind == "gdrive" || src.Kind == "onedrive" || src.Kind == "dropbox" {
			return "", fmt.Errorf("%w: the file is probably not public. Share it with \"Anyone with the link\" and try again", ErrNotAFile)
		}
		return "", fmt.Errorf("%w: copy the direct download link (right-click the download button → Copy link)", ErrNotAFile)
	}
	total := resp.ContentLength
	if maxBytes > 0 && total > maxBytes {
		return "", fmt.Errorf("%w: %s, but only %s are free", ErrTooLarge, humanBytes(total), humanBytes(maxBytes))
	}
	w, err := sink.Create()
	if err != nil {
		return "", err
	}
	committed := false
	defer func() {
		if !committed {
			sink.Abort()
		}
	}()
	buf := make([]byte, 1<<20)
	var done int64
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if maxBytes > 0 && done > maxBytes {
				return "", fmt.Errorf("%w: more than %s", ErrTooLarge, humanBytes(maxBytes))
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return "", werr
			}
			if progress != nil && time.Since(last) > 300*time.Millisecond {
				progress(done, total)
				last = time.Now()
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("the download broke off: %w", rerr)
		}
	}
	if total > 0 && done != total {
		return "", fmt.Errorf("the download broke off at %s of %s", humanBytes(done), humanBytes(total))
	}
	if progress != nil {
		progress(done, total)
	}
	final, err := sink.Commit(name)
	if err != nil {
		return "", err
	}
	committed = true
	return final, nil
}

// fileName takes Content-Disposition, then the last path element.
func fileName(resp *http.Response) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if n := strings.TrimSpace(params["filename"]); n != "" {
				return Clean(n)
			}
		}
	}
	if resp.Request != nil && resp.Request.URL != nil {
		if b := path.Base(resp.Request.URL.Path); b != "" && b != "/" && b != "." && b != "download" && b != "content" {
			if un, err := url.PathUnescape(b); err == nil {
				b = un
			}
			return Clean(b)
		}
	}
	return "download"
}

// Clean makes a remote name a safe single file name.
func Clean(name string) string {
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '/' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimLeft(strings.TrimSpace(name), ".")
	if len(name) > 200 {
		ext := path.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = name[:200-len(ext)] + ext
	}
	if name == "" {
		return "download"
	}
	return name
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
