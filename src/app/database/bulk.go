package database

import (
	"fmt"
	"strings"
)

// Selection is what a bulk action applies to: either explicit rows, or
// everything the owner's current filter shows. Twenty thousand rows cannot be
// ticked one by one, so "all matching" has to travel as a filter rather than as
// a list of ids.
type Selection struct {
	IDs      []int64
	Q        string
	Supplier string
	// All switches from IDs to the filter. Kept explicit so an empty id list
	// can never be mistaken for "everything".
	All bool
}

// where builds the clause for a selection. Returns ok=false when there is
// nothing to act on — a bulk call with neither ids nor the all flag must touch
// no rows at all.
func (s Selection) where() (string, []any, bool) {
	if s.All {
		w, args := productWhere("", s.Q, s.Supplier, false)
		return w, args, true
	}
	if len(s.IDs) == 0 {
		return "", nil, false
	}
	args := make([]any, len(s.IDs))
	for i, id := range s.IDs {
		args[i] = id
	}
	return fmt.Sprintf(" WHERE id IN (%s)",
		strings.TrimSuffix(strings.Repeat("?,", len(s.IDs)), ",")), args, true
}

func (d *Database) bulkUpdate(s Selection, set string, values ...any) (int, error) {
	where, args, ok := s.where()
	if !ok {
		return 0, nil
	}
	res, err := d.db.Exec(
		`UPDATE products SET `+set+`, updated_at=CURRENT_TIMESTAMP`+where,
		append(values, args...)...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// SetStockBulk writes one stock level across the selection. A YML feed carries
// only an availability flag, so a whole catalogue can land on zero and be
// unbuyable; correcting that row by row is not realistic at 20 000 products.
func (d *Database) SetStockBulk(s Selection, stock int) (int, error) {
	if stock < 0 {
		return 0, fmt.Errorf("stock cannot be negative: %d", stock)
	}
	return d.bulkUpdate(s, "stock=?", stock)
}

// SetHiddenBulk takes products off the storefront, or puts them back. The
// marketplace link is untouched: what a shop shows and what it sells on a
// channel are separate decisions.
func (d *Database) SetHiddenBulk(s Selection, hidden bool) (int, error) {
	return d.bulkUpdate(s, "hidden=?", hidden)
}

// SetSupplierBulk moves products between groups — for when an article changes
// hands from one supplier to another.
func (d *Database) SetSupplierBulk(s Selection, supplier string) (int, error) {
	return d.bulkUpdate(s, "supplier=?", supplier)
}

// DeleteProductsBulk takes an explicit list only. There is deliberately no
// "delete everything matching": one click erasing 20 000 products along with
// their indexed slugs and channel links is not a button worth having.
func (d *Database) DeleteProductsBulk(ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	res, err := d.db.Exec(fmt.Sprintf(`DELETE FROM products WHERE id IN (%s)`,
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")), args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
