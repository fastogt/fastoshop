package database

import "time"

// OzonPriceRow is a link whose Ozon price is due for a push. Price is the price
// ON OZON in kopecks — the owner's own number, not products.price.
type OzonPriceRow struct {
	ProductID   int64
	OfferID     string
	Price       int64
	PricePushed int64
	Error       string
}

// kOzonPriceGuard: price = 0 means the owner never opted this product in, and we
// never touch a price we were not asked to manage. price_pushed = -1 is "no
// baseline yet", so the first push of a set price always goes.
// The comparison rounds the wanted price up to whole rubles first, because that
// is what Ozon accepts and therefore what price_pushed holds — comparing raw
// kopecks against a rounded value would mark every such row as changed forever.
const kOzonPriceGuard = `offer_id != '' AND price > 0
	 AND (price + 99) / 100 * 100 != price_pushed`

func (d *Database) OzonPriceToPush() ([]OzonPriceRow, error) {
	return d.ozonPriceRows(
		`SELECT product_id, offer_id, price, price_pushed, price_error
		 FROM ozon_links
		 WHERE ` + kOzonPriceGuard + `
		   AND (retry_at IS NULL OR retry_at <= CURRENT_TIMESTAMP)
		 ORDER BY product_id`)
}

func (d *Database) ListOzonPriceErrors() ([]OzonPriceRow, error) {
	return d.ozonPriceRows(
		`SELECT product_id, offer_id, price, price_pushed, price_error
		 FROM ozon_links WHERE price_error != '' ORDER BY product_id`)
}

func (d *Database) ozonPriceRows(query string) ([]OzonPriceRow, error) {
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonPriceRow
	for rows.Next() {
		var r OzonPriceRow
		if err := rows.Scan(&r.ProductID, &r.OfferID, &r.Price, &r.PricePushed, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkOzonPricePushed stores the value actually sent (rounded up to whole
// rubles and back to kopecks), not what the owner typed: comparing the wanted
// price against the sent one is what keeps the row from flapping every pass.
func (d *Database) MarkOzonPricePushed(productID, level int64) error {
	_, err := d.db.Exec(
		`UPDATE ozon_links SET price_pushed=?, price_error='', retry_at=NULL
		 WHERE product_id=?`, level, productID)
	return err
}

// MarkOzonPriceError writes retry_at in the same UTC format as the stock side —
// the column is shared, and so is the comparison against CURRENT_TIMESTAMP.
func (d *Database) MarkOzonPriceError(productID int64, msg string, retryAt time.Time) error {
	_, err := d.db.Exec(
		`UPDATE ozon_links SET price_error=?, retry_at=? WHERE product_id=?`,
		msg, retryAt.UTC().Format("2006-01-02 15:04:05"), productID)
	return err
}

func (d *Database) CountOzonPriceState() (pending, failed int, err error) {
	err = d.db.QueryRow(
		`SELECT
		   (SELECT COUNT(*) FROM ozon_links WHERE `+kOzonPriceGuard+`),
		   (SELECT COUNT(*) FROM ozon_links WHERE price_error != '')`).
		Scan(&pending, &failed)
	return pending, failed, err
}

// SetOzonPrice returns false when there is no link for that product: setting a
// platform price for something we never linked is a mistake worth reporting,
// not a row worth creating. Clearing price_error is deliberate — an edit is the
// owner's attempt to fix whatever Ozon complained about, and the stale message
// must not outlive it.
func (d *Database) SetOzonPrice(productID, price int64) (bool, error) {
	res, err := d.db.Exec(
		`UPDATE ozon_links SET price=?, price_error='' WHERE product_id=?`, price, productID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// FillOzonPrices sets the Ozon price from the shelf price plus a markup in basis
// points, and only for linked rows that have none yet: prices the owner already
// chose are never overwritten by a bulk helper. Arithmetic stays in integer
// kopecks and rounds up, exactly like a single push does.
func (d *Database) FillOzonPrices(markupBP int64) (int, error) {
	res, err := d.db.Exec(
		`UPDATE ozon_links SET price = (
		   SELECT (p.price * (10000 + ?) + 9999) / 10000
		   FROM products p WHERE p.id = ozon_links.product_id)
		 WHERE price = 0 AND offer_id != ''
		   AND EXISTS (SELECT 1 FROM products p
		               WHERE p.id = ozon_links.product_id AND p.price > 0)`, markupBP)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// OzonLinkRow is one line of the linked-products table on the tab. Title and SKU
// are empty for a link whose product is gone — the row still matters, it is the
// card we keep zeroing out on the platform.
type OzonLinkRow struct {
	ProductID   int64
	OfferID     string
	Title       string
	SKU         string
	Stock       int64
	ShopPrice   int64
	Price       int64
	StockPushed int64
	PricePushed int64
	StockError  string
	PriceError  string
}

func (d *Database) CountOzonLinkRows() (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM ozon_links`).Scan(&n)
	return n, err
}

func (d *Database) ListOzonLinksPage(limit, offset int) ([]OzonLinkRow, error) {
	rows, err := d.db.Query(
		`SELECT l.product_id, l.offer_id, COALESCE(p.title, ''), COALESCE(p.sku, ''),
		        MAX(COALESCE(p.stock, 0), 0), COALESCE(p.price, 0),
		        l.price, l.stock_pushed, l.price_pushed, l.stock_error, l.price_error
		 FROM ozon_links l LEFT JOIN products p ON p.id = l.product_id
		 ORDER BY l.product_id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonLinkRow
	for rows.Next() {
		var r OzonLinkRow
		if err := rows.Scan(&r.ProductID, &r.OfferID, &r.Title, &r.SKU, &r.Stock,
			&r.ShopPrice, &r.Price, &r.StockPushed, &r.PricePushed,
			&r.StockError, &r.PriceError); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
