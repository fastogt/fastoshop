package handler

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The log is the owner's only window into what the background did, so it has to
// arrive as readable text - and only for someone who is logged in.
func TestLogsServed(t *testing.T) {
	h := newTestHandler(t)
	path := filepath.Join(t.TempDir(), "fastoshop.log")
	if err := os.WriteFile(path, []byte("18/08/2026 fastoshop [WARN] localize image\n"), 0644); err != nil {
		t.Fatal(err)
	}
	h.LogPath = path

	w := httptest.NewRecorder()
	h.Logs(w, httptest.NewRequest("GET", "/api/logs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("logs: %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content type: %q", ct)
	}
	if !strings.Contains(w.Body.String(), "localize image") {
		t.Fatalf("body: %q", w.Body.String())
	}

	w = httptest.NewRecorder()
	h.LogInfo(w, httptest.NewRequest("GET", "/api/logs/info", nil))
	if !strings.Contains(w.Body.String(), `"available":true`) {
		t.Fatalf("info: %s", w.Body.String())
	}
}

// A shop started without a log file must say so rather than answer a link with
// an empty file.
func TestLogsAbsent(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	h.Logs(w, httptest.NewRequest("GET", "/api/logs", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("logs without a file: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.LogInfo(w, httptest.NewRequest("GET", "/api/logs/info", nil))
	if !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("info: %s", w.Body.String())
	}
}
