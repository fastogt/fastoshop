package database

import (
	"database/sql"
	"encoding/json"
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
	// SourcePrice is the feed's price; PriceManual marks a price a recompute leaves alone.
	SourcePrice int64  `json:"source_price"`
	PriceManual bool   `json:"price_manual"`
	Stock       int    `json:"stock"`
	Category    string `json:"category"`
	// Brand is the manufacturer, not the supplier.
	Brand string `json:"brand"`
	// Supplier owns this product; empty means hand-made and no feed may touch it.
	Supplier string `json:"supplier"`
	// Hidden governs the storefront only, not marketplace publication.
	Hidden bool `json:"hidden"`
	// Weight in grams and size in millimetres; an unweighed product is absent, not zero.
	WeightG  *int64 `json:"weight_g"`
	LengthMM *int64 `json:"length_mm"`
	WidthMM  *int64 `json:"width_mm"`
	HeightMM *int64 `json:"height_mm"`
	// Params keep the source's order; weight and size stay out, they have own columns.
	Params    []Param   `json:"params"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Param is one characteristic: Value keeps its source's JSON type, Name is the key.
type Param struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// ParamValueOK reports whether v is a JSON scalar or a flat list of them.
func ParamValueOK(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) != ""
	case float64, int, int64, bool:
		return true
	case []any:
		if len(x) == 0 {
			return false
		}
		for _, e := range x {
			if _, nested := e.([]any); nested || !ParamValueOK(e) {
				return false
			}
		}
		return true
	}
	return false
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

// slugBase falls back to "product": a punctuation-only title yields an empty slug.
func slugBase(title string) string {
	if base := Slugify(title); base != "" {
		return base
	}
	return "product"
}

// execer is what a write needs from either the pool or an open transaction.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (d *Database) CreateProduct(p *Product) error {
	slug, err := d.uniqueSlug(slugBase(p.Title))
	if err != nil {
		return err
	}
	p.Slug = slug
	return insertProduct(d.db, p)
}

func insertProduct(q execer, p *Product) error {
	res, err := q.Exec(
		`INSERT INTO products (sku, title, slug, description, price, source_price,
		 price_manual, stock, category, brand, supplier, hidden,
		 weight_g, length_mm, width_mm, height_mm, params)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.SKU, p.Title, p.Slug, p.Description, p.Price, p.SourcePrice,
		p.PriceManual, p.Stock, p.Category, p.Brand, p.Supplier, p.Hidden,
		p.WeightG, p.LengthMM, p.WidthMM, p.HeightMM, paramsJSON(p.Params))
	if err != nil {
		return err
	}
	p.ID, _ = res.LastInsertId()
	return nil
}

// UpdateProduct never re-slugs: the slug is a public, indexed URL and must stay put.
func (d *Database) UpdateProduct(p *Product) error { return updateProduct(d.db, p) }

