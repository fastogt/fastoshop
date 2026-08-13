package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
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
	// Raw file for the csv source, base64: the browser sends the bytes as they
	// are so the server can tell UTF-8 from cp1251 itself. Decoding in the client
	// would put that logic where it cannot be tested.
	FileBase64 string `json:"file_base64"`
	// Supplier group the import belongs to. Empty means the owner's own goods,
	// which no feed touches — so an import into it is refused.
	Supplier string `json:"supplier"`
	// Multiplier from the source price to the shelf price. 0 means the client
	// did not send one — keep whatever the shop already uses.
	Coefficient float64 `json:"coefficient"`
}

// ImportTemplate hands back the spreadsheet the owner fills in. Ozon does the
// same in its cabinet, and for a good reason: a fixed format beats guessing at
// someone's layout.
func (h *Handler) ImportTemplate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="fastoshop-template.csv"`)
	_, _ = w.Write(importer.Template())
}

type suppliersResponse struct {
	Suppliers []string `json:"suppliers"`
}

// Suppliers feeds the group picker in the import form: the owner either reuses a
// group or types a new one, the same way a mailing list is chosen on upload.
func (h *Handler) Suppliers(w http.ResponseWriter, r *http.Request) {
	list, err := h.db.Suppliers()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, suppliersResponse{Suppliers: list})
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
		return &importer.CSV{Data: raw}, true
	case "yml":
		if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
			return nil, false
		}
		return &importer.YML{URL: req.URL, DefaultStock: req.DefaultStock}, true
	}
	return nil, false
}

// ImportCheck is the "Check" button: it validates the credentials and answers
// what the import would change. Counts plus outliers rather than a full listing:
// the owner decides on "120 new, 40 gone, this one is 80% dearer", not on three
// thousand rows.
func (h *Handler) ImportCheck(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	src, ok := makeSource(req)
	if !ok {
		writeBadRequest(w, "source and credentials required")
		return
	}
	coefficient, ok := h.coefficient(w, req.Coefficient)
	if !ok {
		return
	}
	// ponytail: the feed is fetched here and again on import — seconds on 20 000
	// items, and it keeps the whole "staged upload" machinery out of the product.
	items, err := src.Fetch()
	if err != nil {
		writeBadRequest(w, i18n.Localize(h.lang(), err))
		return
	}
	existing, err := h.db.ListProducts()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	diff := importer.Compare(items, existing, strings.TrimSpace(req.Supplier), coefficient)
	diff.Currency = importer.FeedCurrency(src)
	writeOK(w, diff)
}

// coefficient resolves what the client sent against what the shop remembers:
// an absent value means "keep using the shop's".
func (h *Handler) coefficient(w http.ResponseWriter, sent float64) (float64, bool) {
	c := sent
	if c == 0 {
		var err error
		if c, err = h.db.PriceCoefficient(); err != nil {
			writeInternalError(w, err)
			return 0, false
		}
	}
	if !database.ValidCoefficient(c) {
		writeBadRequest(w, h.msg(i18n.KeyBadCoefficient))
		return 0, false
	}
	return c, true
}

// ImportRun starts the transfer and returns at once. It used to answer only when
// the whole catalogue was in, which put it under nginx's sixty-second proxy read
// timeout — 24 000 products walk right up to that line. Progress is polled from
// GET /api/job.
func (h *Handler) ImportRun(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	src, ok := makeSource(req)
	if !ok {
		writeBadRequest(w, "source and credentials required")
		return
	}
	coefficient, ok := h.coefficient(w, req.Coefficient)
	if !ok {
		return
	}
	// Remembered so the "recompute" button knows what the catalogue was built
	// with, without asking the owner to retype it.
	if err := h.db.SetPriceCoefficient(coefficient); err != nil {
		writeInternalError(w, err)
		return
	}
	supplier := strings.TrimSpace(req.Supplier)
	if supplier == "" {
		writeBadRequest(w, h.msg(i18n.KeySupplierRequired))
		return
	}
	if _, ok := h.job.start(kJobImport, []jobStage{
		{Task: importer.StageFetch}, {Task: importer.StageProducts},
	}); !ok {
		writeBadRequest(w, h.msg(i18n.KeyJobBusy))
		return
	}
	// The source keeps the cabinet keys in memory for as long as the job runs and
	// no longer — the admin promises they are never stored.
	go func() {
		res, err := importer.Run(src, h.db, supplier, coefficient,
			func(stage string, done, total int) {
				h.job.progress(stage, done, total, nil)
			})
		// Remembered so next week's refresh is one button. Only the feed link: the
		// cabinet keys stay unsaved.
		if err == nil && req.Source == "yml" {
			err = h.db.SaveFeed(&database.Feed{URL: req.URL, Supplier: supplier})
		}
		// The job stores plain text for the admin, so the owner's language is
		// applied here, at the last point the error is still typed.
		if err != nil {
			err = errors.New(i18n.Localize(h.lang(), err))
		}
		h.job.finish(res, err)
	}()
	writeOK(w, startedResponse{Started: true})
}

type feedResponse struct {
	URL      string `json:"url"`
	Supplier string `json:"supplier"`
}

// Feed tells the admin whether a saved feed exists, so the tab can offer the
// refresh button instead of an empty form.
func (h *Handler) Feed(w http.ResponseWriter, r *http.Request) {
	f, err := h.db.GetFeed()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if f == nil {
		writeOK(w, feedResponse{})
		return
	}
	writeOK(w, feedResponse{URL: f.URL, Supplier: f.Supplier})
}

// RecomputePrices re-derives every imported shelf price from the source price.
// Prices the owner typed are left alone: a rate change must not undo their work.
func (h *Handler) RecomputePrices(w http.ResponseWriter, r *http.Request) {
	var req recomputeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		!database.ValidCoefficient(req.Coefficient) {
		writeBadRequest(w, h.msg(i18n.KeyBadCoefficient))
		return
	}
	n, err := h.db.ApplyPriceCoefficient(req.Coefficient)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	h.stockChanged()
	writeOK(w, recomputeResponse{Updated: n})
}
