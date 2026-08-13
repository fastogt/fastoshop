package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func router(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/products", h.CreateProduct)
	r.Get("/api/products", h.ListProducts)
	r.Put("/api/products/{id}", h.UpdateProduct)
	r.Delete("/api/products/{id}", h.DeleteProduct)
	r.Post("/api/products/{id}/images", h.UploadImage)
	r.Delete("/api/products/{id}/images/{imageID}", h.DeleteImage)
	return r
}

func TestProductAPI(t *testing.T) {
	h := newTestHandler(t)
	r := router(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/products",
		strings.NewReader(`{"title":"Чайник","price":250000,"stock":2,"category":"kitchen"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		Data struct {
			ID        int64     `json:"id"`
			Slug      string    `json:"slug"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Data.Slug != "chajnik" {
		t.Fatalf("slug: %+v", created)
	}
	if created.Data.CreatedAt.IsZero() {
		t.Fatalf("create must return real created_at: %s", w.Body.String())
	}

	// Empty title — 400.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/products", strings.NewReader(`{"title":""}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty title: %d", w.Code)
	}

	// Upload jpeg.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "photo.jpg")
	_, _ = fw.Write([]byte("\xff\xd8\xff\xe0fakejpeg"))
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/api/products/1/images", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}

	// Update returns slug/timestamps.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("PUT", "/api/products/1",
		strings.NewReader(`{"title":"Чайник","price":300000}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d", w.Code)
	}
	var updated struct {
		Data struct {
			Slug      string    `json:"slug"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &updated)
	if updated.Data.Slug != "chajnik" || updated.Data.CreatedAt.IsZero() {
		t.Fatalf("update response must carry slug+timestamps: %s", w.Body.String())
	}
	// List returns the product with its images.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/products", nil))
	body := w.Body.String()
	if !strings.Contains(body, `"images"`) {
		t.Fatalf("list: %s", body)
	}

	// Delete.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/products/1", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d", w.Code)
	}
}

func TestListProductsPaginationAndSearch(t *testing.T) {
	h := newTestHandler(t)
	r := router(h)
	// More than the page cap: otherwise the "per is capped" check checks nothing.
	const seeded = kAdminMaxPageSize + 50
	for i := range seeded {
		body := fmt.Sprintf(`{"title":"Товар %d","sku":"SKU-%d","price":100}`, i, i)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/products", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("seed %d: %d", i, w.Code)
		}
	}
	list := func(query string) listProductsResponse {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/products"+query, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("list %s: %d", query, w.Code)
		}
		var got struct {
			Data listProductsResponse `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got.Data
	}

	// No parameters — the first page of the default size.
	def := list("")
	if len(def.Products) != kAdminPageSize || def.Total != seeded || def.Page != 1 {
		t.Fatalf("default page: %d items %+v", len(def.Products), def)
	}
	if last := list("?page=" + strconv.Itoa(def.Pages)); len(last.Products) != seeded%kAdminPageSize ||
		last.Page != def.Pages {
		t.Fatalf("last page: %d items %+v", len(last.Products), last)
	}
	// per is capped at the maximum.
	if big := list("?per=9999"); len(big.Products) != kAdminMaxPageSize {
		t.Fatalf("per must be capped: %d", len(big.Products))
	}
	// Search by title and by SKU.
	// The very last one is the only title that is not a prefix of its
	// neighbors ("Товар 42" would also match "Товар 420").
	lastTitle := fmt.Sprintf("Товар %d", seeded-1)
	if s := list("?q=" + url.QueryEscape(lastTitle)); s.Total != 1 || s.Products[0].Title != lastTitle {
		t.Fatalf("title search: %+v", s)
	}
	if s := list("?q=SKU-249"); s.Total != 1 || s.Products[0].SKU != "SKU-249" {
		t.Fatalf("sku search: %+v", s)
	}
	// Wildcards are literals, not "match everything".
	if s := list("?q=%25"); s.Total != 0 {
		t.Fatalf("%% must be escaped, got %d", s.Total)
	}
	if s := list("?q=_"); s.Total != 0 {
		t.Fatalf("_ must be escaped, got %d", s.Total)
	}
}

// A punctuation-only title yields an empty slug → an unreachable product page.
func TestCreateProductPunctuationOnlyTitle(t *testing.T) {
	h := newTestHandler(t)
	r := router(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/products", strings.NewReader(`{"title":"!!!"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Data struct {
			ID   int64  `json:"id"`
			Slug string `json:"slug"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Data.Slug == "" {
		t.Fatalf("slug must not be empty: %s", w.Body.String())
	}
	if p, err := h.db.GetVisibleProductBySlug(got.Data.Slug); err != nil || p.ID != got.Data.ID {
		t.Fatalf("product must be reachable by slug %q: %v", got.Data.Slug, err)
	}

	// A second such product must not hit the unique index.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/products", strings.NewReader(`{"title":"???"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("second punctuation title: %d %s", w.Code, w.Body.String())
	}
}

// Stock lives its own life (sales decrease it), so the product form changes
// it only when the field was explicitly sent.
func TestUpdateProductStockOptional(t *testing.T) {
	h := newTestHandler(t)
	r := router(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/products",
		strings.NewReader(`{"title":"Чайник","price":1,"stock":5}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}

	put := func(body string) {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("PUT", "/api/products/1", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("update %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	stock := func() int {
		t.Helper()
		p, err := h.db.GetProduct(1)
		if err != nil {
			t.Fatal(err)
		}
		return p.Stock
	}

	put(`{"title":"Чайник","price":2}`)
	if got := stock(); got != 5 {
		t.Fatalf("omitted stock must stay: %d", got)
	}
	put(`{"title":"Чайник","price":2,"stock":9}`)
	if got := stock(); got != 9 {
		t.Fatalf("sent stock must win: %d", got)
	}
	put(`{"title":"Чайник","price":2,"stock":0}`)
	if got := stock(); got != 0 {
		t.Fatalf("explicit zero must apply: %d", got)
	}
}

// A photo can be removed, not just added — otherwise a mistaken upload stays
// on the card forever. A file from our own upload leaves the disk.
func TestUploadAndDeleteImage(t *testing.T) {
	h := newTestHandler(t)
	r := router(h)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/products",
		strings.NewReader(`{"title":"Чайник","price":100}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d", w.Code)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "photo.jpg")
	_, _ = part.Write([]byte("\xff\xd8\xffjpegdata"))
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/api/products/1/images", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	imgs, _ := h.db.ListImages(1)
	if len(imgs) != 1 {
		t.Fatalf("images after upload: %+v", imgs)
	}
	file := filepath.Join(h.uploadsDir, imgs[0].Path)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("файл не сохранён: %v", err)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("DELETE",
		fmt.Sprintf("/api/products/1/images/%d", imgs[0].ID), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if imgs, _ := h.db.ListImages(1); len(imgs) != 0 {
		t.Fatalf("фото осталось: %+v", imgs)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Error("файл остался на диске")
	}
}
