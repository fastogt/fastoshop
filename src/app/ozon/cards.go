package ozon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/fastogt/fastoshop/app/channel"
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
)

// kCardsPageSize is a page of the cabinet-cards table.
const kCardsPageSize = 100

// cardRow is one cabinet card, labelled by its article: Ozon's product list carries no names.
type cardRow struct {
	LinkID        int64  `json:"link_id"`
	OfferID       string `json:"offer_id"`
	OzonProductID int64  `json:"ozon_product_id"`
	ProductID     int64  `json:"product_id"`
	ProductTitle  string `json:"product_title"`
	ProductSKU    string `json:"product_sku"`
	ProductStock  int64  `json:"product_stock"`
	Qty           int64  `json:"qty"`
	SuggestedQty  int    `json:"suggested_qty"`
	CardStock     int64  `json:"card_stock"`
	Price         int64  `json:"price"`
}

type cabinetCardsResponse struct {
	Cards    []cardRow `json:"cards"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

type linkCardRequest struct {
	OfferID   string `json:"offer_id"`
	ProductID int64  `json:"product_id"`
	Qty       int64  `json:"qty"`
}

// Cards lists the cabinet's cards with what each is linked to; view is all, unlinked or packs.
//
// ponytail: the cabinet is read whole on every page, as Cabinet does; a catalogue of tens
// of thousands of cards needs the list cached between pages.
func (h *Handlers) Cards(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	offers, err := c.ListProducts()
	if err != nil {
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	links, err := h.db.AllOzonLinks()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	rows := cardRows(offers, links)
	rows = filterCards(rows, r.URL.Query().Get("view"), r.URL.Query().Get("q"))
	page := channel.PageParam(r)
	total := len(rows)
	from := min((page-1)*kCardsPageSize, total)
	rows = rows[from:min(from+kCardsPageSize, total)]
	if err := h.fillProducts(rows); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, cabinetCardsResponse{Cards: rows, Total: total, Page: page, PageSize: kCardsPageSize})
}

func cardRows(offers []Offer, links map[string]database.OzonLinkState) []cardRow {
	rows := make([]cardRow, 0, len(offers))
	for _, o := range offers {
		row := cardRow{OfferID: o.OfferID, OzonProductID: o.ProductID, Qty: 1,
			SuggestedQty: channel.SuggestPackQty(o.OfferID)}
		if l, linked := links[o.OfferID]; linked {
			row.LinkID, row.ProductID, row.Qty, row.Price = l.ID, l.ProductID, l.Qty, l.Price
		}
		rows = append(rows, row)
	}
	return rows
}

func filterCards(rows []cardRow, view, q string) []cardRow {
	q = strings.ToLower(strings.TrimSpace(q))
	out := rows[:0:0]
	for _, r := range rows {
		switch view {
		case "unlinked":
			if r.LinkID != 0 {
				continue
			}
		case "packs":
			if r.Qty < 2 && r.SuggestedQty < 2 {
				continue
			}
		}
		if q != "" && !strings.Contains(strings.ToLower(r.OfferID), q) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// fillProducts adds the linked product's name and stock to one page of rows.
func (h *Handlers) fillProducts(rows []cardRow) error {
	var ids []int64
	for _, r := range rows {
		if r.ProductID != 0 {
			ids = append(ids, r.ProductID)
		}
	}
	products, err := h.db.ProductsByIDs(ids)
	if err != nil {
		return err
	}
	byID := make(map[int64]database.Product, len(products))
	for _, p := range products {
		byID[p.ID] = p
	}
	for i := range rows {
		p, found := byID[rows[i].ProductID]
		if !found {
			continue
		}
		rows[i].ProductTitle, rows[i].ProductSKU = p.Title, p.SKU
		rows[i].ProductStock = int64(max(p.Stock, 0))
		rows[i].CardStock = rows[i].ProductStock / rows[i].Qty
	}
	return nil
}

// LinkCard ties one card to a product by hand, with the units one card sells.
func (h *Handlers) LinkCard(w http.ResponseWriter, r *http.Request) {
	var req linkCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OfferID == "" {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	if !channel.ValidPackQty(req.Qty) {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyBadPackQty))
		return
	}
	products, err := h.db.ProductsByIDs([]int64{req.ProductID})
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if len(products) == 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyLinkProductGone))
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
	for _, o := range offers {
		if o.OfferID != req.OfferID {
			continue
		}
		if err := h.db.UpsertOzonLink(&database.OzonLink{ProductID: req.ProductID, Qty: req.Qty,
			OfferID: o.OfferID}); err != nil {
			httpjson.WriteInternalError(w, err)
			return
		}
		h.worker.StockChanged()
		httpjson.WriteOK(w, channel.OKStatusResponse{Status: "ok"})
		return
	}
	httpjson.WriteBadRequest(w, h.msg(i18n.KeyCardNotInCabinet))
}

// UnlinkCard zeroes the card on Ozon before forgetting it, like Unpublish.
func (h *Handlers) UnlinkCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "linkID"), 10, 64)
	if err != nil {
		httpjson.WriteBadRequest(w, "invalid link id")
		return
	}
	l, err := h.db.OzonLinkByID(id)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if l == nil {
		httpjson.WriteNotFound(w, h.msg(i18n.KeyOzonNotLinked))
		return
	}
	if l.StockPushed > 0 {
		failed, err := h.zeroOut(w, []database.OzonLinkState{*l})
		if err != nil {
			return
		}
		if msg, bad := failed[l.OfferID]; bad {
			httpjson.WriteBadRequest(w, msg)
			return
		}
	}
	if err := h.db.DeleteOzonLink(l.ID); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, channel.OKStatusResponse{Status: "ok"})
}
