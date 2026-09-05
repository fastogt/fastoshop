package database

import (
	"database/sql"
	"fmt"
)

// OrderItem - the working order line; name and price live in the items_json snapshot.
type OrderItem struct {
	ProductID int64
	Qty       int
}

// OutOfStockError carries the product the deduction tripped over, so it can be named.
type OutOfStockError struct {
	ProductID int64
	Title     string
}

func (e *OutOfStockError) Error() string {
	return fmt.Sprintf("out of stock: product %d (%s)", e.ProductID, e.Title)
}

// Name always yields something readable: the product may have been deleted.
func (e *OutOfStockError) Name() string {
	if e.Title == "" {
		return "товар"
	}
	return e.Title
}

// takeStock deducts with a conditional UPDATE: the DB settles the race, loser gets 0.
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

// orderItems reads every line before the first Exec: one connection, no open Rows.
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

// CreateOrderWithStock writes order and deduction in one tx; may fail *OutOfStockError.
func (d *Database) CreateOrderWithStock(o *Order, items []OrderItem) error {
	var id int64
	err := d.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO orders (name, phone, email, comment, items_json, stock_applied)
			 VALUES (?, ?, ?, ?, ?, 1)`, o.Name, o.Phone, o.Email, o.Comment, o.ItemsJSON)
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
