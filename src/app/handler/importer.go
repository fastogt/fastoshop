package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
	"github.com/fastogt/fastoshop/app/importer"
)

type importRequest struct {
	Source       string `json:"source"` // ozon|wb|yml
	ClientID     string `json:"client_id"`
	APIKey       string `json:"api_key"`
	Token        string `json:"token"`
	URL          string `json:"url"`
	DefaultStock int    `json:"default_stock"`
	// Raw base64: the server tells UTF-8 from cp1251 itself.
	FileBase64 string `json:"file_base64"`
	// Empty means the owner's own goods, which no feed touches: refused on import.
	Supplier string `json:"supplier"`
	// 0 means the client sent none: keep whatever the shop already uses.
	Coefficient float64 `json:"coefficient"`
}

func (h *Handler) ImportTemplate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="fastoshop-template.csv"`)
	_, _ = w.Write(importer.Template())
}

type suppliersResponse struct {
	Suppliers []string `json:"suppliers"`
}

func (h *Handler) Suppliers(w http.ResponseWriter, r *http.Request) {
	list, err := h.db.Suppliers()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, suppliersResponse{Suppliers: list})
}

type recomputeRequest struct {
	Coefficient float64 `json:"coefficient"`
}

type recomputeResponse struct {
	Updated int `json:"updated"`
}

func makeSource(req importRequest) (importer.Source, bool) {
	switch req.Source {
	case "ozon":
		if req.ClientID == "" || req.APIKey == "" {
			return nil, false
		}
		return &importer.Ozon{ClientID: req.ClientID, APIKey: req.APIKey}, true
	case "wb":
		if req.Token == "" {
			return nil, false
		}
		return &importer.WB{Token: req.Token}, true
	case "csv":
		raw, err := base64.StdEncoding.DecodeString(req.FileBase64)
		if err != nil || len(raw) == 0 {
			return nil, false
		}
		// One file field, three formats, each recognised by its own bytes.
		if importer.IsXLSX(raw) {
			return &importer.XLSX{Data: raw}, true
		}
		if importer.IsYML(raw) {
			return &importer.YML{Data: raw, DefaultStock: req.DefaultStock}, true
		}
		return &importer.CSV{Data: raw}, true
	case "yml":
		if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
			return nil, false
		}
		return &importer.YML{URL: req.URL, DefaultStock: req.DefaultStock}, true
	}
	return nil, false
}

// Counts plus outliers rather than a full listing of every row.
func (h *Handler) ImportCheck(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	src, ok := makeSource(req)
	if !ok {
		httpjson.WriteBadRequest(w, "source and credentials required")
		return
	}
	coefficient, ok := h.coefficient(w, req.Coefficient)
	if !ok {
		return
	}
	// ponytail: the feed is fetched here and again on import - seconds on 20 000
	// items, and it keeps the whole "staged upload" machinery out of the product.
	items, err := src.Fetch()
	if err != nil {
		httpjson.WriteBadRequest(w, i18n.Localize(h.db.Lang(), err))
		return
	}
	existing, err := h.db.ListProducts()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	rules, err := h.db.ShopPriceRules()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	diff := importer.Compare(items, existing, strings.TrimSpace(req.Supplier), coefficient, rules)
	diff.Currency = importer.FeedCurrency(src)
	httpjson.WriteOK(w, diff)
}

// An absent value means "keep using the shop's".
func (h *Handler) coefficient(w http.ResponseWriter, sent float64) (float64, bool) {
	c := sent
	if c == 0 {
		var err error
		if c, err = h.db.PriceCoefficient(); err != nil {
			httpjson.WriteInternalError(w, err)
			return 0, false
		}
	}
	if !database.ValidCoefficient(c) {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyBadCoefficient))
		return 0, false
	}
	return c, true
}

// Returns at once: a full import walks past nginx's 60 s proxy read timeout.
func (h *Handler) ImportRun(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	src, ok := makeSource(req)
	if !ok {
		httpjson.WriteBadRequest(w, "source and credentials required")
		return
	}
	coefficient, ok := h.coefficient(w, req.Coefficient)
	if !ok {
		return
	}
	// Not remembered: a run's coefficient belongs to that run, not to the shop.
	supplier := strings.TrimSpace(req.Supplier)
	if supplier == "" {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeySupplierRequired))
		return
	}
	if _, ok := h.job.start(kJobImport, []jobStage{
		{Task: importer.StageFetch}, {Task: importer.StageProducts},
	}); !ok {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyJobBusy))
		return
	}
	// The cabinet keys live in memory only while the job runs: they are never stored.
	go func() {
		res, err := importer.Run(src, h.db, supplier, coefficient,
			func(stage string, done, total int) {
				h.job.progress(stage, done, total, nil)
			})
		// Only the feed link is saved; the cabinet keys stay unsaved.
		if err == nil && req.Source == "yml" {
			err = h.db.SaveFeed(&database.Feed{URL: req.URL, Supplier: supplier})
		}
		// The job stores plain text, so the error is translated while still typed.
		if err != nil {
			err = errors.New(i18n.Localize(h.db.Lang(), err))
		}
		h.job.finish(res, err)
	}()
	httpjson.WriteOK(w, startedResponse{Started: true})
}

type feedResponse struct {
	URL      string `json:"url"`
	Supplier string `json:"supplier"`
}

func (h *Handler) Feed(w http.ResponseWriter, r *http.Request) {
	f, err := h.db.GetFeed()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if f == nil {
		httpjson.WriteOK(w, feedResponse{})
		return
	}
	httpjson.WriteOK(w, feedResponse{URL: f.URL, Supplier: f.Supplier})
}

// Prices the owner typed are left alone: a rate change must not undo their work.
func (h *Handler) RecomputePrices(w http.ResponseWriter, r *http.Request) {
	var req recomputeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		!database.ValidCoefficient(req.Coefficient) {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyBadCoefficient))
		return
	}
	n, err := h.db.ApplyPriceCoefficient(req.Coefficient)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	h.stockChanged()
	httpjson.WriteOK(w, recomputeResponse{Updated: n})
}
