package database

import (
	"database/sql"
	"time"
)

type Order struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Comment   string    `json:"comment"`
	ItemsJSON string    `json:"items_json"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (d *Database) CreateOrder(o *Order) error {
	res, err := d.db.Exec(
		`INSERT INTO orders (name, phone, comment, items_json) VALUES (?, ?, ?, ?)`,
		o.Name, o.Phone, o.Comment, o.ItemsJSON)
	if err != nil {
		return err
	}
	o.ID, _ = res.LastInsertId()
	o.Status = "new"
	return nil
}

// SetOrderStatus двигает остаток вместе со статусом: отмена возвращает товар на
// склад, возврат в работу списывает его заново. Флаг stock_applied гарантирует,
// что переключение туда-сюда двигает остаток ровно один раз в каждую сторону, а
// заказы из времён без учёта (stock_applied = 0) не возвращают ничего.
// Возврат в работу может не состояться: тогда *OutOfStockError и статус не
// меняется — обещать покупателю товар, которого нет, нельзя.
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

// kOrderSortable is the ORDER BY whitelist. The order total is not here on
// purpose: it lives inside items_json, and sorting by it would mean either
// parsing every row in Go or teaching SQLite to read JSON — neither is worth it
// while a shop has thousands of orders, not millions.
var kOrderSortable = map[string]string{
	"created": "created_at",
	"status":  "status",
	"name":    "name",
}

// id is the tiebreaker for the same reason as in products: orders placed within
// one second would otherwise page unstably.
func orderOrderBy(sort string, desc bool) string {
	col, ok := kOrderSortable[sort]
	if !ok {
		return " ORDER BY created_at DESC, id DESC"
	}
	if desc {
		return " ORDER BY " + col + " DESC, id DESC"
	}
	return " ORDER BY " + col + " ASC, id ASC"
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

// ListOrdersPage is the admin list. The unpaginated variant stays for the CSV
// export, which genuinely needs every row.
func (d *Database) ListOrdersPage(status, sort string, desc bool, limit, offset int) ([]Order, error) {
	where := ""
	args := []any{}
	if status != "" {
		where = " WHERE status=?"
		args = append(args, status)
	}
	args = append(args, limit, offset)
	return d.scanOrders(`SELECT id, name, phone, comment, items_json, status, created_at
		 FROM orders`+where+orderOrderBy(sort, desc)+` LIMIT ? OFFSET ?`, args...)
}

func (d *Database) ListOrders() ([]Order, error) {
	return d.scanOrders(`SELECT id, name, phone, comment, items_json, status, created_at
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
		if err := rows.Scan(&o.ID, &o.Name, &o.Phone, &o.Comment, &o.ItemsJSON,
			&o.Status, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
