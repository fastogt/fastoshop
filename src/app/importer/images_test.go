package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// A one-pixel PNG: enough for http.DetectContentType to call it an image.
var kPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01")

func imageServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ok.png", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(kPNG)
	})
	mux.HandleFunc("/gone.jpg", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	// A .jpg link that answers with an error page — the case that would leave a
	// broken card behind if we trusted the extension.
	mux.HandleFunc("/liar.jpg", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>404</body></html>"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func seedProduct(t *testing.T, d *database.Database, title, supplier string, urls ...string) int64 {
	t.Helper()
	p := &database.Product{Title: title, Price: 100, Supplier: supplier}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	for _, u := range urls {
		if err := d.AddImage(p.ID, u); err != nil {
			t.Fatal(err)
		}
	}
	return p.ID
}

func TestLocalizeImages(t *testing.T) {
	srv := imageServer(t)
	d, _ := database.OpenInMemory()
	defer func() { _ = d.Close() }()
	uploads := t.TempDir()

	id := seedProduct(t, d, "Тёрка", "Ромашка",
		srv.URL+"/ok.png", srv.URL+"/gone.jpg", srv.URL+"/liar.jpg")
	other := seedProduct(t, d, "Чайник", "Оптбаза", srv.URL+"/ok.png")

	sel := database.Selection{All: true, Supplier: "Ромашка"}
	imgs, err := d.ListRemoteImages(sel)
	if err != nil || len(imgs) != 3 {
		t.Fatalf("remote images: %v %+v", err, imgs)
	}

	ok, failed := LocalizeImages(context.Background(), d, uploads, imgs, nil)
	if ok != 1 || failed != 2 {
		t.Fatalf("ok=%d failed=%d", ok, failed)
	}

	got, _ := d.ListImages(id)
	if len(got) != 3 {
		t.Fatalf("images: %+v", got)
	}
	// Position order survives: the first photo is the card in search results.
	if !strings.HasPrefix(got[0].Path, "p") || !strings.HasSuffix(got[0].Path, ".png") {
		t.Fatalf("first image not localized: %q", got[0].Path)
	}
	if _, err := os.Stat(filepath.Join(uploads, got[0].Path)); err != nil {
		t.Fatalf("file not written: %v", err)
	}
	// What failed keeps its link — a hotlinked picture beats no picture.
	if !strings.HasPrefix(got[1].Path, "http") || !strings.HasPrefix(got[2].Path, "http") {
		t.Fatalf("failed downloads must stay links: %+v", got[1:])
	}
	// An error page served as .jpg must not land on disk at all.
	files, _ := os.ReadDir(uploads)
	if len(files) != 1 {
		t.Fatalf("uploads dir: %+v", files)
	}

	// Another group's photos are none of this selection's business.
	if imgs, _ := d.ListImages(other); !strings.HasPrefix(imgs[0].Path, "http") {
		t.Fatalf("other supplier touched: %+v", imgs)
	}
}

// Stopping must actually stop: the way out of sixty thousand photos started by
// mistake cannot be a service restart.
func TestLocalizeImagesStop(t *testing.T) {
	srv := imageServer(t)
	d, _ := database.OpenInMemory()
	defer func() { _ = d.Close() }()

	urls := make([]string, 200)
	for i := range urls {
		urls[i] = srv.URL + "/ok.png"
	}
	seedProduct(t, d, "Тёрка", "Ромашка", urls...)
	imgs, _ := d.ListRemoteImages(database.Selection{All: true, Supplier: "Ромашка"})

	ctx, cancel := context.WithCancel(context.Background())
	ok, failed := LocalizeImages(ctx, d, t.TempDir(), imgs, func(done int, _ []int64) {
		if done >= 10 {
			cancel()
		}
	})
	// Work already in flight finishes, so the exact count is not fixed — what
	// matters is that the remaining hundreds were never started.
	if ok+failed >= len(imgs) {
		t.Fatalf("stop did nothing: %d of %d done", ok+failed, len(imgs))
	}
}

func TestLocalizeImagesProgress(t *testing.T) {
	srv := imageServer(t)
	d, _ := database.OpenInMemory()
	defer func() { _ = d.Close() }()

	seedProduct(t, d, "Тёрка", "Ромашка", srv.URL+"/ok.png", srv.URL+"/ok.png")
	imgs, _ := d.ListRemoteImages(database.Selection{All: true, Supplier: "Ромашка"})

	var last int
	LocalizeImages(context.Background(), d, t.TempDir(), imgs, func(done int, inFlight []int64) {
		last = done
	})
	if last != 2 {
		t.Fatalf("progress ended at %d of 2", last)
	}
}
