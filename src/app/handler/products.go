package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/media"
)

const kMaxUploadSize = 10 << 20 // 10 MB

type productRequest struct {
	SKU         string `json:"sku"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Price       int64  `json:"price"`
	// A missing field means "don't touch": a stale form must not reset the stock.
	Stock    *int   `json:"stock"`
	Category string `json:"category"`
	Brand    string `json:"brand"`
	// A client that does not send the field must not move the product out of its group.
	Supplier *string `json:"supplier"`
	// An absent field means "leave as is".
	Hidden *bool `json:"hidden"`
	// Grams and millimetres. Here nil means "clear it" rather than "leave as is".
	WeightG  *int64 `json:"weight_g"`
	LengthMM *int64 `json:"length_mm"`
	WidthMM  *int64 `json:"width_mm"`
	HeightMM *int64 `json:"height_mm"`
	// Nil means "leave as is", an empty list means "clear them": a set arrives whole.
	Params []database.Param `json:"params"`
}

// A form's blank and half-filled rows are dropped rather than stored.
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

// Zero and negative are an empty field or a typo, so they are stored as unknown.
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
	// 500 keeps a page one quick request: enrich() runs a query per row.
	kAdminMaxPageSize = 500
)

func pageParams(q url.Values, def, maxPer int) (per, page int) {
	per, page = def, 1
	if n, err := strconv.Atoi(q.Get("per")); err == nil && n > 0 {
		per = min(n, maxPer)
	}
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 1 {
		page = n
	}
	return per, page
}

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
		httpjson.WriteBadRequest(w, "title required")
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
		httpjson.WriteInternalError(w, err)
		return
	}
	// Re-read: the slug and timestamps are set by the DB, the request lacks them.
	saved, err := h.db.GetProduct(p.ID)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, h.enrich(*saved))
}

func (h *Handler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpjson.WriteBadRequest(w, "bad id")
		return
	}
	old, err := h.db.GetProduct(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var req productRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Title) == "" {
		httpjson.WriteBadRequest(w, "title required")
		return
	}
	p := &database.Product{ID: id, SKU: req.SKU, Title: req.Title,
		Description: req.Description, Price: req.Price,
		Stock: old.Stock, Category: req.Category, Brand: req.Brand, Hidden: old.Hidden,
		// The admin form has no source price; dropping it would skip later recomputes.
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
	// Typing a different price claims it: a recompute must not overwrite this row.
	if req.Price != old.Price {
		p.PriceManual = true
	}
	if err := h.db.UpdateProduct(p); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	saved, err := h.db.GetProduct(id)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	h.stockChanged()
	httpjson.WriteOK(w, h.enrich(*saved))
}

func (h *Handler) DeleteProduct(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpjson.WriteBadRequest(w, "bad id")
		return
	}
	if err := h.db.DeleteProduct(id); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	// A deleted product means zero on the marketplace; no point waiting for the tick.
	h.stockChanged()
	httpjson.WriteOK(w, okStatusResponse{Status: "deleted"})
}

type categoriesResponse struct {
	Categories []string `json:"categories"`
}

func (h *Handler) Categories(w http.ResponseWriter, r *http.Request) {
	list, err := h.db.Categories()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, categoriesResponse{Categories: list})
}

func (h *Handler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	imageID, err := strconv.ParseInt(chi.URLParam(r, "imageID"), 10, 64)
	if err != nil {
		httpjson.WriteBadRequest(w, "bad image id")
		return
	}
	im, err := h.db.GetImage(imageID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.db.DeleteImage(imageID); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if !strings.HasPrefix(im.Path, "http") {
		// Best effort: a missing file must not fail the request.
		if err := os.Remove(filepath.Join(h.uploadsDir, im.Path)); err != nil {
			log.Warnf("delete image file %q: %v", im.Path, err)
		}
		media.RemoveThumb(h.uploadsDir, im.Path)
	}
	p, err := h.db.GetProduct(im.ProductID)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, h.enrich(*p))
}

func (h *Handler) ListProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := strings.TrimSpace(q.Get("q"))
	per, page := pageParams(q, kAdminPageSize, kAdminMaxPageSize)
	supplier := database.AnySupplier
	if v, ok := q["supplier"]; ok && len(v) > 0 {
		supplier = v[0]
	}
	sort := q.Get("sort")
	desc := q.Get("dir") == "desc"
	total, err := h.db.CountProducts(search, supplier)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	pages := max((total+per-1)/per, 1)
	if page > pages {
		page = pages
	}
	list, err := h.db.ListProductsSorted(search, supplier, sort, desc, per, (page-1)*per)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	ids := make([]int64, 0, len(list))
	for _, p := range list {
		ids = append(ids, p.ID)
	}
	images, err := h.db.ImagesFor(ids)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	out := make([]productResponse, 0, len(list))
	for _, p := range list {
		imgs := images[p.ID]
		if imgs == nil {
			imgs = []database.ProductImage{}
		}
		out = append(out, productResponse{Product: p, Images: imgs})
	}
	httpjson.WriteOK(w, listProductsResponse{Products: out, Total: total, Page: page, Pages: pages})
}

func (h *Handler) UploadImage(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpjson.WriteBadRequest(w, "bad id")
		return
	}
	if _, err := h.db.GetProduct(id); err != nil {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, kMaxUploadSize)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		httpjson.WriteBadRequest(w, "file required (max 10MB)")
		return
	}
	defer func() { _ = f.Close() }()
	ext := strings.ToLower(filepath.Ext(hdr.Filename))
	if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" {
		httpjson.WriteBadRequest(w, "only jpeg/png/webp")
		return
	}
	name := fmt.Sprintf("p%d-%s%s", id, newToken()[:8], ext)
	if err := os.MkdirAll(h.uploadsDir, 0755); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	dst, err := os.Create(filepath.Join(h.uploadsDir, name))
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	defer func() { _ = dst.Close() }()
	if _, err := io.Copy(dst, f); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	// Not fatal: the storefront falls back to the original, only heavier.
	if err := media.MakeThumb(h.uploadsDir, name); err != nil {
		log.Warnf("thumbnail for %q: %v", name, err)
	}
	if err := h.db.AddImage(id, name); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	p, _ := h.db.GetProduct(id)
	httpjson.WriteOK(w, h.enrich(*p))
}

type imageOrderRequest struct {
	IDs []int64 `json:"ids"`
}

// The first photo is what the catalogue and the search snippet show.
func (h *Handler) SetImageOrder(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpjson.WriteBadRequest(w, "bad id")
		return
	}
	var req imageOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		httpjson.WriteBadRequest(w, "ids required")
		return
	}
	if err := h.db.SetImageOrder(id, req.IDs); err != nil {
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	p, err := h.db.GetProduct(id)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, h.enrich(*p))
}
