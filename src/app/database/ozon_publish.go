package database

import (
	"fmt"
	"strings"
)

// OzonCandidate is a shop product for the channel tab; a link row IS published.
type OzonCandidate struct {
	ProductID int64
	SKU       string
	Title     string
	Stock     int64
	Price     int64
	Hidden    bool
	Published bool
}

// CandidateFilter narrows publication rows; the caller passes ids from the cabinet call.
type CandidateFilter struct {
	Q string
	// IDs, when set, is the whole result: the set the cabinet call returned.
	IDs []int64
	// ExcludeIDs removes those ready ids from the unlinked rows.
	ExcludeIDs []int64
	// Linked filters our own link table: nil any, true linked, false not.
	Linked *bool
}

// clauses turns the filter into SQL fragments over the products alias p.
//
// ponytail: the id lists travel in the query string, so a filter over a few thousand
// ready products outgrows the URL; the tab then falls back to the unfiltered table.
func (f CandidateFilter) clauses() (string, []any) {
	var conds []string
	var args []any
	if len(f.IDs) > 0 {
		in, a := inClause(f.IDs)
		conds = append(conds, `p.id IN (`+in+`)`)
		args = append(args, a...)
	}
	if len(f.ExcludeIDs) > 0 {
		in, a := inClause(f.ExcludeIDs)
		conds = append(conds, `p.id NOT IN (`+in+`)`)
		args = append(args, a...)
	}
	if f.Linked != nil {
		if *f.Linked {
			conds = append(conds, `l.product_id IS NOT NULL`)
		} else {
			conds = append(conds, `l.product_id IS NULL`)
		}
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(conds, " AND "), args
}

// candidateWhere reuses the product search builder; only category needs the p. alias.
func candidateWhere(f CandidateFilter) (string, []any) {
	where, args := productWhere("", f.Q, AnySupplier, false)
	where = strings.ReplaceAll(where, "category=", "p.category=")
	extra, extraArgs := f.clauses()
	if extra == "" {
		return where, args
	}
	if where == "" {
		// No search: the filter is the whole clause, so its leading AND becomes WHERE.
		return " WHERE " + strings.TrimPrefix(extra, " AND "), extraArgs
	}
	return where + extra, append(args, extraArgs...)
}

// CountOzonCandidates must count the same filter the list uses, or pages come out empty.
func (d *Database) CountOzonCandidates(f CandidateFilter) (int, error) {
	where, args := candidateWhere(f)
	var n int
	err := d.db.QueryRow(
		`SELECT count(*) FROM products p
		 LEFT JOIN ozon_links l ON l.product_id = p.id`+where, args...).Scan(&n)
	return n, err
}

// ListOzonCandidates includes hidden products: off the storefront, still sold on Ozon.
func (d *Database) ListOzonCandidates(f CandidateFilter, limit, offset int) ([]OzonCandidate, error) {
	where, args := candidateWhere(f)
	args = append(args, limit, offset)
	rows, err := d.db.Query(
		`SELECT p.id, p.sku, p.title, MAX(p.stock, 0), p.price, p.hidden,
		        l.product_id IS NOT NULL
		 FROM products p LEFT JOIN ozon_links l ON l.product_id = p.id`+
			where+` ORDER BY p.id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonCandidate
	for rows.Next() {
		var c OzonCandidate
		if err := rows.Scan(&c.ProductID, &c.SKU, &c.Title, &c.Stock, &c.Price,
			&c.Hidden, &c.Published); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// OzonLinkState is a link as unpublishing sees it: article and last pushed level.
type OzonLinkState struct {
	ProductID   int64
	OfferID     string
	StockPushed int64
}

// OzonLinksByProducts returns link rows so an unlink knows what to zero on Ozon.
func (d *Database) OzonLinksByProducts(ids []int64) ([]OzonLinkState, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	in, args := inClause(ids)
	q := fmt.Sprintf(
		`SELECT product_id, offer_id, stock_pushed FROM ozon_links
		 WHERE product_id IN (%s)`, in)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonLinkState
	for rows.Next() {
		var l OzonLinkState
		if err := rows.Scan(&l.ProductID, &l.OfferID, &l.StockPushed); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ProductsByIDs is what publication needs to match articles against the cabinet.
func (d *Database) ProductsByIDs(ids []int64) ([]Product, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	in, args := inClause(ids)
	rows, err := d.db.Query(fmt.Sprintf(`SELECT `+kProductCols+` FROM products
		 WHERE id IN (%s)`, in), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// OzonSKUState is every product's article and whether it is already linked.
//
// ponytail: the whole catalogue in memory - short strings read once when the tab opens.
// An IN (…) against the platform's list is the upgrade if a catalogue outgrows that.
func (d *Database) OzonSKUState() (map[string]int64, map[string]bool, error) {
	rows, err := d.db.Query(
		`SELECT p.sku, p.id, l.product_id IS NOT NULL
		 FROM products p LEFT JOIN ozon_links l ON l.product_id = p.id
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
