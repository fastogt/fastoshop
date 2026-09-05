package wb

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/fastogt/fastoshop/app/channel"
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
)

// errNoClient means the answer to the owner is already written; the caller stops.
var errNoClient = errors.New("wb: no token")

type publishResponse struct {
	Published int               `json:"published"`
	NoCard    []unlinkedProduct `json:"no_card"`
}

type unpublishResponse struct {
	Unpublished int               `json:"unpublished"`
	Failed      []unlinkedProduct `json:"failed"`
}

func (h *Handlers) Candidates(w http.ResponseWriter, r *http.Request) {
	page := channel.PageParam(r)
	f := channel.CandidateFilter(r)
	total, err := h.db.CountWBCandidates(f)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	list, err := h.db.ListWBCandidates(f, channel.CandidatesPageSize, (page-1)*channel.CandidatesPageSize)
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

// cabinetResponse is how the shop's catalogue and the cabinet's cards overlap.
type cabinetResponse struct {
	Cards    int `json:"cards"`
	Products int `json:"products"`
	Linked   int `json:"linked"`
	Ready    int `json:"ready"`
	NoCard   int `json:"no_card"`
	// Cards in the cabinet whose article matches no product of ours.
	Orphans int `json:"orphans"`
	// OrphanSKUs are those articles, capped: the count answers "should I care".
	OrphanSKUs []string `json:"orphan_skus"`
	// A multi-size match is neither ready nor cardless: the size cannot be picked.
	Ambiguous int     `json:"ambiguous"`
	ReadyIDs  []int64 `json:"ready_ids"`
}

// Asked once when the tab opens; matched through the same index Publish uses.
func (h *Handlers) Cabinet(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	cards, err := c.ListCards()
	if err != nil {
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	ids, linked, err := h.db.WBSKUState()
	if err != nil {
		httpjson.WriteInternalError(w, err)
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
			if len(res.OrphanSKUs) < channel.OrphanSample {
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
	httpjson.WriteOK(w, res)
}

// A selection, not a sweep: which goods go to a marketplace is the owner's call.
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
	cards, err := c.ListCards()
	if err != nil {
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	products, err := h.db.ProductsByIDs(req.ProductIDs)
	if err != nil {
		httpjson.WriteInternalError(w, err)
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
			httpjson.WriteInternalError(w, err)
			return
		}
		res.Published++
	}
	h.worker.StockChanged()
	httpjson.WriteOK(w, res)
}

// The link is dropped only after the platform has been told the stock is zero.
func (h *Handlers) Unpublish(w http.ResponseWriter, r *http.Request) {
	var req channel.PublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.ProductIDs) == 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyNothingSelected))
		return
	}
	links, err := h.db.WBLinksByProducts(req.ProductIDs)
	if err != nil {
		httpjson.WriteInternalError(w, err)
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
			if msg, bad := failed[l.Barcode]; bad {
				res.Failed = append(res.Failed,
					unlinkedProduct{ProductID: l.ProductID, SKU: l.Barcode, Title: msg})
				continue
			}
			if err := h.db.DeleteWBLink(l.ProductID); err != nil {
				httpjson.WriteInternalError(w, err)
				return
			}
			res.Unpublished++
		}
	}
	httpjson.WriteOK(w, res)
}

// A transport failure aborts the whole unpublish: a forgotten card oversells.
func (h *Handlers) zeroOut(w http.ResponseWriter, links []database.WBLinkState) (map[string]string, error) {
	c, ok := h.client(w)
	if !ok {
		return nil, errNoClient
	}
	s, err := h.db.GetWBSettings()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return nil, err
	}
	warehouse, err := strconv.ParseInt(s.WarehouseID, 10, 64)
	if err != nil {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyWBBadWarehouse))
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
			httpjson.WriteBadRequest(w, err.Error())
			return nil, err
		}
		for barcode, msg := range refused {
			failed[barcode] = i18n.TIfKey(lang, msg)
		}
	}
	return failed, nil
}
