package wb

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

const kCandidatesPageSize = 100

// errNoClient marks "the answer to the owner is already written"; the caller
// only has to stop.
var errNoClient = errors.New("wb: no token")

type candidateRow struct {
	ProductID int64  `json:"product_id"`
	SKU       string `json:"sku"`
	Title     string `json:"title"`
	Stock     int64  `json:"stock"`
	Price     int64  `json:"price"`
	Hidden    bool   `json:"hidden"`
	Published bool   `json:"published"`
}

type candidatesResponse struct {
	Products []candidateRow `json:"products"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type publishRequest struct {
	ProductIDs []int64 `json:"product_ids"`
}

type publishResponse struct {
	Published int               `json:"published"`
	NoCard    []unlinkedProduct `json:"no_card"`
}

type unpublishResponse struct {
	Unpublished int               `json:"unpublished"`
	Failed      []unlinkedProduct `json:"failed"`
}

// candidateFilter reads the state the tab is showing. The ids come from the
// cabinet call the tab already made when it opened: which products have a card
// on the platform is the platform's answer, not ours, and re-asking it per page
// of a hundred rows is what this endpoint must never do.
//
// Nothing given means the whole catalogue, which is what the table did before
// the filter existed and what it still falls back to.
func candidateFilter(r *http.Request) database.CandidateFilter {
	f := database.CandidateFilter{Q: strings.TrimSpace(r.URL.Query().Get("q"))}
	f.IDs = idList(r.URL.Query().Get("ids"))
	f.ExcludeIDs = idList(r.URL.Query().Get("exclude"))
	switch r.URL.Query().Get("state") {
	case "linked":
		yes := true
		f.Linked = &yes
	case "unlinked":
		no := false
		f.Linked = &no
	}
	return f
}

// idList parses "1,2,3". A malformed id is skipped rather than failing the
// request: the list is a view filter, and answering with a shorter table beats
// answering with an error the owner cannot act on.
func idList(s string) []int64 {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// Candidates lists shop products with their publication state - the table the
// owner ticks before pressing "Publish".
func (h *Handlers) Candidates(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	f := candidateFilter(r)
	total, err := h.db.CountWBCandidates(f)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	list, err := h.db.ListWBCandidates(f, kCandidatesPageSize, (page-1)*kCandidatesPageSize)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	res := candidatesResponse{
		Products: make([]candidateRow, 0, len(list)),
		Total:    total, Page: page, PageSize: kCandidatesPageSize,
	}
	for _, c := range list {
		res.Products = append(res.Products, candidateRow{
			ProductID: c.ProductID, SKU: c.SKU, Title: c.Title, Stock: c.Stock,
			Price: c.Price, Hidden: c.Hidden, Published: c.Published,
		})
	}
	writeOK(w, res)
}

// kOrphanSample caps the named orphans. The count already answers whether
// they matter; a thousand names would answer nothing louder than twenty.
const kOrphanSample = 20

// cabinetResponse is what the tab learns when it opens: how the shop's
// catalogue and the cabinet's cards actually overlap.
type cabinetResponse struct {
	Cards    int `json:"cards"`
	Products int `json:"products"`
	Linked   int `json:"linked"`
	Ready    int `json:"ready"`
	NoCard   int `json:"no_card"`
	// Cards in the cabinet whose article matches no product of ours.
	Orphans int `json:"orphans"`
	// OrphanSKUs are those articles, so the tab can name them instead of only
	// counting them. Capped: the number answers "should I care", the list
	// answers "which ones", and nobody reads past the first screen of either.
	OrphanSKUs []string `json:"orphan_skus"`
	// Products whose article matches a card that carries several sizes. They are
	// neither ready nor cardless: the card exists, and publishing still cannot
	// pick a size. Counted apart so the tab can say so instead of filing them
	// under "no card", which would send the owner to create a duplicate.
	Ambiguous int     `json:"ambiguous"`
	ReadyIDs  []int64 `json:"ready_ids"`
}

// Cabinet is asked once, when the tab opens. Not cached and not folded into
// Candidates: the paged product table would otherwise re-read the platform's
// whole card list for every page of a hundred rows, and a cached answer would go
// stale exactly when the owner has just created the card they are looking for.
//
// The match runs through the same index Publish uses, not a set of articles: on
// Wildberries a card with several sizes cannot be linked by vendor code alone,
// and a tab that promised otherwise would be lying about its own button.
func (h *Handlers) Cabinet(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	cards, err := c.ListCards()
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	ids, linked, err := h.db.WBSKUState()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	idx := newCardIndex(cards)
	res := cabinetResponse{
		Cards: len(cards), Products: len(ids),
		ReadyIDs: []int64{}, OrphanSKUs: []string{},
	}
	mine := make(map[string]struct{}, len(ids))
	for sku := range ids {
		mine[key(sku)] = struct{}{}
	}
	for _, card := range cards {
		if _, ours := mine[key(card.VendorCode)]; !ours {
			res.Orphans++
			if len(res.OrphanSKUs) < kOrphanSample {
				res.OrphanSKUs = append(res.OrphanSKUs, card.VendorCode)
			}
		}
	}
	for sku, id := range ids {
		if linked[sku] {
			res.Linked++
			continue
		}
		switch _, reason, ok := idx.lookup(sku); {
		case ok:
			res.Ready++
			res.ReadyIDs = append(res.ReadyIDs, id)
		case reason == i18n.KeyWBAmbiguousCard:
			res.Ambiguous++
		default:
			res.NoCard++
		}
	}
	writeOK(w, res)
}

// Publish links the selected products to their cabinet cards by article. It is
// deliberately a selection, not a sweep: which goods go to a marketplace is the
// owner's decision, and "everything that matched" is not that decision.
func (h *Handlers) Publish(w http.ResponseWriter, r *http.Request) {
	var req publishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.ProductIDs) == 0 {
		writeBadRequest(w, h.msg(i18n.KeyWBNothingSelected))
		return
	}
	c, ok := h.client(w)
	if !ok {
		return
	}
	cards, err := c.ListCards()
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	products, err := h.db.ProductsByIDs(req.ProductIDs)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	links, missing := matchProducts(products, newCardIndex(cards))
	lang := h.db.Lang()
	res := publishResponse{NoCard: []unlinkedProduct{}}
	for _, m := range missing {
		m.Reason = i18n.TIfKey(lang, m.Reason)
		res.NoCard = append(res.NoCard, m)
	}
	for i := range links {
		if err := h.db.UpsertWBLink(&links[i]); err != nil {
			writeInternalError(w, err)
			return
		}
		res.Published++
	}
	h.worker.StockChanged()
	writeOK(w, res)
}

// Unpublish takes products off the channel. The link is dropped only after the
// platform has been told the stock is zero: a card left behind with the last
// level we pushed would keep selling goods we no longer account for. Rows that
// never reached the platform (stock_pushed <= 0) need no call.
func (h *Handlers) Unpublish(w http.ResponseWriter, r *http.Request) {
	var req publishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.ProductIDs) == 0 {
		writeBadRequest(w, h.msg(i18n.KeyWBNothingSelected))
		return
	}
	links, err := h.db.WBLinksByProducts(req.ProductIDs)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var needZero []database.WBLinkState
	res := unpublishResponse{Failed: []unlinkedProduct{}}
	for _, l := range links {
		if l.StockPushed > 0 {
			needZero = append(needZero, l)
			continue
		}
		if err := h.db.DeleteWBLink(l.ProductID); err != nil {
			writeInternalError(w, err)
			return
		}
		res.Unpublished++
	}
	if len(needZero) > 0 {
		failed, err := h.zeroOut(w, needZero)
		if err != nil {
			return
		}
		for _, l := range needZero {
			if msg, bad := failed[l.Barcode]; bad {
				res.Failed = append(res.Failed,
					unlinkedProduct{ProductID: l.ProductID, SKU: l.Barcode, Title: msg})
				continue
			}
			if err := h.db.DeleteWBLink(l.ProductID); err != nil {
				writeInternalError(w, err)
				return
			}
			res.Unpublished++
		}
	}
	writeOK(w, res)
}

// zeroOut pushes a zero stock for every barcode and reports the ones the
// platform refused. A transport failure aborts the whole unpublish: guessing
// that a card is safe to forget is exactly how an oversell starts.
func (h *Handlers) zeroOut(w http.ResponseWriter, links []database.WBLinkState) (map[string]string, error) {
	c, ok := h.client(w)
	if !ok {
		return nil, errNoClient
	}
	s, err := h.db.GetWBSettings()
	if err != nil {
		writeInternalError(w, err)
		return nil, err
	}
	warehouse, err := strconv.ParseInt(s.WarehouseID, 10, 64)
	if err != nil {
		writeBadRequest(w, h.msg(i18n.KeyWBBadWarehouse))
		return nil, err
	}
	failed := make(map[string]string)
	lang := h.db.Lang()
	for len(links) > 0 {
		n := min(kStockBatch, len(links))
		batch := links[:n]
		links = links[n:]

		items := make([]StockItem, len(batch))
		for i, l := range batch {
			items[i] = StockItem{Sku: l.Barcode, Amount: 0}
		}
		refused, err := c.SetStocks(warehouse, items)
		if err != nil {
			writeBadRequest(w, err.Error())
			return nil, err
		}
		for barcode, msg := range refused {
			failed[barcode] = i18n.TIfKey(lang, msg)
		}
	}
	return failed, nil
}
