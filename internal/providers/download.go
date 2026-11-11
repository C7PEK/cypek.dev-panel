package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const UserAgent = "homepanel/0.1 (+https://github.com/C7PEK/cypek.dev-panel)"

var Client = &http.Client{Timeout: 30 * time.Second}

var DownloadClient = &http.Client{Timeout: 0}

type Progress func(done, total int64)

var ErrChecksum = errors.New("checksum mismatch")

func Download(ctx context.Context, rawURL, dest, sha256hex string, progress Progress) error {
	return DownloadVerified(ctx, rawURL, dest, sha256.New(), sha256hex, 0, progress)
}

type userAgentKey struct{}

func WithUserAgent(ctx context.Context, ua string) context.Context {
	return context.WithValue(ctx, userAgentKey{}, ua)
}

func userAgentFor(ctx context.Context) string {
	if ua, ok := ctx.Value(userAgentKey{}).(string); ok && ua != "" {
		return ua
	}
	return UserAgent
}

var ErrTooLarge = errors.New("file is too large")

func DownloadVerified(ctx context.Context, rawURL, dest string, h hash.Hash, wantHex string, maxBytes int64, progress Progress) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("refusing to download %q: only https URLs are allowed", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgentFor(ctx))
	resp, err := DownloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %s", rawURL, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".part-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if maxBytes > 0 && resp.ContentLength > maxBytes {
		tmp.Close()
		return fmt.Errorf("%w: %s is %d bytes (limit %d)", ErrTooLarge, filepath.Base(dest), resp.ContentLength, maxBytes)
	}
	w := io.MultiWriter(tmp, h)
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 256*1024)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				tmp.Close()
				return werr
			}
			done += int64(n)
			if maxBytes > 0 && done > maxBytes {
				tmp.Close()
				return fmt.Errorf("%w: %s exceeds %d bytes", ErrTooLarge, filepath.Base(dest), maxBytes)
			}
			if progress != nil && (time.Since(last) > 200*time.Millisecond || done == total) {
				progress(done, total)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			tmp.Close()
			return fmt.Errorf("download %s: %w", rawURL, rerr)
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if progress != nil {
		progress(done, total)
	}
	if wantHex != "" {
		got := hex.EncodeToString(h.Sum(nil))
		if !strings.EqualFold(got, wantHex) {
			return fmt.Errorf("%w for %s: got %s, want %s", ErrChecksum, filepath.Base(dest), got, wantHex)
		}
	}
	if err := os.Chmod(tmpName, 0o640); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}

func getJSON(ctx context.Context, rawURL string, into any, decode func(io.Reader, any) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := Client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %s", rawURL, resp.Status)
	}
	return decode(io.LimitReader(resp.Body, 8<<20), into)
}
