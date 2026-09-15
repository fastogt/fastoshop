package database

import "time"

// OzonPriceRow is a link due for a price push; Price is the Ozon price in kopecks.
type OzonPriceRow struct {
	ProductID   int64
	OfferID     string
	Price       int64
	PricePushed int64
	Error       string
}

// price = 0 is "not managed here", never pushed; price_pushed = -1 is no baseline.
const kOzonPriceGuard = `offer_id != '' AND price > 0
	 AND price != price_pushed`

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

// MarkOzonPricePushed stores the value actually sent, so the row stops flapping.
func (d *Database) MarkOzonPricePushed(productID, level int64) error {
	_, err := d.db.Exec(
		`UPDATE ozon_links SET price_pushed=?, price_error='', retry_at=NULL
		 WHERE product_id=?`, level, productID)
	return err
}

// MarkOzonPriceError writes retry_at as UTC - the column is shared with the stock side.
func (d *Database) MarkOzonPriceError(productID int64, msg string, retryAt time.Time) error {
	_, err := d.db.Exec(
		`UPDATE ozon_links SET price_error=?, retry_at=? WHERE product_id=?`,
		msg, retryAt.UTC().Format(time.DateTime), productID)
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

// SetOzonPrice returns false when the product has no link; it also clears price_error.
func (d *Database) SetOzonPrice(productID, price int64) (bool, error) {
	res, err := d.db.Exec(
		`UPDATE ozon_links SET price=?, price_error='' WHERE product_id=?`, price, productID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// FillOzonPrices marks up the shelf price only where no Ozon price was set yet.
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

// OzonLinkRow is one linked-products row; Title and SKU are empty for a gone product.
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
