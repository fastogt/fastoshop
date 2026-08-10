package database

import (
	"database/sql"
	"fmt"
)

// OrderItem — рабочая позиция заказа: связь с товаром и количество. Названия и
// цены здесь нет намеренно, их хранит снапшот orders.items_json.
type OrderItem struct {
	ProductID int64
	Qty       int
}

// OutOfStockError несёт товар, на котором споткнулось списание, чтобы витрина и
// админка могли назвать его покупателю и владельцу.
type OutOfStockError struct {
	ProductID int64
	Title     string
}

func (e *OutOfStockError) Error() string {
	return fmt.Sprintf("out of stock: product %d (%s)", e.ProductID, e.Title)
}

// Name всегда даёт что-то читаемое: товар мог быть удалён между рендером
// корзины и оформлением, тогда названия уже нет.
func (e *OutOfStockError) Name() string {
	if e.Title == "" {
		return "товар"
	}
	return e.Title
}

// takeStock списывает остаток условным UPDATE: гонка двух покупателей за
// последней единицей разрешается самой БД, проигравший получает 0 строк.
func takeStock(tx *sql.Tx, items []OrderItem) error {
	for _, it := range items {
		res, err := tx.Exec(
			`UPDATE products SET stock = stock - ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ? AND stock >= ?`, it.Qty, it.ProductID, it.Qty)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			var title string
			_ = tx.QueryRow(`SELECT title FROM products WHERE id = ?`, it.ProductID).Scan(&title)
			return &OutOfStockError{ProductID: it.ProductID, Title: title}
		}
	}
	return nil
}

func returnStock(tx *sql.Tx, items []OrderItem) error {
	for _, it := range items {
		if _, err := tx.Exec(
			`UPDATE products SET stock = stock + ?, updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`, it.Qty, it.ProductID); err != nil {
			return err
		}
	}
	return nil
}

// orderItems читает позиции целиком до первого Exec: соединение у транзакции
// одно, и держать открытый Rows во время записи нельзя.
// Удалённые товары (product_id IS NULL) выпадают — возвращать остаток некуда.
func orderItems(tx *sql.Tx, orderID int64) ([]OrderItem, error) {
	rows, err := tx.Query(
		`SELECT product_id, qty FROM order_items
		 WHERE order_id = ? AND product_id IS NOT NULL`, orderID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OrderItem
	for rows.Next() {
		var it OrderItem
		if err := rows.Scan(&it.ProductID, &it.Qty); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// CreateOrderWithStock создаёт заказ и списывает остаток одной транзакцией:
// если хотя бы одной позиции не хватило, не остаётся ни заказа, ни движения
// остатков. Возвращает *OutOfStockError в этом случае.
func (d *Database) CreateOrderWithStock(o *Order, items []OrderItem) error {
	var id int64
	err := d.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO orders (name, phone, comment, items_json, stock_applied)
			 VALUES (?, ?, ?, ?, 1)`, o.Name, o.Phone, o.Comment, o.ItemsJSON)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		for _, it := range items {
			if _, err := tx.Exec(
				`INSERT INTO order_items (order_id, product_id, qty) VALUES (?, ?, ?)`,
				id, it.ProductID, it.Qty); err != nil {
				return err
			}
		}
		return takeStock(tx, items)
	})
	if err != nil {
		return err
	}
	o.ID = id
	o.Status = "new"
	return nil
}
