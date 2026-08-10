package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type Product struct {
	ID          int64  `json:"id"`
	SKU         string `json:"sku"`
	Title       string `json:"title"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Price       int64  `json:"price"` // минорные единицы
	// SourcePrice is what the feed charged, kept so the shelf price can be
	// recomputed from scratch; PriceManual marks a price the owner typed, which
	// a recompute leaves alone.
	SourcePrice int64  `json:"source_price"`
	PriceManual bool   `json:"price_manual"`
	Currency    string `json:"currency"`
	Stock       int    `json:"stock"`
	Category    string `json:"category"`
	// Supplier is the group that owns this product; empty means the owner made it
	// by hand and no feed may touch it.
	Supplier string `json:"supplier"`
	// Hidden only governs the storefront; whether the product is published to a
	// marketplace is the channel tab's business.
	Hidden    bool      `json:"hidden"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// uniqueSlug добирает суффикс -2, -3… пока не найдёт свободный.
func (d *Database) uniqueSlug(base string) (string, error) {
	slug := base
	for n := 2; ; n++ {
		var id int64
		err := d.db.QueryRow(`SELECT id FROM products WHERE slug = ?`, slug).Scan(&id)
		if err == sql.ErrNoRows {
			return slug, nil
		}
		if err != nil {
			return "", err
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}
}

func (d *Database) CreateProduct(p *Product) error {
	base := Slugify(p.Title)
	if base == "" {
		// Название из одной пунктуации ("!!!") даёт пустой слаг и товар
		// становится недостижим; uniqueSlug доберёт product-2, product-3…
		base = "product"
	}
	slug, err := d.uniqueSlug(base)
	if err != nil {
		return err
	}
	p.Slug = slug
	if p.Currency == "" {
		p.Currency = "RUB"
	}
	res, err := d.db.Exec(
		`INSERT INTO products (sku, title, slug, description, price, source_price,
		 price_manual, currency, stock, category, supplier, hidden)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.SKU, p.Title, p.Slug, p.Description, p.Price, p.SourcePrice,
		p.PriceManual, p.Currency, p.Stock, p.Category, p.Supplier, p.Hidden)
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// UpdateProduct intentionally never re-slugs: the slug is part of the public
// URL and already indexed by search engines, so it must stay stable once set.
func (d *Database) UpdateProduct(p *Product) error {
	_, err := d.db.Exec(
		`UPDATE products SET sku=?, title=?, description=?, price=?, source_price=?,
		 currency=?, stock=?, category=?, supplier=?, hidden=?, price_manual=?,
		 updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		p.SKU, p.Title, p.Description, p.Price, p.SourcePrice, p.Currency, p.Stock,
		p.Category, p.Supplier, p.Hidden, p.PriceManual, p.ID)
	return err
}

func (d *Database) DeleteProduct(id int64) error {
	_, err := d.db.Exec(`DELETE FROM products WHERE id=?`, id)
	return err
}

const kProductCols = `id, sku, title, slug, description, price, source_price,
	price_manual, currency, stock, category, supplier, hidden, created_at, updated_at`

func scanProduct(row interface{ Scan(...any) error }) (*Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.SKU, &p.Title, &p.Slug, &p.Description, &p.Price,
		&p.SourcePrice, &p.PriceManual, &p.Currency, &p.Stock, &p.Category,
		&p.Supplier, &p.Hidden, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (d *Database) GetProduct(id int64) (*Product, error) {
	return scanProduct(d.db.QueryRow(`SELECT `+kProductCols+` FROM products WHERE id=?`, id))
}

func (d *Database) GetProductBySlug(slug string) (*Product, error) {
	return scanProduct(d.db.QueryRow(`SELECT `+kProductCols+` FROM products WHERE slug=?`, slug))
}

// likePattern экранирует %, _ и сам escape-символ: без этого запрос «%» из
// поиска админки совпал бы со всем каталогом.
func likePattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

// supplierAny is what "no filter" looks like: an empty string is a real value
// (goods the owner made by hand), so it cannot double as "any".
const supplierAny = "\x00any"

func productWhere(category, q, supplier string, onlyVisible bool) (string, []any) {
	var conds []string
	var args []any
	if onlyVisible {
		conds = append(conds, `hidden=0`)
	}
	if supplier != supplierAny {
		conds = append(conds, `supplier=?`)
		args = append(args, supplier)
	}
	if category != "" {
		conds = append(conds, `category=?`)
		args = append(args, category)
	}
	if q != "" {
		conds = append(conds, `(title LIKE ? ESCAPE '\' OR sku LIKE ? ESCAPE '\')`)
		p := likePattern(q)
		args = append(args, p, p)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

func (d *Database) ListProducts(category string) ([]Product, error) {
	return d.ListProductsPage(category, "", supplierAny, -1, 0)
}

// The storefront reads through its own three functions rather than a boolean
// argument: a hidden product leaking into the catalogue or the sitemap is an SEO
// bug that no test at the call site would catch, and "Visible" in the name is
// harder to forget than a true.
// q is the buyer's search: the same substring match over title and article the
// admin uses, so a shop needs no second index to be searchable.
func (d *Database) ListVisibleProductsPage(category, q string, limit, offset int) ([]Product, error) {
	return d.listProducts(category, q, supplierAny, "", false, limit, offset, true)
}

func (d *Database) CountVisibleProducts(category, q string) (int, error) {
	return d.countProducts(category, q, supplierAny, true)
}

func (d *Database) GetVisibleProductBySlug(slug string) (*Product, error) {
	return scanProduct(d.db.QueryRow(
		`SELECT `+kProductCols+` FROM products WHERE slug=? AND hidden=0`, slug))
}

// ListProductsPage — страница каталога (витрина) или админки. limit < 0 —
// без ограничения (SQLite понимает LIMIT -1), q — подстрока по названию и SKU.
// kSortable is the whitelist of ORDER BY columns. Sorting happens in SQL, not in
// the browser: with 20 000 products a client-side sort would order the current
// page only and call it "sorted by price", which is worse than no sorting.
var kSortable = map[string]string{
	"title":   "title",
	"price":   "price",
	"stock":   "stock",
	"created": "created_at",
	"sku":     "sku",
}

// orderBy renders a safe ORDER BY. Unknown keys fall back to newest first, the
// order the catalogue had before sorting existed.
//
// id is always the last term, and that is not cosmetic: an import writes twenty
// thousand rows within one second, so created_at ties across the whole
// catalogue. Without a unique tiebreaker SQLite is free to order ties
// differently between queries, and paging would then repeat some rows and skip
// others.
func orderBy(sort string, desc bool) string {
	col, ok := kSortable[sort]
	if !ok {
		return " ORDER BY created_at DESC, id DESC"
	}
	if desc {
		return " ORDER BY " + col + " DESC, id DESC"
	}
	return " ORDER BY " + col + " ASC, id ASC"
}

// ListProductsPage takes supplier as a filter; pass AnySupplier for no filter.
func (d *Database) ListProductsPage(category, q, supplier string, limit, offset int) ([]Product, error) {
	return d.listProducts(category, q, supplier, "", false, limit, offset, false)
}

// ListProductsSorted is the admin list: same filters plus an explicit order.
func (d *Database) ListProductsSorted(q, supplier, sort string, desc bool, limit, offset int) ([]Product, error) {
	return d.listProducts("", q, supplier, sort, desc, limit, offset, false)
}

// AnySupplier disables the supplier filter.
const AnySupplier = supplierAny

func (d *Database) listProducts(category, q, supplier, sort string, desc bool, limit, offset int, onlyVisible bool) ([]Product, error) {
	where, args := productWhere(category, q, supplier, onlyVisible)
	args = append(args, limit, offset)
	rows, err := d.db.Query(`SELECT `+kProductCols+` FROM products`+where+
		orderBy(sort, desc)+` LIMIT ? OFFSET ?`, args...)
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

// Categories lists the storefront categories in use. Like suppliers, derived
// from the products: a category is a slug in the catalogue URL, not an entity
// with a life of its own.
func (d *Database) Categories() ([]string, error) {
	return d.distinct("category")
}

// Suppliers lists the groups in use. Derived from the products rather than kept
// in a table of its own: a group is just a name, and a CRUD screen for three
// rows earns nobody anything.
func (d *Database) Suppliers() ([]string, error) {
	return d.distinct("supplier")
}

// distinct is safe because the column name never comes from a request — the two
// callers pass a literal.
func (d *Database) distinct(column string) ([]string, error) {
	rows, err := d.db.Query(
		`SELECT DISTINCT ` + column + ` FROM products WHERE ` + column +
			` != '' ORDER BY ` + column)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *Database) CountProducts(category, q, supplier string) (int, error) {
	return d.countProducts(category, q, supplier, false)
}

func (d *Database) countProducts(category, q, supplier string, onlyVisible bool) (int, error) {
	where, args := productWhere(category, q, supplier, onlyVisible)
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM products`+where, args...).Scan(&n)
	return n, err
}