func updateProduct(q execer, p *Product) error {
	_, err := q.Exec(
		`UPDATE products SET sku=?, title=?, description=?, price=?, source_price=?,
		 stock=?, category=?, brand=?, supplier=?, hidden=?, price_manual=?,
		 weight_g=?, length_mm=?, width_mm=?, height_mm=?, params=?,
		 updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		p.SKU, p.Title, p.Description, p.Price, p.SourcePrice, p.Stock,
		p.Category, p.Brand, p.Supplier, p.Hidden, p.PriceManual,
		p.WeightG, p.LengthMM, p.WidthMM, p.HeightMM, paramsJSON(p.Params), p.ID)
	return err
}

func (d *Database) DeleteProduct(id int64) error {
	_, err := d.db.Exec(`DELETE FROM products WHERE id=?`, id)
	return err
}

const kProductCols = `id, sku, title, slug, description, price, source_price,
	price_manual, stock, category, brand, supplier, hidden,
	weight_g, length_mm, width_mm, height_mm, params, created_at, updated_at`

func scanProduct(row interface{ Scan(...any) error }) (*Product, error) {
	var p Product
	var params string
	err := row.Scan(&p.ID, &p.SKU, &p.Title, &p.Slug, &p.Description, &p.Price,
		&p.SourcePrice, &p.PriceManual, &p.Stock, &p.Category, &p.Brand,
		&p.Supplier, &p.Hidden, &p.WeightG, &p.LengthMM, &p.WidthMM, &p.HeightMM,
		&params, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	// Unreadable params are dropped, not fatal: one bad row must not take a page down.
	p.Params = nil
	var stored []Param
	if err := json.Unmarshal([]byte(params), &stored); err == nil {
		for _, prm := range stored {
			if prm.Name != "" && ParamValueOK(prm.Value) {
				p.Params = append(p.Params, prm)
			}
		}
	}
	return &p, nil
}

// paramsJSON always yields a list, never NULL nor the "null" a nil slice marshals to.
func paramsJSON(v []Param) string {
	if len(v) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func (d *Database) GetProduct(id int64) (*Product, error) {
	return scanProduct(d.db.QueryRow(`SELECT `+kProductCols+` FROM products WHERE id=?`, id))
}

// likePattern escapes %, _ and the escape char, so a "%" query is not a wildcard.
func likePattern(q string) string {
	return "%" + likeEscape(q) + "%"
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// CategorySep joins the segments of a category path: a category is a path, not a name.
const CategorySep = "/"

// AnySupplier disables the supplier filter; "" is a real value (hand-made goods).
const AnySupplier = "\x00any"

// inClause renders the placeholders and args of an `IN (...)` filter.
func inClause[T any](vals []T) (string, []any) {
	args := make([]any, len(vals))
	for i, v := range vals {
		args[i] = v
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(vals)), ","), args
}

type CatalogFilter struct {
	Category string
	Query    string
	// Sort is a key of kCatalogSortable; anything else means the shop's own order.
	Sort    string
	Desc    bool
	InStock bool
}

// kCatalogSortable whitelists the orders a buyer may ask for; sorting runs in SQL.
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
	if supplier != AnySupplier {
		conds = append(conds, `supplier=?`)
		args = append(args, supplier)
	}
	if category != "" {
		// A category is a path: a node covers its descendants, or a parent page is empty.
		conds = append(conds, `(category=? OR category LIKE ? ESCAPE '\')`)
		args = append(args, category, likeEscape(category)+CategorySep+`%`)
	}
	// Every word must match title or article; ulower both sides - SQLite folds ASCII only.
	//
	// ponytail: a full scan per word - three words is three passes of the 46 ms
	// one pass already costs. FTS5 with the unicode61 tokenizer is the upgrade,
	// and it brings ranking, which this has never had; it also brings a virtual
	// table to keep in step with an import that writes 24 000 rows at once.
	for _, word := range strings.Fields(q) {
		conds = append(conds, `(ulower(title) LIKE ? ESCAPE '\' OR ulower(sku) LIKE ? ESCAPE '\')`)
		p := strings.ToLower(likePattern(word))
		args = append(args, p, p)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// ListProducts returns the whole catalogue (LIMIT -1 is SQLite for "no limit").
func (d *Database) ListProducts() ([]Product, error) {
	return d.listProducts("", "", AnySupplier, "", false, -1, 0, false)
}

// Visible-only reads have their own functions: a hidden product must never leak out.
func (d *Database) ListVisibleProductsPage(f CatalogFilter, limit, offset int) ([]Product, error) {
	where, args := productWhere(f.Category, f.Query, AnySupplier, true)
	where = withStock(where, f.InStock)
	args = append(args, limit, offset)
	return d.queryProducts(`SELECT `+kProductCols+` FROM products`+where+
		orderBy(kCatalogSortable, f.Sort, f.Desc)+` LIMIT ? OFFSET ?`, args...)
}

func (d *Database) CountVisibleProducts(f CatalogFilter) (int, error) {
	where, args := productWhere(f.Category, f.Query, AnySupplier, true)
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM products`+withStock(where, f.InStock), args...).Scan(&n)
	return n, err
}

// withStock hides what cannot be bought today.
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

// kSortable whitelists ORDER BY columns; sorting runs in SQL, not in the browser.
var kSortable = map[string]string{
	"title":   "title",
	"price":   "price",
	"stock":   "stock",
	"created": "created_at",
	// The admin sorts by this after an import, to see what actually moved.
	"updated": "updated_at",
	"sku":     "sku",
}

// orderBy renders a safe ORDER BY; id is last so ties cannot shuffle between pages.
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

// Categories lists the categories in use, derived from the products themselves.
func (d *Database) Categories() ([]string, error) {
	return d.distinct("category")
}

// Suppliers lists the groups in use, derived from the products themselves.
func (d *Database) Suppliers() ([]string, error) {
	return d.distinct("supplier")
}

// distinct interpolates the column name: callers must pass a literal, never input.
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
	where, args := productWhere("", q, supplier, false)
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM products`+where, args...).Scan(&n)
	return n, err
}

type ProductLink struct {
	Slug  string
	Image string
}

// LinksBySKU maps order-line articles to products; a deleted product is absent.
func (d *Database) LinksBySKU(skus []string) (map[string]ProductLink, error) {
	out := map[string]ProductLink{}
	if len(skus) == 0 {
		return out, nil
	}
	in, args := inClause(skus)
	rows, err := d.db.Query(
		`SELECT p.sku, p.slug, COALESCE((SELECT i.path FROM product_images i
		     WHERE i.product_id = p.id ORDER BY i.position, i.id LIMIT 1), '')
		 FROM products p WHERE p.sku IN (`+in+`)`, args...)
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
