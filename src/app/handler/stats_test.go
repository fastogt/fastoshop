package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fastogt/fastoshop/app/database"
)

// The shop figures must match the database, not merely look plausible.
func TestStats(t *testing.T) {
	h := newTestHandler(t)
	if err := h.db.CreateSettings(&database.Settings{OwnerEmail: "a@b.c", PasswordHash: "h"}); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		p := &database.Product{Title: "Товар", Price: 100, Stock: 1, Hidden: i == 0}
		if err := h.db.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
	}
	// A file in uploads: the directory walk is counted separately from the DB.
	if err := os.WriteFile(filepath.Join(h.uploadsDir, "p1.jpg"), make([]byte, 2048), 0o600); err != nil {
		t.Fatal(err)
	}
	uploadsAt = time.Time{} // reset the one-minute cache left by neighboring tests

	w := httptest.NewRecorder()
	h.Stats(w, httptest.NewRequest("GET", "/api/stats", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("stats: %d %s", w.Code, w.Body.String())
	}

	var resp struct {
		Data statsResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v\n%s", err, w.Body.String())
	}

	if got := resp.Data.Shop.Products; got != 3 {
		t.Errorf("products = %d, want 3", got)
	}
	if got := resp.Data.Shop.ProductsVisible; got != 2 {
		t.Errorf("products_visible = %d, want 2", got)
	}
	if got := resp.Data.Shop.UploadsBytes; got != 2048 {
		t.Errorf("uploads_bytes = %d, want 2048", got)
	}
	if got := resp.Data.Shop.UploadsFiles; got != 1 {
		t.Errorf("uploads_files = %d, want 1", got)
	}
	if resp.Data.Server.MemoryTotalBytes == 0 {
		t.Error("server memory_total is zero")
	}
	if resp.Data.Shop.ProcessRSSBytes == 0 {
		t.Error("process_rss_bytes is zero")
	}
}
