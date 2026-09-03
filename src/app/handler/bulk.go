package handler

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
	"github.com/fastogt/fastoshop/app/importer"
	"github.com/fastogt/fastoshop/app/media"
)

// bulkRequest carries either the rows the owner ticked or the filter they are
// looking at. Twenty thousand products cannot be selected by hand, so "all
// matching" travels as the filter itself.
type bulkRequest struct {
	IDs []int64 `json:"ids"`
	All bool    `json:"all"`
	Q   string  `json:"q"`
	// Supplier is a pointer because "" is a real group (goods added by hand),
	// so it cannot double as "any group".
	Supplier *string `json:"supplier"`

	Stock       *int    `json:"stock"`
	Hidden      *bool   `json:"hidden"`
	NewSupplier *string `json:"new_supplier"`
	MainOnly    bool    `json:"main_only"`
}

type bulkResponse struct {
	Updated int `json:"updated"`
}

func (h *Handler) selection(req bulkRequest) database.Selection {
	s := database.Selection{IDs: req.IDs, All: req.All, Q: req.Q,
		Supplier: database.AnySupplier}
	if req.Supplier != nil {
		s.Supplier = *req.Supplier
	}
	return s
}

func decodeBulk(w http.ResponseWriter, r *http.Request) (bulkRequest, bool) {
	var req bulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return req, false
	}
	return req, true
}

// BulkStock writes one stock level across the selection - the way out of a feed
// that carried availability but no quantity.
func (h *Handler) BulkStock(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	if req.Stock == nil || *req.Stock < 0 {
		writeBadRequest(w, h.msg(i18n.KeyBadStock))
		return
	}
	n, err := h.db.SetStockBulk(h.selection(req), *req.Stock)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	h.stockChanged()
	writeOK(w, bulkResponse{Updated: n})
}

// BulkVisibility takes products off the storefront or puts them back. The
// marketplace link is untouched on purpose.
func (h *Handler) BulkVisibility(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	if req.Hidden == nil {
		writeBadRequest(w, "hidden required")
		return
	}
	n, err := h.db.SetHiddenBulk(h.selection(req), *req.Hidden)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, bulkResponse{Updated: n})
}

// BulkSupplier moves products between groups, for when an article changes hands.
func (h *Handler) BulkSupplier(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	if req.NewSupplier == nil {
		writeBadRequest(w, h.msg(i18n.KeySupplierRequired))
		return
	}
	n, err := h.db.SetSupplierBulk(h.selection(req), *req.NewSupplier)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, bulkResponse{Updated: n})
}

type startedResponse struct {
	Started bool `json:"started"`
	Total   int  `json:"total"`
}

// BulkFill pulls the supplier's photos onto our own disk over the selection, in
// the background - no feed and no keys are involved, the URLs are already in
// product_images from the import, so a catalogue brought in months ago is fixed
// by the same button as one imported a minute ago.
//
// r.Context() would cancel the download the moment the response is written.
//
//nolint:contextcheck // the job outlives the request on purpose: inheriting
type fillCountResponse struct {
	Main  int `json:"main"`
	Total int `json:"total"`
}

// BulkFillCount feeds the fill dialog: the owner decides between the main photo
// of each product and every photo there is, and that choice is a third of the
// disk and a third of the wait.
func (h *Handler) BulkFillCount(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	main, total, err := h.db.CountRemoteImages(h.selection(req))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, fillCountResponse{Main: main, Total: total})
}

func (h *Handler) BulkFill(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	// Checked before the query: a busy slot is the answer whether or not there
	// turns out to be anything to do.
	if h.job.busy() {
		writeBadRequest(w, h.msg(i18n.KeyJobBusy))
		return
	}
	imgs, err := h.db.ListRemoteImages(h.selection(req), req.MainOnly)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	missing, err := media.Missing(h.uploadsDir)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if len(imgs) == 0 && len(missing) == 0 {
		writeOK(w, startedResponse{})
		return
	}
	ctx, ok := h.job.start(kJobFill, []jobStage{
		{Task: kStagePhotos, Total: len(imgs)},
		// Total is what needs a thumbnail right now; the photos downloaded by
		// the first stage make their own on the way in.
		{Task: kStageThumbs, Total: len(missing)},
	})
	if !ok {
		writeBadRequest(w, h.msg(i18n.KeyJobBusy))
		return
	}
	go func() {
		okCount, failed := importer.LocalizeImages(ctx, h.db, h.uploadsDir, imgs,
			func(done int, inFlight []int64) {
				h.job.progress(kStagePhotos, done, len(imgs), inFlight)
			})
		// Re-read the list: the download has just added files, and photos that
		// failed to download have no thumbnail to make.
		rest, err := media.Missing(h.uploadsDir)
		if err != nil {
			log.Warnf("scan for thumbnails: %v", err)
		}
		h.job.progress(kStageThumbs, 0, len(rest), nil)
		thumbs, thumbErrors := media.MakeThumbs(ctx, h.uploadsDir, rest, func(done int) {
			h.job.progress(kStageThumbs, done, len(rest), nil)
		})
		// The one line that outlives the job: job state is in memory and a restart
		// wipes it, while the owner reads the log after the fact.
		log.Infof("fill photos: %d downloaded, %d failed, %d thumbnails",
			okCount, failed, thumbs)
		h.job.finish(&importer.Result{
			Imported: okCount, Updated: thumbs, Errors: failed + thumbErrors,
		}, nil)
	}()
	writeOK(w, startedResponse{Started: true, Total: len(imgs) + len(missing)})
}

// BulkDelete takes an explicit list only: there is deliberately no "delete
// everything matching the filter". One click erasing a whole catalogue along
// with its indexed slugs and channel links is not a button worth having.
func (h *Handler) BulkDelete(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	if len(req.IDs) == 0 {
		writeBadRequest(w, h.msg(i18n.KeyNothingSelected))
		return
	}
	n, err := h.db.DeleteProductsBulk(req.IDs)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	h.stockChanged()
	writeOK(w, bulkResponse{Updated: n})
}
