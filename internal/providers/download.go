// Package providers knows how to find and fetch third-party artifacts
// (Paper builds, Temurin JREs). Downloads are HTTPS-only and verified
// against the provider's checksum when one exists.
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

// UserAgent identifies the panel to upstream APIs, as PaperMC asks.
const UserAgent = "homepanel/0.1 (+https://github.com/cypek019/cypek.dev---panel)"

// Client is a shared HTTP client with sane timeouts for API calls.
var Client = &http.Client{Timeout: 30 * time.Second}

// DownloadClient streams large files; no overall timeout, ctx governs it.
// Tests swap it for one that trusts their TLS server.
var DownloadClient = &http.Client{Timeout: 0}

// Progress receives download progress; total is -1 when unknown.
type Progress func(done, total int64)

// ErrChecksum means the downloaded bytes did not match the expected digest.
var ErrChecksum = errors.New("checksum mismatch")

// Download fetches rawURL to dest atomically (temp file + rename). When
// sha256hex is non-empty the file is verified before it is moved into place.
func Download(ctx context.Context, rawURL, dest, sha256hex string, progress Progress) error {
	return DownloadVerified(ctx, rawURL, dest, sha256.New(), sha256hex, 0, progress)
}

// ErrTooLarge means the download exceeded its size limit.
var ErrTooLarge = errors.New("file is too large")

// DownloadVerified is Download with any digest (h, compared to wantHex when
// non-empty) and an optional size limit (maxBytes > 0).
func DownloadVerified(ctx context.Context, rawURL, dest string, h hash.Hash, wantHex string, maxBytes int64, progress Progress) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("refusing to download %q: only https URLs are allowed", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
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

// getJSON fetches a JSON document with the panel's User-Agent.
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
