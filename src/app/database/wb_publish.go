package database

import "fmt"

// WBCandidate is a shop product for the channel tab; the link set is the published set.
type WBCandidate struct {
	ProductID int64
	SKU       string
	Title     string
	Stock     int64
	Price     int64
	Hidden    bool
	Published bool
}

// CountWBCandidates counts what the same filter would list.
func (d *Database) CountWBCandidates(f CandidateFilter) (int, error) {
	where, args := candidateWhere(f)
	var n int
	err := d.db.QueryRow(
		`SELECT count(*) FROM products p
		 LEFT JOIN wb_links l ON l.product_id = p.id`+where, args...).Scan(&n)
	return n, err
}

// ListWBCandidates includes hidden products: off the storefront, still sold on WB.
func (d *Database) ListWBCandidates(f CandidateFilter, limit, offset int) ([]WBCandidate, error) {
	where, args := candidateWhere(f)
	args = append(args, limit, offset)
	rows, err := d.db.Query(
		`SELECT p.id, p.sku, p.title, MAX(p.stock, 0), p.price, p.hidden,
		        l.product_id IS NOT NULL
		 FROM products p LEFT JOIN wb_links l ON l.product_id = p.id`+
			where+` ORDER BY p.id LIMIT ? OFFSET ?`, args...)
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

// WBLinkState is a link as unpublishing sees it: barcode to zero out, last level.
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

// WBSKUState returns every product's article and whether it is already linked.
//
// ponytail: the whole catalogue in memory - 24 000 short strings read once when
// the tab opens. An IN (…) against the platform's list would need thousands of
// bound parameters and buys nothing at this size.
func (d *Database) WBSKUState() (map[string]int64, map[string]bool, error) {
	rows, err := d.db.Query(
		`SELECT p.sku, p.id, l.product_id IS NOT NULL
		 FROM products p LEFT JOIN wb_links l ON l.product_id = p.id
		 WHERE p.sku != ''`)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make(map[string]int64)
	linked := make(map[string]bool)
	for rows.Next() {
		var sku string
		var id int64
		var isLinked bool
		if err := rows.Scan(&sku, &id, &isLinked); err != nil {
			return nil, nil, err
		}
		ids[sku] = id
		// A duplicate article keeps the linked side.
		linked[sku] = linked[sku] || isLinked
	}
	return ids, linked, rows.Err()
}
