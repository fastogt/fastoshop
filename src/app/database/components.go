package database

import (
	"database/sql"
	"errors"
)

// ErrNested refuses a set inside a set: the stock triggers recalculate one level only.
var ErrNested = errors.New("a set cannot be part of another set")

// Component is one line of a set: which single product and how many of it.
type Component struct {
	ProductID int64  `json:"product_id"`
	Qty       int    `json:"qty"`
	Title     string `json:"title"`
	SKU       string `json:"sku"`
	Slug      string `json:"slug"`
	Stock     int    `json:"stock"`
	Hidden    bool   `json:"hidden"`
}

// SetRef names a set a product is part of, for the storefront's links.
type SetRef struct {
	Title string
	Slug  string
}

// kSetStock is how many whole sets the components allow; the only writer of a set's stock.
const kSetStock = `stock = (SELECT COALESCE(MIN(MAX(u.stock, 0) / c.qty), 0)
		FROM product_components c JOIN products u ON u.id = c.product_id
		WHERE c.set_id = products.id),
	updated_at = CURRENT_TIMESTAMP`

// kIsSet is true for a product with a composition, packed or put together at sale.
const kIsSet = `EXISTS (SELECT 1 FROM product_components WHERE set_id = products.id)`

// kDerived is true when the stock belongs to the triggers: a set put together at sale.
const kDerived = `(products.packed = 0 AND ` + kIsSet + `)`

// recursive_triggers is off in SQLite, so a set's own update does not re-enter the trigger.
const kComponentsSchema = `
	CREATE TABLE IF NOT EXISTS product_components (
		set_id     INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
		product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
		qty        INTEGER NOT NULL CHECK (qty >= 1),
		PRIMARY KEY (set_id, product_id)
	);
	CREATE INDEX IF NOT EXISTS idx_product_components_product ON product_components(product_id);
	CREATE TRIGGER IF NOT EXISTS trg_set_stock_on_unit AFTER UPDATE OF stock ON products
	BEGIN
		UPDATE products SET ` + kSetStock + `
		WHERE id IN (SELECT set_id FROM product_components WHERE product_id = NEW.id) AND packed = 0;
	END;
	CREATE TRIGGER IF NOT EXISTS trg_set_stock_on_add AFTER INSERT ON product_components
	BEGIN
		UPDATE products SET ` + kSetStock + ` WHERE id = NEW.set_id AND packed = 0;
	END;
	CREATE TRIGGER IF NOT EXISTS trg_set_stock_on_unpack AFTER UPDATE OF packed ON products
	WHEN NEW.packed = 0
	BEGIN
		UPDATE products SET ` + kSetStock + ` WHERE id = NEW.id AND ` + kIsSet + `;
	END;
	CREATE TRIGGER IF NOT EXISTS trg_set_stock_on_remove AFTER DELETE ON product_components
	BEGIN
		UPDATE products SET ` + kSetStock + ` WHERE id = OLD.set_id AND packed = 0;
	END;`

// ListComponents is a set's composition with each component's current stock.
func (d *Database) ListComponents(setID int64) ([]Component, error) {
	rows, err := d.db.Query(
		`SELECT c.product_id, c.qty, p.title, p.sku, p.slug, p.stock, p.hidden
		 FROM product_components c JOIN products p ON p.id = c.product_id
		 WHERE c.set_id = ? ORDER BY p.title`, setID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Component{}
	for rows.Next() {
		var c Component
		if err := rows.Scan(&c.ProductID, &c.Qty, &c.Title, &c.SKU, &c.Slug, &c.Stock, &c.Hidden); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetsOf lists the visible sets a product is part of: a hidden page answers 404.
func (d *Database) SetsOf(productID int64) ([]SetRef, error) {
	rows, err := d.db.Query(
		`SELECT p.title, p.slug FROM product_components c JOIN products p ON p.id = c.set_id
		 WHERE c.product_id = ? AND p.hidden = 0 ORDER BY p.title`, productID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SetRef
	for rows.Next() {
		var s SetRef
		if err := rows.Scan(&s.Title, &s.Slug); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetComponents replaces a composition; an empty list turns the set back into a product.
func (d *Database) SetComponents(setID int64, rows []Component) error {
	return d.withTx(func(tx *sql.Tx) error {
		if len(rows) > 0 {
			var usedInSet bool
			if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM product_components
				WHERE product_id = ?)`, setID).Scan(&usedInSet); err != nil {
				return err
			}
			if usedInSet {
				return ErrNested
			}
		}
		for _, r := range rows {
			var isSet bool
			if err := tx.QueryRow(`SELECT EXISTS (SELECT 1 FROM product_components
				WHERE set_id = ?)`, r.ProductID).Scan(&isSet); err != nil {
				return err
			}
			if isSet || r.ProductID == setID {
				return ErrNested
			}
		}
		if _, err := tx.Exec(`DELETE FROM product_components WHERE set_id = ?`, setID); err != nil {
			return err
		}
		for _, r := range rows {
			if _, err := tx.Exec(`INSERT INTO product_components (set_id, product_id, qty)
				VALUES (?, ?, ?)`, setID, r.ProductID, r.Qty); err != nil {
				return err
			}
		}
		return nil
	})
}

// unitItems turns sets put together at sale into components; anything else passes as is.
//
// ponytail: a cancel expands by today's composition, not the one at order time. Edit a set
// with open orders and the wrong units come back; the upgrade is storing the units taken.
func unitItems(tx *sql.Tx, items []OrderItem) ([]OrderItem, error) {
	var out []OrderItem
	for _, it := range items {
		parts, err := setParts(tx, it.ProductID)
		if err != nil {
			return nil, err
		}
		if len(parts) == 0 {
			out = append(out, it)
			continue
		}
		for _, p := range parts {
			out = append(out, OrderItem{ProductID: p.ProductID, Qty: it.Qty * p.Qty})
		}
	}
	return out, nil
}

func setParts(tx *sql.Tx, setID int64) ([]OrderItem, error) {
	rows, err := tx.Query(`SELECT c.product_id, c.qty FROM product_components c
		JOIN products s ON s.id = c.set_id AND s.packed = 0 WHERE c.set_id = ?`, setID)
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

// drainStock is the marketplace deduction: the sale already happened, so it floors at zero.
func drainStock(tx *sql.Tx, items []OrderItem) error {
	units, err := unitItems(tx, items)
	if err != nil {
		return err
	}
	for _, it := range units {
		if _, err := tx.Exec(
			`UPDATE products SET stock = MAX(0, stock - ?), updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`, it.Qty, it.ProductID); err != nil {
			return err
		}
	}
	return nil
}
