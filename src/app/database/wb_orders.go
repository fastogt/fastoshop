package database

import (
	"database/sql"
	"fmt"
	"time"
)

// WBStatusCancelled collapses the platform's several cancelled spellings into one.
const WBStatusCancelled = "cancelled"

// WBOrder is one assembly task; Wildberries carries exactly one item per task.
type WBOrder struct {
	ID        int64
	OrderID   int64
	Status    string
	Cancelled bool
	ProductID *int64
	// Title of the shop product, filled on read only.
	Title   string
	Barcode string
	Article string
	NmID    int64
	Qty     int
	// Units is what the order took off the shop's stock: Qty times the card's pack size.
	Units     int
	Oversold  bool
	CreatedAt time.Time
}

func (o *WBOrder) storedStatus() string {
	if o.Cancelled {
		return WBStatusCancelled
	}
	return o.Status
}

// ApplyWBOrder applies a task once (UNIQUE(order_id)) and reports if stock moved.
func (d *Database) ApplyWBOrder(o *WBOrder) (moved bool, err error) {
	err = d.withTx(func(tx *sql.Tx) error {
		productID, pack, err := resolveBarcode(tx, o.Barcode)
		if err != nil {
			return err
		}
		units := 0
		if productID != nil && !o.Cancelled && o.Qty > 0 {
			units = o.Qty * pack
		}
		res, err := tx.Exec(
			`INSERT INTO wb_orders (order_id, status, product_id, barcode, article,
			   nm_id, qty, units, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(order_id) DO NOTHING`,
			o.OrderID, o.storedStatus(), productID, o.Barcode, o.Article, o.NmID,
			o.Qty, units, o.CreatedAt.UTC().Format(time.DateTime))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		if units == 0 {
			return nil
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		var have int
		if err := tx.QueryRow(
			`SELECT stock FROM products WHERE id = ?`, *productID).Scan(&have); err != nil {
			return err
		}
		if have < units {
			if _, err := tx.Exec(`UPDATE wb_orders SET oversold = 1 WHERE id = ?`, id); err != nil {
				return err
			}
		}
		// MAX(0, ...): the marketplace already sold, negative stock is worse than zero.
		if _, err := tx.Exec(
			`UPDATE products SET stock = MAX(0, stock - ?), updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`, units, *productID); err != nil {
			return err
		}
		moved = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return moved, nil
}

// SetWBOrderStatus returns stock only on the not cancelled -> cancelled transition.
func (d *Database) SetWBOrderStatus(orderID int64, status string, cancelled bool) (moved bool, err error) {
	err = d.withTx(func(tx *sql.Tx) error {
		var id int64
		var prev string
		var productID sql.NullInt64
		var units int
		err := tx.QueryRow(
			`SELECT id, status, product_id, units FROM wb_orders WHERE order_id = ?`,
			orderID).Scan(&id, &prev, &productID, &units)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		next := status
		if cancelled {
			next = WBStatusCancelled
		}
		if prev == next {
			return nil
		}
		if cancelled && prev != WBStatusCancelled && productID.Valid && units > 0 {
			if err := returnStock(tx, []OrderItem{{ProductID: productID.Int64, Qty: units}}); err != nil {
				return err
			}
			moved = true
		}
		_, err = tx.Exec(`UPDATE wb_orders SET status = ? WHERE id = ?`, next, id)
		return err
	})
	if err != nil {
		return false, err
	}
	return moved, nil
}

// resolveBarcode returns the linked product and its pack size; a nil product means no link.
func resolveBarcode(tx *sql.Tx, barcode string) (*int64, int, error) {
	if barcode == "" {
		return nil, 0, nil
	}
	var id int64
	var pack int
	err := tx.QueryRow(
		`SELECT product_id, qty FROM wb_links WHERE barcode = ?`, barcode).Scan(&id, &pack)
	if err == sql.ErrNoRows {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	return &id, pack, nil
}

// OpenWBOrderIDs returns the tasks whose status may still change.
func (d *Database) OpenWBOrderIDs(limit int) ([]int64, error) {
	rows, err := d.db.Query(
		`SELECT order_id FROM wb_orders WHERE status != ?
		 ORDER BY created_at DESC LIMIT ?`, WBStatusCancelled, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (d *Database) CountWBOrders() (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM wb_orders`).Scan(&n)
	return n, err
}

func (d *Database) CountWBOrderState() (total, oversold, unresolved int, err error) {
	err = d.db.QueryRow(
		`SELECT (SELECT COUNT(*) FROM wb_orders),
		        (SELECT COUNT(*) FROM wb_orders WHERE oversold = 1),
		        (SELECT COUNT(*) FROM wb_orders WHERE product_id IS NULL)`).
		Scan(&total, &oversold, &unresolved)
	return total, oversold, unresolved, err
}

func (d *Database) ListWBOrdersPage(limit, offset int) ([]WBOrder, error) {
	rows, err := d.db.Query(
		`SELECT o.id, o.order_id, o.status, o.product_id, o.barcode, o.article,
		        o.nm_id, o.qty, o.units, o.oversold, o.created_at, COALESCE(p.title, '')
		 FROM wb_orders o LEFT JOIN products p ON p.id = o.product_id
		 ORDER BY o.created_at DESC, o.id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WBOrder
	for rows.Next() {
		var o WBOrder
		var productID sql.NullInt64
		var title string
		if err := rows.Scan(&o.ID, &o.OrderID, &o.Status, &productID, &o.Barcode,
			&o.Article, &o.NmID, &o.Qty, &o.Units, &o.Oversold, &o.CreatedAt, &title); err != nil {
			return nil, err
		}
		if productID.Valid {
			id := productID.Int64
			o.ProductID = &id
		}
		o.Title = title
		out = append(out, o)
	}
	return out, rows.Err()
}

// WBOrdersSince returns the poll cursor; a zero time means never polled.
func (d *Database) WBOrdersSince() (time.Time, error) {
	var t time.Time
	err := d.db.QueryRow(`SELECT orders_since FROM wb_cursor WHERE id=1`).Scan(&t)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("read wb cursor: %w", err)
	}
	return t, nil
}

func (d *Database) SetWBOrdersSince(t time.Time) error {
	_, err := d.db.Exec(
		`INSERT INTO wb_cursor (id, orders_since) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET orders_since=excluded.orders_since`,
		t.UTC().Format(time.DateTime))
	return err
}
