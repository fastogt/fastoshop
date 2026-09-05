package database

import (
	"fmt"
)

// Selection is what a bulk action applies to: explicit ids, or the owner's filter.
type Selection struct {
	IDs      []int64
	Q        string
	Supplier string
	// All switches from IDs to the filter, so an empty id list is never "everything".
	All bool
}

// where returns ok=false when the selection is empty: such a call must touch no rows.
func (s Selection) where() (string, []any, bool) {
	if s.All {
		w, args := productWhere("", s.Q, s.Supplier, false)
		return w, args, true
	}
	if len(s.IDs) == 0 {
		return "", nil, false
	}
	in, args := inClause(s.IDs)
	return fmt.Sprintf(" WHERE id IN (%s)", in), args, true
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

// SetStockBulk writes one stock level across the selection.
func (d *Database) SetStockBulk(s Selection, stock int) (int, error) {
	if stock < 0 {
		return 0, fmt.Errorf("stock cannot be negative: %d", stock)
	}
	return d.bulkUpdate(s, "stock=?", stock)
}

// SetHiddenBulk takes products off the storefront; channel links are untouched.
func (d *Database) SetHiddenBulk(s Selection, hidden bool) (int, error) {
	return d.bulkUpdate(s, "hidden=?", hidden)
}

// SetSupplierBulk moves products between supplier groups.
func (d *Database) SetSupplierBulk(s Selection, supplier string) (int, error) {
	return d.bulkUpdate(s, "supplier=?", supplier)
}

// DeleteProductsBulk takes an explicit list only: there is no delete-by-filter.
func (d *Database) DeleteProductsBulk(ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	in, args := inClause(ids)
	res, err := d.db.Exec(fmt.Sprintf(`DELETE FROM products WHERE id IN (%s)`, in), args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
