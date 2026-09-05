package database

import (
	"database/sql"
	"time"
)

type Order struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Phone string `json:"phone"`
	// At least one of Phone and Email is always filled.
	Email   string `json:"email"`
	Comment string `json:"comment"`
	// The snapshot stays on the server: the admin receives it parsed.
	ItemsJSON string    `json:"-"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateOrder leaves stock alone; the storefront uses CreateOrderWithStock instead.
func (d *Database) CreateOrder(o *Order) error {
	res, err := d.db.Exec(
		`INSERT INTO orders (name, phone, email, comment, items_json) VALUES (?, ?, ?, ?, ?)`,
		o.Name, o.Phone, o.Email, o.Comment, o.ItemsJSON)
	if err != nil {
		return err
	}
	o.ID, _ = res.LastInsertId()
	o.Status = "new"
	return nil
}

// SetOrderStatus moves stock too; stock_applied keeps it to once per direction.
// Re-activating a cancelled order may fail *OutOfStockError.
func (d *Database) SetOrderStatus(id int64, status string) error {
	return d.withTx(func(tx *sql.Tx) error {
		var current string
		var applied int
		if err := tx.QueryRow(
			`SELECT status, stock_applied FROM orders WHERE id=?`, id).Scan(&current, &applied); err != nil {
			return err
		}
		items, err := orderItems(tx, id)
		if err != nil {
			return err
		}
		switch {
		case status == "cancelled" && applied == 1:
			if err := returnStock(tx, items); err != nil {
				return err
			}
			applied = 0
		case current == "cancelled" && status != "cancelled" && applied == 0:
			if err := takeStock(tx, items); err != nil {
				return err
			}
			applied = 1
		}
		_, err = tx.Exec(`UPDATE orders SET status=?, stock_applied=? WHERE id=?`,
			status, applied, id)
		return err
	})
}

// kOrderSortable is the ORDER BY whitelist; no total, it lives inside items_json.
var kOrderSortable = map[string]string{
	"created": "created_at",
	"status":  "status",
	"name":    "name",
}

func (d *Database) CountOrders(status string) (int, error) {
	var n int
	var err error
	if status == "" {
		err = d.db.QueryRow(`SELECT COUNT(*) FROM orders`).Scan(&n)
	} else {
		err = d.db.QueryRow(`SELECT COUNT(*) FROM orders WHERE status=?`, status).Scan(&n)
	}
	return n, err
}

// ListOrdersPage is the admin list; the unpaginated ListOrders stays for the CSV export.
func (d *Database) ListOrdersPage(status, sort string, desc bool, limit, offset int) ([]Order, error) {
	where := ""
	args := []any{}
	if status != "" {
		where = " WHERE status=?"
		args = append(args, status)
	}
	args = append(args, limit, offset)
	return d.scanOrders(`SELECT id, name, phone, email, comment, items_json, status, created_at
		 FROM orders`+where+orderBy(kOrderSortable, sort, desc)+` LIMIT ? OFFSET ?`, args...)
}

func (d *Database) ListOrders() ([]Order, error) {
	return d.scanOrders(`SELECT id, name, phone, email, comment, items_json, status, created_at
		 FROM orders ORDER BY created_at DESC, id DESC`)
}

func (d *Database) scanOrders(query string, args ...any) ([]Order, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.Name, &o.Phone, &o.Email, &o.Comment, &o.ItemsJSON,
			&o.Status, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// DeleteOrders erases orders with their items; stock is not returned - cancel first.
func (d *Database) DeleteOrders(ids []int64) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders, args := inClause(ids)
	res, err := d.db.Exec(`DELETE FROM orders WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
