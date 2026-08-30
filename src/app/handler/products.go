package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/media"
)

const kMaxUploadSize = 10 << 20 // 10 MB

type productRequest struct {
	SKU         string `json:"sku"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Price       int64  `json:"price"`
	// A pointer, like smtp_password in settings: a missing field means "don't
	// touch". Otherwise a product form opened before a sale would reset the
	// stock to the number it displayed and resurrect already sold units.
	Stock    *int   `json:"stock"`
	Category string `json:"category"`
	Brand    string `json:"brand"`
	// Supplier group. Pointer for the same reason as the others: a client that
	// does not send the field must not move the product out of its group.
	Supplier *string `json:"supplier"`
	// Same pointer idiom: an absent field means "leave as is". A plain bool would
	// flip visibility for every client that does not know the field yet.
	Hidden *bool `json:"hidden"`
	// Gross weight in grams, packed size in millimetres. Here nil means "clear
	// it" rather than "leave as is": an empty field in the form is how a wrong
	// weight is taken back, and the admin ships inside the same package as the
	// server, so there is no older client to protect against.
	WeightG  *int64 `json:"weight_g"`
	LengthMM *int64 `json:"length_mm"`
	WidthMM  *int64 `json:"width_mm"`
	HeightMM *int64 `json:"height_mm"`
	// Characteristics, in the order the form lists them. Nil means "leave as
	// is" and an empty list means "clear them": a set arrives from a source or
	// from a person, and both state the whole set rather than one key of it.
	Params []database.Param `json:"params"`
}

// cleanParams keeps the rows a card can show. A form always has a blank row at
// the bottom and a person always leaves one half-filled, so both are dropped
// here rather than stored as an empty line under the price.
func cleanParams(in []database.Param) []database.Param {
	out := make([]database.Param, 0, len(in))
	for _, p := range in {
		name := strings.TrimSpace(p.Name)
		if name == "" || !database.ParamValueOK(p.Value) {
			continue
		}
		if s, ok := p.Value.(string); ok {
			p.Value = strings.TrimSpace(s)
		}
		p.Name = name
		out = append(out, p)
	}
	return out
}

// positive keeps a measurement only when it is one: zero and negative are not
// weights or sizes, they are an empty field or a typo, and either must be
// stored as "unknown" rather than as a number a delivery quote would trust.
func positive(v *int64) *int64 {
	if v == nil || *v <= 0 {
		return nil
	}
	return v
}

type productResponse struct {
	database.Product
	Images []database.ProductImage `json:"images"`
}

type listProductsResponse struct {
	Products []productResponse `json:"products"`
	Total    int               `json:"total"`
	Page     int               `json:"page"`
	Pages    int               `json:"pages"`
}

const (
	kAdminPageSize = 100
	// 500 is what bulk work needs: a catalogue of twenty thousand is corrected
	// in blocks, not read row by row. enrich() runs a query per row, so the
	// ceiling stays where a page is still one quick request.
	kAdminMaxPageSize = 500
)

func idParam(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

func (h *Handler) enrich(p database.Product) productResponse {
	imgs, _ := h.db.ListImages(p.ID)
	if imgs == nil {
		imgs = []database.ProductImage{}
	}
	return productResponse{Product: p, Images: imgs}
}

func (h *Handler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var req productRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		writeBadRequest(w, "title required")
		return
	}
	p := &database.Product{SKU: req.SKU, Title: req.Title, Description: req.Description,
		Price: req.Price, Category: req.Category, Brand: req.Brand,
		WeightG: positive(req.WeightG), LengthMM: positive(req.LengthMM),
		WidthMM: positive(req.WidthMM), HeightMM: positive(req.HeightMM),
		Params: cleanParams(req.Params)}
	if req.Stock != nil {
		p.Stock = *req.Stock
	}
	if req.Hidden != nil {
		p.Hidden = *req.Hidden
	}
	if req.Supplier != nil {
		p.Supplier = *req.Supplier
	}
	if err := h.db.CreateProduct(p); err != nil {
		writeInternalError(w, err)
		return
	}
	// Re-read: the slug and timestamps are set by the DB, the request lacks them.
	saved, err := h.db.GetProduct(p.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, h.enrich(*saved))
}

func (h *Handler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeBadRequest(w, "bad id")
		return
	}
	old, err := h.db.GetProduct(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var req productRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		writeBadRequest(w, "title required")
		return
	}
	p := &database.Product{ID: id, SKU: req.SKU, Title: req.Title,
		Description: req.Description, Price: req.Price,
		Stock: old.Stock, Category: req.Category, Brand: req.Brand, Hidden: old.Hidden,
		// Carried from the stored row: the admin form knows nothing about the
		// source price, and dropping it would leave the product out of every
		// later coefficient recompute.
		SourcePrice: old.SourcePrice, PriceManual: old.PriceManual,
		Supplier: old.Supplier,
		WeightG:  positive(req.WeightG),
		LengthMM: positive(req.LengthMM),
		WidthMM:  positive(req.WidthMM),
		HeightMM: positive(req.HeightMM),
		Params:   old.Params}
	if req.Params != nil {
		p.Params = cleanParams(req.Params)
	}
	if req.Stock != nil {
		p.Stock = *req.Stock
	}
	if req.Hidden != nil {
		p.Hidden = *req.Hidden
	}
	if req.Supplier != nil {
		p.Supplier = *req.Supplier
	}
	// Typing a different price claims it: from now on a coefficient recompute
	// must not overwrite this row.
	if req.Price != old.Price {
		p.PriceManual = true
	}
	if err := h.db.UpdateProduct(p); err != nil {
		writeInternalError(w, err)
		return
	}
	saved, err := h.db.GetProduct(id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	h.stockChanged()
	writeOK(w, h.enrich(*saved))
}

func (h *Handler) DeleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeBadRequest(w, "bad id")
		return
	}
	if err := h.db.DeleteProduct(id); err != nil {
		writeInternalError(w, err)
		return
	}
	// A deleted product means zero on the marketplace, and there is no point
	// waiting for the tick while selling something that no longer exists.
	h.stockChanged()
	writeOK(w, okStatusResponse{Status: "deleted"})
}

// DeleteImage removes a photo from a product, and the file with it when the
// photo is ours. Imported photos are links to the supplier's server — there is
// nothing of ours to unlink.
type categoriesResponse struct {
	Categories []string `json:"categories"`
}

// Categories feeds the picker in the product form: the owner reuses a category
// or types a new one, and no separate screen has to exist for a list of slugs.
func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	list, err := h.db.Categories()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, categoriesResponse{Categories: list})
}

func (h *Handler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil {
		writeBadRequest(w, "bad image id")
		return
	}
	im, err := h.db.GetImage(imageID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.db.DeleteImage(imageID); err != nil {
		writeInternalError(w, err)
		return
	}
	if !strings.HasPrefix(im.Path, "http") {
		// Best effort: a missing file must not fail the request, but leaving
		// every removed upload on disk would grow the volume forever.
		if err := os.Remove(filepath.Join(h.uploadsDir, im.Path)); err != nil {
			log.Warnf("delete image file %q: %v", im.Path, err)
		}
		media.RemoveThumb(h.uploadsDir, im.Path)
	}
	p, err := h.db.GetProduct(im.ProductID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, h.enrich(*p))
}

func (h *Handler) ListProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("q"))
	per := kAdminPageSize
	if n, err := strconv.Atoi(q.Get("per")); err == nil && n > 0 {
		per = min(n, kAdminMaxPageSize)
	}
	page := 1
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 1 {
		page = n
	}
	supplier := database.AnySupplier
	if v, ok := q["supplier"]; ok && len(v) > 0 {
		supplier = v[0]
	}
	sort := q.Get("sort")
	desc := q.Get("dir") == "desc"
	total, err := h.db.CountProducts(search, supplier)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	pages := max((total+per-1)/per, 1)
	if page > pages {
		page = pages
	}
	list, err := h.db.ListProductsSorted(search, supplier, sort, desc, per, (page-1)*per)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	// enrich issues one query per product: 100 rows — 101 queries to local
	// SQLite, measured as unnoticeable. A join will be needed if per grows.
	out := make([]productResponse, 0, len(list))
	for _, p := range list {
		out = append(out, h.enrich(p))
	}
	writeOK(w, listProductsResponse{Products: out, Total: total, Page: page, Pages: pages})
}

func (h *Handler) UploadImage(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeBadRequest(w, "bad id")
		return
	}
	if _, err := h.db.GetProduct(id); err != nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, kMaxUploadSize)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeBadRequest(w, "file required (max 10MB)")
		return
	}
	defer func() { _ = f.Close() }()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		writeBadRequest(w, "only jpeg/png/webp")
		return
	}
	name := fmt.Sprintf("p%d-%s%s", id, newToken()[:8], ext)
	if err := os.MkdirAll(h.uploadsDir, 0755); err != nil {
		writeInternalError(w, err)
		return
	}
	dst, err := os.Create(filepath.Join(h.uploadsDir, name))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	defer func() { _ = dst.Close() }()
	if _, err := io.Copy(dst, f); err != nil {
		writeInternalError(w, err)
		return
	}
	// The small copy for the catalogue grid. Not fatal when it fails: the
	// storefront falls back to the original, it is only heavier.
	if err := media.MakeThumb(h.uploadsDir, name); err != nil {
		log.Warnf("thumbnail for %q: %v", name, err)
	}
	if err := h.db.AddImage(id, name); err != nil {
		writeInternalError(w, err)
		return
	}
	p, _ := h.db.GetProduct(id)
	writeOK(w, h.enrich(*p))
}

type imageOrderRequest struct {
	IDs []int64 `json:"ids"`
}

// SetImageOrder is the drag and drop behind the photo strip. The first photo is
// what a shopper sees in the catalogue and what a search engine puts next to the
// snippet, so moving a good one to the front is a routine job, not a nicety —
// especially on an imported catalogue, where the order came from the supplier.
func (h *Handler) SetImageOrder(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeBadRequest(w, "bad id")
		return
	}
	var req imageOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		writeBadRequest(w, "ids required")
		return
	}
	if err := h.db.SetImageOrder(id, req.IDs); err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	p, err := h.db.GetProduct(id)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, h.enrich(*p))
}
