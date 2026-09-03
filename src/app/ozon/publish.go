package ozon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// kCandidatesPageSize is a page of the product picker in the tab.
const kCandidatesPageSize = 100

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
	Published int `json:"published"`
	// Products whose article has no card in the cabinet. Creating cards is not
	// ours to do yet, so the owner is told plainly instead of silently skipping.
	NoCard []unlinkedProduct `json:"no_card"`
}

type unpublishResponse struct {
	Unpublished int `json:"unpublished"`
	// Rows we could not zero out on the platform. They keep their link on
	// purpose: dropping it would leave a card selling stock we no longer track.
	Failed []unlinkedProduct `json:"failed"`
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
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	f := candidateFilter(r)
	total, err := h.db.CountOzonCandidates(f)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	list, err := h.db.ListOzonCandidates(f, kCandidatesPageSize, (page-1)*kCandidatesPageSize)
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
	// Cards in the cabinet, products in the shop, and the three states between
	// them. They do not add up to Products by accident: a product with no
	// article at all is in none of them.
	Cards    int `json:"cards"`
	Products int `json:"products"`
	Linked   int `json:"linked"`
	Ready    int `json:"ready"`
	NoCard   int `json:"no_card"`
	// Orphans are cards in the cabinet whose article is in no product of ours.
	Orphans int `json:"orphans"`
	// OrphanSKUs are those articles, so the tab can name them instead of only
	// counting them. Capped: the number answers "should I care", the list
	// answers "which ones", and nobody reads past the first screen of either.
	OrphanSKUs []string `json:"orphan_skus"`
	// ReadyIDs are the products that pressing "Publish" would actually link.
	// The tab marks its rows from this and stops offering a button that cannot
	// work: on a live shop the owner ticked a hundred rows and got ninety-nine
	// refusals, because the table never said which ones had a card.
	ReadyIDs []int64 `json:"ready_ids"`
}

// Cabinet is asked once, when the tab opens. It is deliberately not cached and
// not folded into Candidates: the paged product table would otherwise re-read
// the platform's whole article list for every page of a hundred rows, and a
// cached answer would go stale exactly when the owner has just created the card
// they are looking for.
func (h *Handlers) Cabinet(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	offers, err := c.ListProducts()
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	ids, linked, err := h.db.OzonSKUState()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	res := cabinetResponse{
		Cards: len(offers), Products: len(ids),
		ReadyIDs: []int64{}, OrphanSKUs: []string{},
	}
	onPlatform := make(map[string]struct{}, len(offers))
	for _, o := range offers {
		onPlatform[o.OfferID] = struct{}{}
		if _, mine := ids[o.OfferID]; !mine {
			res.Orphans++
			if len(res.OrphanSKUs) < kOrphanSample {
				res.OrphanSKUs = append(res.OrphanSKUs, o.OfferID)
			}
		}
	}
	for sku, id := range ids {
		switch {
		case linked[sku]:
			res.Linked++
		case hasCard(onPlatform, sku):
			res.Ready++
			res.ReadyIDs = append(res.ReadyIDs, id)
		default:
			res.NoCard++
		}
	}
	writeOK(w, res)
}

func hasCard(onPlatform map[string]struct{}, sku string) bool {
	_, ok := onPlatform[sku]
	return ok
}

// Publish links the selected products to their cabinet cards by article. It is
// deliberately a selection, not a sweep: which goods go to a marketplace is the
// owner's decision, and "everything that matched" is not that decision.
func (h *Handlers) Publish(w http.ResponseWriter, r *http.Request) {
	var req publishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.ProductIDs) == 0 {
		writeBadRequest(w, h.msg(i18n.KeyOzonNothingSelected))
		return
	}
	c, ok := h.client(w)
	if !ok {
		return
	}
	offers, err := c.ListProducts()
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	byOffer := make(map[string]Offer, len(offers))
	for _, o := range offers {
		byOffer[o.OfferID] = o
	}
	products, err := h.db.ProductsByIDs(req.ProductIDs)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	res := publishResponse{NoCard: []unlinkedProduct{}}
	for _, p := range products {
		o, found := byOffer[p.SKU]
		if p.SKU == "" || !found {
			res.NoCard = append(res.NoCard,
				unlinkedProduct{ID: p.ID, Title: p.Title, SKU: p.SKU})
			continue
		}
		link := &database.OzonLink{ProductID: p.ID, OfferID: o.OfferID}
		if err := h.db.UpsertOzonLink(link); err != nil {
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
		writeBadRequest(w, h.msg(i18n.KeyOzonNothingSelected))
		return
	}
	links, err := h.db.OzonLinksByProducts(req.ProductIDs)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var needZero []database.OzonLinkState
	res := unpublishResponse{Failed: []unlinkedProduct{}}
	for _, l := range links {
		if l.StockPushed > 0 {
			needZero = append(needZero, l)
			continue
		}
		if err := h.db.DeleteOzonLink(l.ProductID); err != nil {
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
			if msg, bad := failed[l.OfferID]; bad {
				res.Failed = append(res.Failed,
					unlinkedProduct{ID: l.ProductID, SKU: l.OfferID, Title: msg})
				continue
			}
			if err := h.db.DeleteOzonLink(l.ProductID); err != nil {
				writeInternalError(w, err)
				return
			}
			res.Unpublished++
		}
	}
	writeOK(w, res)
}

// zeroOut pushes a zero stock for every offer and reports the ones the platform
// refused. A transport failure aborts the whole unpublish: guessing that a card
// is safe to forget is exactly how an oversell starts.
func (h *Handlers) zeroOut(w http.ResponseWriter, links []database.OzonLinkState) (map[string]string, error) {
	s, err := h.db.GetOzonSettings()
	if err != nil {
		writeInternalError(w, err)
		return nil, err
	}
	warehouse, err := strconv.ParseInt(s.WarehouseID, 10, 64)
	if err != nil {
		writeBadRequest(w, h.msg(i18n.KeyOzonBadWarehouse))
		return nil, err
	}
	c := &Client{ClientID: s.ClientID, APIKey: s.APIKey, BaseURL: h.BaseURL}
	failed := make(map[string]string)
	for len(links) > 0 {
		n := min(kBatchSize, len(links))
		batch := links[:n]
		links = links[n:]

		items := make([]StockItem, len(batch))
		for i, l := range batch {
			items[i] = StockItem{OfferID: l.OfferID, Stock: 0, WarehouseID: warehouse}
		}
		results, err := c.SetStocks(items)
		if err != nil {
			writeBadRequest(w, err.Error())
			return nil, err
		}
		byOffer := make(map[string]ItemResult, len(results))
		for _, res := range results {
			byOffer[res.OfferID] = res
		}
		for _, l := range batch {
			res, found := byOffer[l.OfferID]
			if !found {
				failed[l.OfferID] = h.msg(i18n.KeyOzonNoAnswer)
				continue
			}
			if msg := res.Err(); msg != "" {
				failed[l.OfferID] = i18n.TIfKey(h.db.Lang(), msg)
			}
		}
	}
	return failed, nil
}
