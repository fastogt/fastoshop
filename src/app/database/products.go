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
	Price       int64  `json:"price"` // minor units
	// SourcePrice is what the feed charged, kept so the shelf price can be
	// recomputed from scratch; PriceManual marks a price the owner typed, which
	// a recompute leaves alone.
	SourcePrice int64  `json:"source_price"`
	PriceManual bool   `json:"price_manual"`
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

// uniqueSlug keeps trying suffixes -2, -3… until it finds a free one.
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
		// A title made of pure punctuation ("!!!") yields an empty slug and the
		// product becomes unreachable; uniqueSlug will pick product-2, product-3…
		base = "product"
	}
	slug, err := d.uniqueSlug(base)
	if err != nil {
		return err
	}
	p.Slug = slug
	res, err := d.db.Exec(
		`INSERT INTO products (sku, title, slug, description, price, source_price,
		 price_manual, stock, category, supplier, hidden)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.SKU, p.Title, p.Slug, p.Description, p.Price, p.SourcePrice,
		p.PriceManual, p.Stock, p.Category, p.Supplier, p.Hidden)
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
		 stock=?, category=?, supplier=?, hidden=?, price_manual=?,
		 updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		p.SKU, p.Title, p.Description, p.Price, p.SourcePrice, p.Stock,
		p.Category, p.Supplier, p.Hidden, p.PriceManual, p.ID)
	return err
}

func (d *Database) DeleteProduct(id int64) error {
	_, err := d.db.Exec(`DELETE FROM products WHERE id=?`, id)
	return err
}

const kProductCols = `id, sku, title, slug, description, price, source_price,
	price_manual, stock, category, supplier, hidden, created_at, updated_at`

func scanProduct(row interface{ Scan(...any) error }) (*Product, error) {
	var p Product
	err := row.Scan(&p.ID, &p.SKU, &p.Title, &p.Slug, &p.Description, &p.Price,
		&p.SourcePrice, &p.PriceManual, &p.Stock, &p.Category,
		&p.Supplier, &p.Hidden, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (d *Database) GetProduct(id int64) (*Product, error) {
	return scanProduct(d.db.QueryRow(`SELECT `+kProductCols+` FROM products WHERE id=?`, id))
}

// likePattern escapes %, _ and the escape character itself: without this a "%"
// query from the admin search would match the entire catalog.
func likePattern(q string) string {
	return "%" + likeEscape(q) + "%"
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// CategorySep joins the segments of a category path. A category is a path, not a
// name: "Текстиль/Текстиль для спальни/КПБ Евро".
const CategorySep = "/"

// supplierAny is what "no filter" looks like: an empty string is a real value
// (goods the owner made by hand), so it cannot double as "any".
const supplierAny = "\x00any"

// inClause renders the placeholders and args of an `IN (...)` filter.
func inClause(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}

// CatalogFilter is what the buyer chose on the storefront: where they are in
// the tree, what they typed, how they want it ordered and whether they want to
// see what is out of stock. A struct rather than five positional arguments,
// half of them empty at every call site.
type CatalogFilter struct {
	Category string
	Query    string
	// Sort is a key of kCatalogSortable; anything else means the shop's own order.
	Sort    string
	Desc    bool
	InStock bool
}

// kCatalogSortable — the orders a buyer may ask for. Sorting runs in SQL over
// the whole catalogue, not over the page in the browser: 60 cards sorted out of
// 20 000 would be a lie.
var kCatalogSortable = map[string]string{
	"price": "price",
	"title": "title",
	"new":   "created_at",
}

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
		// A category is a path, so a node covers its descendants too: "Текстиль"
		// shows everything under "Текстиль/Спальня/КПБ". Without this a parent
		// page would be empty while its children hold the whole catalogue.
		conds = append(conds, `(category=? OR category LIKE ? ESCAPE '\')`)
		args = append(args, category, likeEscape(category)+CategorySep+`%`)
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

// ListProducts returns the whole catalogue (LIMIT -1 is SQLite for "no limit");
// the import diff genuinely needs every row.
func (d *Database) ListProducts() ([]Product, error) {
	return d.listProducts("", "", supplierAny, "", false, -1, 0, false)
}

// The storefront reads through its own three functions rather than a boolean
// argument: a hidden product leaking into the catalogue or the sitemap is an SEO
// bug that no test at the call site would catch, and "Visible" in the name is
// harder to forget than a true.
// q is the buyer's search: the same substring match over title and article the
// admin uses, so a shop needs no second index to be searchable.
func (d *Database) ListVisibleProductsPage(f CatalogFilter, limit, offset int) ([]Product, error) {
	where, args := productWhere(f.Category, f.Query, supplierAny, true)
	where = withStock(where, f.InStock)
	args = append(args, limit, offset)
	return d.queryProducts(`SELECT `+kProductCols+` FROM products`+where+
		orderBy(kCatalogSortable, f.Sort, f.Desc)+` LIMIT ? OFFSET ?`, args...)
}

func (d *Database) CountVisibleProducts(f CatalogFilter) (int, error) {
	where, args := productWhere(f.Category, f.Query, supplierAny, true)
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM products`+withStock(where, f.InStock), args...).Scan(&n)
	return n, err
}

// withStock hides what cannot be bought today. Out of stock is a filter, not a
// separate query: the same page with one condition more.
func withStock(where string, inStock bool) string {
	if !inStock {
		return where
	}
	if where == "" {
		return " WHERE stock > 0"
	}
	return where + " AND stock > 0"
}

func (d *Database) GetVisibleProductBySlug(slug string) (*Product, error) {
	return scanProduct(d.db.QueryRow(
		`SELECT `+kProductCols+` FROM products WHERE slug=? AND hidden=0`, slug))
}

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

// orderBy renders a safe ORDER BY from a whitelist. Unknown keys fall back to
// newest first, the order the lists had before sorting existed.
//
// id is always the last term, and that is not cosmetic: an import writes twenty
// thousand rows within one second, so created_at ties across the whole
// catalogue. Without a unique tiebreaker SQLite is free to order ties
// differently between queries, and paging would then repeat some rows and skip
// others.
func orderBy(whitelist map[string]string, sort string, desc bool) string {
	col, ok := whitelist[sort]
	if !ok {
		return " ORDER BY created_at DESC, id DESC"
	}
	if desc {
		return " ORDER BY " + col + " DESC, id DESC"
	}
	return " ORDER BY " + col + " ASC, id ASC"
}

// ListProductsSorted is the admin list: filters plus an explicit order.
func (d *Database) ListProductsSorted(q, supplier, sort string, desc bool, limit, offset int) ([]Product, error) {
	return d.listProducts("", q, supplier, sort, desc, limit, offset, false)
}

// AnySupplier disables the supplier filter.
const AnySupplier = supplierAny

func (d *Database) listProducts(category, q, supplier, sort string, desc bool, limit, offset int, onlyVisible bool) ([]Product, error) {
	where, args := productWhere(category, q, supplier, onlyVisible)
	args = append(args, limit, offset)
	return d.queryProducts(`SELECT `+kProductCols+` FROM products`+where+
		orderBy(kSortable, sort, desc)+` LIMIT ? OFFSET ?`, args...)
}

func (d *Database) queryProducts(query string, args ...any) ([]Product, error) {
	rows, err := d.db.Query(query, args...)
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

func (d *Database) CountProducts(q, supplier string) (int, error) {
	return d.countProducts("", q, supplier, false)
}

func (d *Database) countProducts(category, q, supplier string, onlyVisible bool) (int, error) {
	where, args := productWhere(category, q, supplier, onlyVisible)
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM products`+where, args...).Scan(&n)
	return n, err
}

// ProductLink is what an order line needs to point at the goods it sold: the
// storefront page and the picture that identifies them at a glance.
type ProductLink struct {
	Slug  string
	Image string
}

// LinksBySKU maps the articles of an order's lines onto the products they refer
// to. A missing article is simply absent from the map: the order's snapshot
// outlives the product, and a line whose goods were deleted still has to render.
func (d *Database) LinksBySKU(skus []string) (map[string]ProductLink, error) {
	out := map[string]ProductLink{}
	if len(skus) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(skus)), ",")
	args := make([]any, len(skus))
	for i, s := range skus {
		args[i] = s
	}
	rows, err := d.db.Query(
		`SELECT p.sku, p.slug, COALESCE((SELECT i.path FROM product_images i
		     WHERE i.product_id = p.id ORDER BY i.position, i.id LIMIT 1), '')
		 FROM products p WHERE p.sku IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var sku string
		var link ProductLink
		if err := rows.Scan(&sku, &link.Slug, &link.Image); err != nil {
			return nil, err
		}
		out[sku] = link
	}
	return out, rows.Err()
}
