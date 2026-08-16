package database

import (
	"fmt"
	"strings"
)

// WBCandidate is a shop product as the channel tab sees it. Published is the
// presence of a link row — the link set IS the published set.
type WBCandidate struct {
	ProductID int64
	SKU       string
	Title     string
	Stock     int64
	Price     int64
	Hidden    bool
	Published bool
}

// ListWBCandidates returns a page of shop products with their publication state.
// Hidden products are included on purpose: a product can be off the storefront
// and still sold on the marketplace.
func (d *Database) ListWBCandidates(q string, limit, offset int) ([]WBCandidate, error) {
	where, args := productWhere("", q, supplierAny, false)
	args = append(args, limit, offset)
	rows, err := d.db.Query(
		`SELECT p.id, p.sku, p.title, MAX(p.stock, 0), p.price, p.hidden,
		        l.product_id IS NOT NULL
		 FROM products p LEFT JOIN wb_links l ON l.product_id = p.id`+
			strings.ReplaceAll(where, "category=", "p.category=")+
			` ORDER BY p.id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WBCandidate
	for rows.Next() {
		var c WBCandidate
		if err := rows.Scan(&c.ProductID, &c.SKU, &c.Title, &c.Stock, &c.Price,
			&c.Hidden, &c.Published); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// WBLinkState is a link as unpublishing sees it: the barcode to zero out on the
// platform and the level we last pushed there.
type WBLinkState struct {
	ProductID   int64
	NmID        int64
	Barcode     string
	StockPushed int64
}

func (d *Database) WBLinksByProducts(ids []int64) ([]WBLinkState, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	in, args := inClause(ids)
	rows, err := d.db.Query(fmt.Sprintf(
		`SELECT product_id, nm_id, barcode, stock_pushed FROM wb_links
		 WHERE product_id IN (%s)`, in), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WBLinkState
	for rows.Next() {
		var l WBLinkState
		if err := rows.Scan(&l.ProductID, &l.NmID, &l.Barcode, &l.StockPushed); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
