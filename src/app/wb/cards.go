package wb

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

// cardRow is one size of a cabinet card; LinkID 0 means nothing of ours sells through it.
type cardRow struct {
	LinkID       int64  `json:"link_id"`
	Barcode      string `json:"barcode"`
	NmID         int64  `json:"nm_id"`
	Article      string `json:"article"`
	Title        string `json:"title"`
	ProductID    int64  `json:"product_id"`
	ProductTitle string `json:"product_title"`
	ProductSKU   string `json:"product_sku"`
	ProductStock int64  `json:"product_stock"`
	Qty          int64  `json:"qty"`
	SuggestedQty int    `json:"suggested_qty"`
	CardStock    int64  `json:"card_stock"`
	Price        int64  `json:"price"`
}

type cabinetCardsResponse struct {
	Cards    []cardRow `json:"cards"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

type linkCardRequest struct {
	Barcode   string `json:"barcode"`
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
	cards, err := c.ListCards()
	if err != nil {
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	links, err := h.db.AllWBLinks()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	rows := cardRows(cards, links)
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

// cardRows flattens cards into sizes: a link, and so a pack size, belongs to one barcode.
func cardRows(cards []Card, links map[string]database.WBLinkState) []cardRow {
	rows := make([]cardRow, 0, len(cards))
	for _, card := range cards {
		for _, s := range card.Sizes {
			if len(s.Skus) == 0 {
				continue
			}
			article := card.VendorCode
			if len(card.Sizes) > 1 {
				article += " · " + sizeLabel(s)
			}
			row := cardRow{Barcode: s.Skus[0], NmID: card.NmID, Article: article,
				Title: card.Title, Qty: 1, SuggestedQty: channel.SuggestPackQty(card.VendorCode)}
			if l, linked := links[row.Barcode]; linked {
				row.LinkID, row.ProductID, row.Qty, row.Price = l.ID, l.ProductID, l.Qty, l.Price
			}
			rows = append(rows, row)
		}
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
		if q != "" && !strings.Contains(strings.ToLower(r.Article+" "+r.Title+" "+r.Barcode), q) {
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

// LinkCard ties one card size to a product by hand, with the units one card sells.
func (h *Handlers) LinkCard(w http.ResponseWriter, r *http.Request) {
	var req linkCardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Barcode == "" {
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
	cards, err := c.ListCards()
	if err != nil {
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	for _, row := range cardRows(cards, nil) {
		if row.Barcode != req.Barcode {
			continue
		}
		vendor, _, _ := strings.Cut(row.Article, " · ")
		if err := h.db.UpsertWBLink(&database.WBLink{ProductID: req.ProductID, Qty: req.Qty,
			NmID: row.NmID, Barcode: row.Barcode, VendorCode: vendor}); err != nil {
			httpjson.WriteInternalError(w, err)
			return
		}
		h.worker.StockChanged()
		httpjson.WriteOK(w, channel.OKStatusResponse{Status: "ok"})
		return
	}
	httpjson.WriteBadRequest(w, h.msg(i18n.KeyCardNotInCabinet))
}

// UnlinkCard zeroes the card on Wildberries before forgetting it, like Unpublish.
func (h *Handlers) UnlinkCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "linkID"), 10, 64)
	if err != nil {
		httpjson.WriteBadRequest(w, "invalid link id")
		return
	}
	l, err := h.db.WBLinkByID(id)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if l == nil {
		httpjson.WriteNotFound(w, h.msg(i18n.KeyWBNotLinked))
		return
	}
	if l.StockPushed > 0 {
		failed, err := h.zeroOut(w, []database.WBLinkState{*l})
		if err != nil {
			return
		}
		if msg, bad := failed[l.Barcode]; bad {
			httpjson.WriteBadRequest(w, msg)
			return
		}
	}
	if err := h.db.DeleteWBLink(l.ID); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, channel.OKStatusResponse{Status: "ok"})
}
