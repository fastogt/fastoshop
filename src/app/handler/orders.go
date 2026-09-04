package handler

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
)

// orderResponse carries the snapshot already parsed. The raw items_json stays in
// the database and out of the wire: it is a legal record, not a payload, and
// having the browser and the CSV each parse it their own way is how two numbers
// for one order appear.
type orderResponse struct {
	database.Order
	Items []orderItem `json:"items"`
	Total int64       `json:"total"`
	// Broken tells the admin the snapshot could not be read, so it shows the row
	// as needing a human instead of quietly printing a zero.
	Broken bool `json:"broken"`
}

type listOrdersResponse struct {
	Orders []orderResponse `json:"orders"`
	Total  int             `json:"total"`
	Page   int             `json:"page"`
	Pages  int             `json:"pages"`
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
	// Slug and Image are looked up now, not stored: the snapshot must not age,
	// but the owner opening an order wants to see what was bought. Both are
	// empty for goods that no longer exist.
	Slug  string `json:"slug"`
	Image string `json:"image"`
}

// orderLines reads the snapshot an order was placed with. One reader for the
// screen and the accountant's CSV alike: two copies of this arithmetic would
// disagree the day one of them is fixed.
func orderLines(o database.Order) (items []orderItem, total int64, ok bool) {
	if err := json.Unmarshal([]byte(o.ItemsJSON), &items); err != nil {
		return nil, 0, false
	}
	for _, it := range items {
		total += it.Price * int64(it.Qty)
	}
	return items, total, true
}

// ListOrders is paginated: a shop that sells keeps every order forever, and
// handing the whole history to the admin on each visit stops working long
// before the shop does.
func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	per, page := pageParams(q, kOrdersPageSize, kOrdersMaxPageSize)
	total, err := h.db.CountOrders(status)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	pages := max((total+per-1)/per, 1)
	if page > pages {
		page = pages
	}
	list, err := h.db.ListOrdersPage(status, q.Get("sort"), q.Get("dir") == "desc",
		per, (page-1)*per)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	orders := make([]orderResponse, 0, len(list))
	var skus []string
	for _, o := range list {
		items, sum, ok := orderLines(o)
		for _, it := range items {
			skus = append(skus, it.SKU)
		}
		orders = append(orders, orderResponse{Order: o, Items: items, Total: sum, Broken: !ok})
	}
	// One query for the whole page: a lookup per line would be fifty round trips
	// to draw one screen.
	links, err := h.db.LinksBySKU(skus)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	for i := range orders {
		for j := range orders[i].Items {
			link := links[orders[i].Items[j].SKU]
			orders[i].Items[j].Slug = link.Slug
			orders[i].Items[j].Image = link.Image
		}
	}
	httpjson.WriteOK(w, listOrdersResponse{Orders: orders, Total: total, Page: page, Pages: pages})
}

func (h *Handler) SetOrderStatus(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r)
	if err != nil {
		httpjson.WriteBadRequest(w, "bad id")
		return
	}
	var req orderStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.Status != "new" && req.Status != "done" && req.Status != "cancelled") {
		httpjson.WriteBadRequest(w, "status must be new|done|cancelled")
		return
	}
	if err := h.db.SetOrderStatus(id, req.Status); err != nil {
		var oos *database.OutOfStockError
		if errors.As(err, &oos) {
			httpjson.WriteBadRequest(w, fmt.Sprintf(
				h.msg(i18n.KeyOrderStockGone), oos.Name()))
			return
		}
		httpjson.WriteInternalError(w, err)
		return
	}
	h.stockChanged()
	httpjson.WriteOK(w, okStatusResponse(req))
}

type bulkIDsRequest struct {
	IDs []int64 `json:"ids"`
}

type deletedResponse struct {
	Deleted int `json:"deleted"`
}

// BulkDeleteOrders erases orders for good. Only by an explicit list of ids -
// deleting "everything the filter matches" is how a shop loses its journal in
// one click. Stock is not returned: a delete is not a cancellation, and an
// order that still holds goods should be cancelled first.
func (h *Handler) BulkDeleteOrders(w http.ResponseWriter, r *http.Request) {
	var req bulkIDsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "bad json")
		return
	}
	if len(req.IDs) == 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyNothingSelected))
		return
	}
	n, err := h.db.DeleteOrders(req.IDs)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, deletedResponse{Deleted: n})
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
// that has to stay transactional per order - one refusal must not roll back the
// rest, and must not silently pass either.
func (h *Handler) BulkOrderStatus(w http.ResponseWriter, r *http.Request) {
	var req bulkStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		(req.Status != "new" && req.Status != "done" && req.Status != "cancelled") {
		httpjson.WriteBadRequest(w, "status must be new|done|cancelled")
		return
	}
	if len(req.IDs) == 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyNothingSelected))
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
		httpjson.WriteInternalError(w, err)
		return
	}
	h.stockChanged()
	httpjson.WriteOK(w, res)
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

// ExportOrdersCSV - the sales journal (for the tax office/accountant).
func (h *Handler) ExportOrdersCSV(w http.ResponseWriter, r *http.Request) {
	list, err := h.db.ListOrders()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="orders.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"id", "date", "name", "phone", "email", "comment", "items", "total", "status",
	})
	for _, o := range list {
		items, total, ok := orderLines(o)
		var desc, totalCell string
		if !ok {
			// This is a tax journal: a broken row must not be shown as zero,
			// or revenue is silently understated. Empty total = "count by hand".
			desc = h.msg(i18n.KeyCSVParseFailed)
		} else {
			for _, it := range items {
				desc += fmt.Sprintf("%s x%d; ", it.Title, it.Qty)
			}
			totalCell = fmt.Sprintf("%.2f", float64(total)/100)
		}
		_ = cw.Write([]string{
			fmt.Sprintf("%d", o.ID), o.CreatedAt.Format("2006-01-02 15:04"),
			csvSafe(o.Name), csvSafe(o.Phone), csvSafe(o.Email), csvSafe(o.Comment),
			csvSafe(desc), totalCell, o.Status,
		})
	}
	cw.Flush()
}
