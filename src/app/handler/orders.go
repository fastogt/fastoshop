package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

type listOrdersResponse struct {
	Orders []database.Order `json:"orders"`
	Total  int              `json:"total"`
	Page   int              `json:"page"`
	Pages  int              `json:"pages"`
}

const (
	kOrdersPageSize    = 50
	kOrdersMaxPageSize = 500
)

type orderStatusRequest struct {
	Status string `json:"status"`
}

type orderItem struct {
	SKU   string `json:"sku"`
	Title string `json:"title"`
	Price int64  `json:"price"`
	Qty   int    `json:"qty"`
}

// ListOrders is paginated: a shop that sells keeps every order forever, and
// handing the whole history to the admin on each visit stops working long
// before the shop does.
func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	per := kOrdersPageSize
	if n, err := strconv.Atoi(q.Get("per")); err == nil && n > 0 {
		per = min(n, kOrdersMaxPageSize)
	}
	page := 1
	if n, err := strconv.Atoi(q.Get("page")); err == nil && n > 1 {
		page = n
	}
	total, err := h.db.CountOrders(status)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	pages := max((total+per-1)/per, 1)
	if page > pages {
		page = pages
	}
	list, err := h.db.ListOrdersPage(status, q.Get("sort"), q.Get("dir") == "desc",
		per, (page-1)*per)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if list == nil {
		list = []database.Order{}
	}
	writeOK(w, listOrdersResponse{Orders: list, Total: total, Page: page, Pages: pages})
}

func (h *Handler) SetOrderStatus(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		writeBadRequest(w, "bad id")
		return
	}
	var req orderStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.Status != "new" && req.Status != "done" && req.Status != "cancelled") {
		writeBadRequest(w, "status must be new|done|cancelled")
		return
	}
	if err := h.db.SetOrderStatus(id, req.Status); err != nil {
		var oos *database.OutOfStockError
		if errors.As(err, &oos) {
			writeBadRequest(w, fmt.Sprintf(
				h.msg(i18n.KeyOrderStockGone), oos.Name()))
			return
		}
		writeInternalError(w, err)
		return
	}
	h.stockChanged()
	writeOK(w, okStatusResponse(req))
}

type bulkIDsRequest struct {
	IDs []int64 `json:"ids"`
}

type deletedResponse struct {
	Deleted int `json:"deleted"`
}

// BulkDeleteOrders erases orders for good. Only by an explicit list of ids —
// deleting "everything the filter matches" is how a shop loses its journal in
// one click. Stock is not returned: a delete is not a cancellation, and an
// order that still holds goods should be cancelled first.
func (h *Handler) BulkDeleteOrders(w http.ResponseWriter, r *http.Request) {
	var req bulkIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "bad json")
		return
	}
	if len(req.IDs) == 0 {
		writeBadRequest(w, h.msg(i18n.KeyNothingSelected))
		return
	}
	n, err := h.db.DeleteOrders(req.IDs)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, deletedResponse{Deleted: n})
}

type bulkStatusRequest struct {
	IDs    []int64 `json:"ids"`
	Status string  `json:"status"`
}

// bulkStatusFailure names the order that would not move and why. Reopening can
// be refused when the goods are gone, and a count alone would leave the owner
// guessing which of the ticked orders did not change.
type bulkStatusFailure struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

type bulkStatusResponse struct {
	Updated int                 `json:"updated"`
	Failed  []bulkStatusFailure `json:"failed"`
}

// BulkOrderStatus moves the ticked orders. Each one goes through the same
// single-order path rather than one UPDATE: a status change moves stock, and
// that has to stay transactional per order — one refusal must not roll back the
// rest, and must not silently pass either.
func (h *Handler) BulkOrderStatus(w http.ResponseWriter, r *http.Request) {
	var req bulkStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.Status != "new" && req.Status != "done" && req.Status != "cancelled") {
		writeBadRequest(w, "status must be new|done|cancelled")
		return
	}
	if len(req.IDs) == 0 {
		writeBadRequest(w, h.msg(i18n.KeyNothingSelected))
		return
	}
	res := bulkStatusResponse{Failed: []bulkStatusFailure{}}
	for _, id := range req.IDs {
		err := h.db.SetOrderStatus(id, req.Status)
		if err == nil {
			res.Updated++
			continue
		}
		var oos *database.OutOfStockError
		if errors.As(err, &oos) {
			res.Failed = append(res.Failed, bulkStatusFailure{
				ID: id, Reason: fmt.Sprintf(h.msg(i18n.KeyOrderStockGone), oos.Name()),
			})
			continue
		}
		writeInternalError(w, err)
		return
	}
	h.stockChanged()
	writeOK(w, res)
}

// csvSafe neutralizes formula injection: name/phone/titles come from the public
// order form, and Excel and LibreOffice execute a cell starting with = + - @.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

// ExportOrdersCSV — the sales journal (for the tax office/accountant).
func (h *Handler) ExportOrdersCSV(w http.ResponseWriter, r *http.Request) {
	list, err := h.db.ListOrders()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="orders.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "date", "name", "phone", "email", "items", "total", "status"})
	for _, o := range list {
		var items []orderItem
		var total int64
		var desc, totalCell string
		if err := json.Unmarshal([]byte(o.ItemsJSON), &items); err != nil {
			// This is a tax journal: a broken row must not be shown as zero,
			// or revenue is silently understated. Empty total = "count by hand".
			desc = h.msg(i18n.KeyCSVParseFailed)
		} else {
			for _, it := range items {
				total += it.Price * int64(it.Qty)
				desc += fmt.Sprintf("%s x%d; ", it.Title, it.Qty)
			}
			totalCell = fmt.Sprintf("%.2f", float64(total)/100)
		}
		_ = cw.Write([]string{
			fmt.Sprintf("%d", o.ID), o.CreatedAt.Format("2006-01-02 15:04"),
			csvSafe(o.Name), csvSafe(o.Phone), csvSafe(o.Email), csvSafe(desc),
			totalCell, o.Status,
		})
	}
	cw.Flush()
}
