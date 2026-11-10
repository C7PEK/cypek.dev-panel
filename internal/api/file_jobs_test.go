package api

import (
	"archive/zip"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func (e *testEnv) waitFileJob(serverID, jobID string) map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		_, body := e.do(http.MethodGet, "/api/v1/servers/"+serverID+"/files/jobs", nil)
		for _, j := range body["jobs"].([]any) {
			jm := j.(map[string]any)
			if jm["id"] == jobID && jm["state"] != "running" {
				return jm
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	e.t.Fatalf("job %s never finished", jobID)
	return nil
}

func TestFileJobs(t *testing.T) {
	e := newEnv(t)
	e.completeSetup("owner", "password123")
	resp, body := e.do(http.MethodPost, "/api/v1/servers", map[string]any{"name": "Files", "run": map[string]any{"command": "/bin/true"}})
	if resp.StatusCode != 201 {
		t.Fatalf("create: %d %v", resp.StatusCode, body)
	}
	id := body["id"].(string)
	dir := filepath.Join(e.cfg.ServersDir(), id)
	base := "/api/v1/servers/" + id + "/files"

	// Links that cannot work are refused up front with a helpful message.
	resp, body = e.do(http.MethodPost, base+"/fetch", map[string]any{"url": "https://mega.nz/file/x#y", "path": ""})
	if resp.StatusCode != 422 || !strings.Contains(body["error"].(map[string]any)["message"].(string), "MEGA") {
		t.Fatalf("mega: %d %v", resp.StatusCode, body)
	}
	// The LAN and localhost are off limits (SSRF).
	resp, body = e.do(http.MethodPost, base+"/fetch", map[string]any{"url": "http://127.0.0.1:8420/api/v1/version", "path": ""})
	if resp.StatusCode != 202 {
		t.Fatalf("fetch: %d %v", resp.StatusCode, body)
	}
	job := e.waitFileJob(id, body["job"].(map[string]any)["id"].(string))
	if job["state"] != "failed" || !strings.Contains(job["error"].(string), "not a public internet address") {
		t.Fatalf("loopback fetch: %v", job)
	}

	// Extraction runs in the background.
	zf, _ := os.Create(filepath.Join(dir, "world.zip"))
	zw := zip.NewWriter(zf)
	for _, n := range []string{"world/level.dat", "world/region/r.0.0.mca"} {
		w, _ := zw.Create(n)
		w.Write([]byte(strings.Repeat("x", 4096)))
	}
	zw.Close()
	zf.Close()
	resp, body = e.do(http.MethodPost, base+"/extract", map[string]any{"path": "world.zip"})
	if resp.StatusCode != 202 {
		t.Fatalf("extract: %d %v", resp.StatusCode, body)
	}
	job = e.waitFileJob(id, body["job"].(map[string]any)["id"].(string))
	if job["state"] != "done" || job["bytes"].(float64) != 8192 {
		t.Fatalf("extract job: %v", job)
	}
	if _, err := os.Stat(filepath.Join(dir, "world", "region", "r.0.0.mca")); err != nil {
		t.Fatal("not extracted")
	}
	resp, _ = e.do(http.MethodDelete, base+"/jobs/nope", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("cancel unknown: %d", resp.StatusCode)
	}
}
