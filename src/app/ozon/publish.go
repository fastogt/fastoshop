package ozon

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/fastogt/fastoshop/app/channel"
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
)

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

// Candidates lists shop products with their publication state - the table the
// owner ticks before pressing "Publish".
func (h *Handlers) Candidates(w http.ResponseWriter, r *http.Request) {
	page := channel.PageParam(r)
	f := channel.CandidateFilter(r)
	total, err := h.db.CountOzonCandidates(f)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	list, err := h.db.ListOzonCandidates(f, channel.CandidatesPageSize, (page-1)*channel.CandidatesPageSize)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	res := channel.CandidatesResponse{
		Products: make([]channel.CandidateRow, 0, len(list)),
		Total:    total, Page: page, PageSize: channel.CandidatesPageSize,
	}
	for _, c := range list {
		res.Products = append(res.Products, channel.CandidateRow{
			ProductID: c.ProductID, SKU: c.SKU, Title: c.Title, Stock: c.Stock,
			Price: c.Price, Hidden: c.Hidden, Published: c.Published,
		})
	}
	httpjson.WriteOK(w, res)
}

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
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	ids, linked, err := h.db.OzonSKUState()
	if err != nil {
		httpjson.WriteInternalError(w, err)
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
			if len(res.OrphanSKUs) < channel.OrphanSample {
				res.OrphanSKUs = append(res.OrphanSKUs, o.OfferID)
			}
		}
	}
	for sku, id := range ids {
		_, onCard := onPlatform[sku]
		switch {
		case linked[sku]:
			res.Linked++
		case onCard:
			res.Ready++
			res.ReadyIDs = append(res.ReadyIDs, id)
		default:
			res.NoCard++
		}
	}
	httpjson.WriteOK(w, res)
}

// Publish links the selected products to their cabinet cards by article. It is
// deliberately a selection, not a sweep: which goods go to a marketplace is the
// owner's decision, and "everything that matched" is not that decision.
func (h *Handlers) Publish(w http.ResponseWriter, r *http.Request) {
	var req channel.PublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.ProductIDs) == 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyNothingSelected))
		return
	}
	c, ok := h.client(w)
	if !ok {
		return
	}
	offers, err := c.ListProducts()
	if err != nil {
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	byOffer := make(map[string]Offer, len(offers))
	for _, o := range offers {
		byOffer[o.OfferID] = o
	}
	products, err := h.db.ProductsByIDs(req.ProductIDs)
	if err != nil {
		httpjson.WriteInternalError(w, err)
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
			httpjson.WriteInternalError(w, err)
			return
		}
		res.Published++
	}
	h.worker.StockChanged()
	httpjson.WriteOK(w, res)
}

// Unpublish takes products off the channel. The link is dropped only after the
// platform has been told the stock is zero: a card left behind with the last
// level we pushed would keep selling goods we no longer account for. Rows that
// never reached the platform (stock_pushed <= 0) need no call.
func (h *Handlers) Unpublish(w http.ResponseWriter, r *http.Request) {
	var req channel.PublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.ProductIDs) == 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyNothingSelected))
		return
	}
	links, err := h.db.OzonLinksByProducts(req.ProductIDs)
	if err != nil {
		httpjson.WriteInternalError(w, err)
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
			httpjson.WriteInternalError(w, err)
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
				httpjson.WriteInternalError(w, err)
				return
			}
			res.Unpublished++
		}
	}
	httpjson.WriteOK(w, res)
}

// zeroOut pushes a zero stock for every offer and reports the ones the platform
// refused. A transport failure aborts the whole unpublish: guessing that a card
// is safe to forget is exactly how an oversell starts.
func (h *Handlers) zeroOut(w http.ResponseWriter, links []database.OzonLinkState) (map[string]string, error) {
	s, err := h.db.GetOzonSettings()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return nil, err
	}
	warehouse, err := strconv.ParseInt(s.WarehouseID, 10, 64)
	if err != nil {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyOzonBadWarehouse))
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
			httpjson.WriteBadRequest(w, err.Error())
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
