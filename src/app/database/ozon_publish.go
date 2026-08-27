package database

import (
	"fmt"
	"strings"
)

// OzonCandidate is a shop product as the channel tab sees it: what it is called,
// whether it is already published, and what was last pushed for it. Published is
// the presence of a link row — the link set IS the published set.
type OzonCandidate struct {
	ProductID int64
	SKU       string
	Title     string
	Stock     int64
	Price     int64
	Hidden    bool
	Published bool
}

// CandidateFilter narrows the publication table to the rows the owner can act
// on. Which state a product is in depends on the platform's article list, and
// that list lives in the tab's one-per-open cabinet call rather than in our
// database — so the caller passes the ids it learned there instead of the
// database trying to answer a question it cannot see.
type CandidateFilter struct {
	Q string
	// IDs, when set, is the whole result: "ready to link" is exactly the set
	// the cabinet call returned.
	IDs []int64
	// ExcludeIDs removes those same ready ids from the unlinked rows, which is
	// what "no card" means.
	ExcludeIDs []int64
	// Linked filters on our own link table: nil is any, true is linked, false
	// is not.
	Linked *bool
}

// clauses turns the filter into SQL fragments over the products alias p.
//
// ponytail: the id lists travel in the query string, so a filter over more than
// a few thousand ready products would outgrow the URL. The tab falls back to
// the unfiltered table in that case — a seller with thousands of linkable
// articles has long since linked them, and paging the whole catalogue is what
// this endpoint did before the filter existed.
func (f CandidateFilter) clauses() (string, []any) {
	var conds []string
	var args []any
	if len(f.IDs) > 0 {
		conds = append(conds, `p.id IN (`+placeholders(len(f.IDs))+`)`)
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if len(f.ExcludeIDs) > 0 {
		conds = append(conds, `p.id NOT IN (`+placeholders(len(f.ExcludeIDs))+`)`)
		for _, id := range f.ExcludeIDs {
			args = append(args, id)
		}
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

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// candidateWhere joins the owner's search to the state filter. The search half
// is the same builder the product table uses, so a word typed here matches what
// it would match there; only the category column needs the alias, the rest are
// unambiguous because the links table has no columns of those names.
func candidateWhere(f CandidateFilter) (string, []any) {
	where, args := productWhere("", f.Q, supplierAny, false)
	where = strings.ReplaceAll(where, "category=", "p.category=")
	extra, extraArgs := f.clauses()
	if extra == "" {
		return where, args
	}
	if where == "" {
		// No search: the filter becomes the whole clause, so its leading AND
		// has to give way to WHERE.
		return " WHERE " + strings.TrimPrefix(extra, " AND "), extraArgs
	}
	return where + extra, append(args, extraArgs...)
}

// CountOzonCandidates counts what the same filter would list. The paged table
// needs it to draw its page numbers, and it has to agree with the list exactly
// — counting all products while listing a filtered subset is how a table grows
// pages that turn out empty.
func (d *Database) CountOzonCandidates(f CandidateFilter) (int, error) {
	where, args := candidateWhere(f)
	var n int
	err := d.db.QueryRow(
		`SELECT count(*) FROM products p
		 LEFT JOIN ozon_links l ON l.product_id = p.id`+where, args...).Scan(&n)
	return n, err
}

// ListOzonCandidates returns a page of shop products with their publication
// state. Hidden products are included on purpose: a product can be off the
// storefront and still sold on the marketplace.
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

// OzonLinkState is a link as unpublishing sees it: the article to zero out on
// the platform and the level we last pushed there.
type OzonLinkState struct {
	ProductID   int64
	OfferID     string
	StockPushed int64
}

// OzonLinksByProducts returns the link rows for the given products, so an unlink
// can tell what still has to be zeroed on the platform before it disappears.
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
// The tab joins this against the cabinet's own article list: counting on the
// hundred rows currently on screen would answer a question nobody asked, while
// "12 of 24 000 can be published" is the one that explains the tab.
//
// ponytail: the whole catalogue in memory — 24 000 short strings read once when
// the tab opens. An IN (…) against the platform's list would need thousands of
// bound parameters and buys nothing at this size.
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
		// A duplicate article keeps the linked side: the tab must not offer to
		// link an article that already is.
		linked[sku] = linked[sku] || isLinked
	}
	return ids, linked, rows.Err()
}
