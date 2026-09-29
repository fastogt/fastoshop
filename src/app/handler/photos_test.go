package handler

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fastogt/fastoshop/app/database"
)

// The fetcher sniffs the bytes, so the supplier must serve a real jpeg.
var kJPEG = []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00" +
	"\xff\xdb\x00C\x00\x03\x02\x02\x02\x02\x02\x03\x02\x02\x02\x03\x03\x03\x03\x04\x06\x04" +
	"\x04\x04\x04\x04\x08\x06\x06\x05\x06\t\x08\n\n\t\x08\t\t\n\x0c\x0f\x0c\n\x0b\x0e\x0b" +
	"\t\t\r\x11\r\x0e\x0f\x10\x10\x11\x10\n\x0c\x12\x13\x12\x10\x13\x0f\x10\x10\x10\xff\xd9")

func zipEntries(t *testing.T, body []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("the archive does not open: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
		out[f.Name] = buf.Bytes()
	}
	return out
}

func photoRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	r.Get("/api/products/{id}/photos.zip", h.ExportProductPhotos)
	return r
}

// Ours from disk and the supplier's over the wire, numbered the way the card shows them.
func TestPhotoArchiveCarriesOursAndTheSuppliers(t *testing.T) {
	h := newTestHandler(t)
	p := &database.Product{Title: "Чайник", Price: 100}
	if err := h.db.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	local := []byte("\xff\xd8\xfflocal jpeg")
	if err := os.WriteFile(filepath.Join(h.uploadsDir, "p1-abcd.jpg"), local, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(kJPEG)
	}))
	defer srv.Close()
	if err := h.db.AddImage(p.ID, "p1-abcd.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := h.db.AddImage(p.ID, srv.URL+"/2.jpg"); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	photoRouter(h).ServeHTTP(w, httptest.NewRequest("GET", "/api/products/1/photos.zip", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "application/zip" {
		t.Errorf("content type %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="chajnik-photos.zip"` {
		t.Errorf("disposition %q", got)
	}
	files := zipEntries(t, w.Body.Bytes())
	if len(files) != 2 {
		t.Fatalf("files in the archive: %v", files)
	}
	if !bytes.Equal(files["01.jpg"], local) {
		t.Errorf("our own photo came back changed: %q", files["01.jpg"])
	}
	if !bytes.Equal(files["02.jpg"], kJPEG) {
		t.Errorf("the supplier photo is not in the archive under 02.jpg")
	}
}

// A dead link must not swallow the rest, and the archive must name what is missing.
func TestPhotoArchiveNamesWhatItCouldNotFetch(t *testing.T) {
	h := newTestHandler(t)
	p := &database.Product{Title: "Чайник", Price: 100}
	if err := h.db.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.uploadsDir, "p1-abcd.jpg"), kJPEG, 0o600); err != nil {
		t.Fatal(err)
	}
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer gone.Close()
	if err := h.db.AddImage(p.ID, "p1-abcd.jpg"); err != nil {
		t.Fatal(err)
	}
	if err := h.db.AddImage(p.ID, gone.URL+"/2.jpg"); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	photoRouter(h).ServeHTTP(w, httptest.NewRequest("GET", "/api/products/1/photos.zip", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d", w.Code)
	}
	files := zipEntries(t, w.Body.Bytes())
	if _, ok := files["01.jpg"]; !ok || len(files) != 2 {
		t.Fatalf("files in the archive: %v", files)
	}
	note := string(files["missing.txt"])
	if !bytes.Contains([]byte(note), []byte("02")) || !bytes.Contains([]byte(note), []byte("404")) {
		t.Errorf("the note does not say what failed: %q", note)
	}
}

// An empty zip would look like a working download.
func TestPhotoArchiveRefusesACardWithoutPhotos(t *testing.T) {
	h := newTestHandler(t)
	if err := h.db.CreateProduct(&database.Product{Title: "Чайник", Price: 100}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	photoRouter(h).ServeHTTP(w, httptest.NewRequest("GET", "/api/products/1/photos.zip", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
}
