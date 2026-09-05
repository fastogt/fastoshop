package handler

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
	"github.com/fastogt/fastoshop/app/importer"
	"github.com/fastogt/fastoshop/app/media"
)

// Either the ticked rows or the filter: 20 000 products cannot be ticked by hand.
type bulkRequest struct {
	IDs []int64 `json:"ids"`
	All bool    `json:"all"`
	Q   string  `json:"q"`
	// A pointer because "" is a real group (goods added by hand), not "any group".
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
		httpjson.WriteBadRequest(w, "invalid body")
		return req, false
	}
	return req, true
}

func (h *Handler) BulkStock(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	if req.Stock == nil || *req.Stock < 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyBadStock))
		return
	}
	n, err := h.db.SetStockBulk(h.selection(req), *req.Stock)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	h.stockChanged()
	httpjson.WriteOK(w, bulkResponse{Updated: n})
}

// The marketplace link is untouched on purpose.
func (h *Handler) BulkVisibility(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	if req.Hidden == nil {
		httpjson.WriteBadRequest(w, "hidden required")
		return
	}
	n, err := h.db.SetHiddenBulk(h.selection(req), *req.Hidden)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, bulkResponse{Updated: n})
}

// BulkSupplier moves products between groups, for when an article changes hands.
func (h *Handler) BulkSupplier(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	if req.NewSupplier == nil {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeySupplierRequired))
		return
	}
	n, err := h.db.SetSupplierBulk(h.selection(req), *req.NewSupplier)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, bulkResponse{Updated: n})
}

type startedResponse struct {
	Started bool `json:"started"`
	Total   int  `json:"total"`
}

type fillCountResponse struct {
	Main  int `json:"main"`
	Total int `json:"total"`
}

func (h *Handler) BulkFillCount(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	main, total, err := h.db.CountRemoteImages(h.selection(req))
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, fillCountResponse{Main: main, Total: total})
}

// r.Context() would cancel the download the moment the response is written.
//
//nolint:contextcheck // the job outlives the request on purpose
func (h *Handler) BulkFill(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	// Checked before the query: a busy slot is the answer either way.
	if h.job.busy() {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyJobBusy))
		return
	}
	imgs, err := h.db.ListRemoteImages(h.selection(req), req.MainOnly)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	missing, err := media.Missing(h.uploadsDir)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if len(imgs) == 0 && len(missing) == 0 {
		httpjson.WriteOK(w, startedResponse{})
		return
	}
	ctx, ok := h.job.start(kJobFill, []jobStage{
		{Task: kStagePhotos, Total: len(imgs)},
		// Total is what needs a thumbnail now; downloads make their own on the way in.
		{Task: kStageThumbs, Total: len(missing)},
	})
	if !ok {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyJobBusy))
		return
	}
	go func() {
		okCount, failed := importer.LocalizeImages(ctx, h.db, h.uploadsDir, imgs,
			func(done int, inFlight []int64) {
				h.job.progress(kStagePhotos, done, len(imgs), inFlight)
			})
		// Re-read: the download just added files, and failed photos have no thumbnail.
		rest, err := media.Missing(h.uploadsDir)
		if err != nil {
			log.Warnf("scan for thumbnails: %v", err)
		}
		h.job.progress(kStageThumbs, 0, len(rest), nil)
		thumbs, thumbErrors := media.MakeThumbs(ctx, h.uploadsDir, rest, func(done int) {
			h.job.progress(kStageThumbs, done, len(rest), nil)
		})
		// Job state is in memory; the log is what the owner reads after a restart.
		log.Infof("fill photos: %d downloaded, %d failed, %d thumbnails",
			okCount, failed, thumbs)
		h.job.finish(&importer.Result{
			Imported: okCount, Updated: thumbs, Errors: failed + thumbErrors,
		}, nil)
	}()
	httpjson.WriteOK(w, startedResponse{Started: true, Total: len(imgs) + len(missing)})
}

// An explicit list only: there is deliberately no "delete everything matching".
func (h *Handler) BulkDelete(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeBulk(w, r)
	if !ok {
		return
	}
	if len(req.IDs) == 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyNothingSelected))
		return
	}
	n, err := h.db.DeleteProductsBulk(req.IDs)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	h.stockChanged()
	httpjson.WriteOK(w, bulkResponse{Updated: n})
}
