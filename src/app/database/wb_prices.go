package database

import (
	"database/sql"
	"time"
)

// WBPriceRow is a link due for a price push; Price is the Wildberries price in kopecks.
type WBPriceRow struct {
	ProductID   int64
	NmID        int64
	Barcode     string
	Price       int64
	PricePushed int64
	Error       string
	// RetryAt is when the row is due again.
	RetryAt sql.NullTime
}

// WBPriceSent is one upload row: what went on the wire, rounded up to whole roubles.
type WBPriceSent struct {
	ProductID int64
	Sent      int64
}

type WBPriceTask struct {
	UploadID  string
	CreatedAt time.Time
}

// kWBPriceGuard: price 0 is opt-out; a non-empty price_task means an upload in flight.
const kWBPriceGuard = `nm_id != 0 AND price > 0 AND price_task = ''
	 AND (price + 99) / 100 * 100 != price_pushed`

func (d *Database) WBPriceToPush() ([]WBPriceRow, error) {
	return d.wbPriceRows(
		`WHERE ` + kWBPriceGuard + `
		   AND (retry_at IS NULL OR retry_at <= CURRENT_TIMESTAMP)
		 ORDER BY nm_id, product_id`)
}

func (d *Database) ListWBPriceErrors() ([]WBPriceRow, error) {
	return d.wbPriceRows(`WHERE price_error != '' ORDER BY product_id`)
}

func (d *Database) wbPriceRows(where string) ([]WBPriceRow, error) {
	rows, err := d.db.Query(
		`SELECT product_id, nm_id, barcode, price, price_pushed, price_error, retry_at
		 FROM wb_links ` + where)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WBPriceRow
	for rows.Next() {
		var r WBPriceRow
		if err := rows.Scan(&r.ProductID, &r.NmID, &r.Barcode, &r.Price,
			&r.PricePushed, &r.Error, &r.RetryAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkWBPriceSent records the upload and the rows it carries in one transaction.
func (d *Database) MarkWBPriceSent(uploadID string, at time.Time, sent []WBPriceSent) error {
	return d.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT INTO wb_price_tasks (upload_id, created_at) VALUES (?, ?)
			 ON CONFLICT(upload_id) DO UPDATE SET created_at=excluded.created_at`,
			uploadID, at.UTC().Format(time.DateTime)); err != nil {
			return err
		}
		for _, s := range sent {
			if _, err := tx.Exec(
				`UPDATE wb_links SET price_task=?, price_sent=? WHERE product_id=?`,
				uploadID, s.Sent, s.ProductID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *Database) WBPriceTasks() ([]WBPriceTask, error) {
	rows, err := d.db.Query(
		`SELECT upload_id, created_at FROM wb_price_tasks ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WBPriceTask
	for rows.Next() {
		var t WBPriceTask
		if err := rows.Scan(&t.UploadID, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// MarkWBPriceTaskDone makes price_sent the new baseline the guard compares against.
func (d *Database) MarkWBPriceTaskDone(uploadID string) error {
	return d.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`UPDATE wb_links SET price_pushed=price_sent, price_task='', price_error='',
			   retry_at=NULL
			 WHERE price_task=?`, uploadID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM wb_price_tasks WHERE upload_id=?`, uploadID)
		return err
	})
}

// MarkWBPriceTaskFailed releases the task's rows; byNm holds per-card messages.
func (d *Database) MarkWBPriceTaskFailed(uploadID string, byNm map[int64]string,
	fallback string, retryAt time.Time) error {
	at := retryAt.UTC().Format(time.DateTime)
	return d.withTx(func(tx *sql.Tx) error {
		for nm, msg := range byNm {
			if _, err := tx.Exec(
				`UPDATE wb_links SET price_error=?, price_task='', retry_at=?
				 WHERE price_task=? AND nm_id=?`, msg, at, uploadID, nm); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(
			`UPDATE wb_links SET price_error=?, price_task='', retry_at=?
			 WHERE price_task=?`, fallback, at, uploadID); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM wb_price_tasks WHERE upload_id=?`, uploadID)
		return err
	})
}

// MarkWBCardError stamps all rows of a card: price lives on the card, not the size.
func (d *Database) MarkWBCardError(nmIDs []int64, msg string, retryAt time.Time) error {
	at := retryAt.UTC().Format(time.DateTime)
	return d.withTx(func(tx *sql.Tx) error {
		for _, nm := range nmIDs {
			if _, err := tx.Exec(
				`UPDATE wb_links SET price_error=?, retry_at=? WHERE nm_id=?`,
				msg, at, nm); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *Database) CountWBPriceState() (pending, inFlight, failed int, err error) {
	err = d.db.QueryRow(
		`SELECT
		   (SELECT COUNT(*) FROM wb_links WHERE `+kWBPriceGuard+`),
		   (SELECT COUNT(*) FROM wb_links WHERE price_task != ''),
		   (SELECT COUNT(*) FROM wb_links WHERE price_error != '')`).
		Scan(&pending, &inFlight, &failed)
	return pending, inFlight, failed, err
}

// SetWBPrice reports false when the product has no link, and clears price_error.
func (d *Database) SetWBPrice(productID, price int64) (bool, error) {
	res, err := d.db.Exec(
		`UPDATE wb_links SET price=?, price_error='' WHERE product_id=?`, price, productID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// FillWBPrices sets only empty prices: the shelf price plus a markup in basis points.
func (d *Database) FillWBPrices(markupBP int64) (int, error) {
	res, err := d.db.Exec(
		`UPDATE wb_links SET price = (
		   SELECT (p.price * (10000 + ?) + 9999) / 10000
		   FROM products p WHERE p.id = wb_links.product_id)
		 WHERE price = 0 AND nm_id != 0
		   AND EXISTS (SELECT 1 FROM products p
		               WHERE p.id = wb_links.product_id AND p.price > 0)`, markupBP)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// WBLinkRow is one line of the linked-products table; Title and SKU may be empty.
type WBLinkRow struct {
	ProductID   int64
	NmID        int64
	Barcode     string
	VendorCode  string
	Title       string
	SKU         string
	Stock       int64
	ShopPrice   int64
	Price       int64
	StockPushed int64
	PricePushed int64
	InFlight    bool
	StockError  string
	PriceError  string
}

func (d *Database) CountWBLinkRows() (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM wb_links`).Scan(&n)
	return n, err
}

func (d *Database) ListWBLinksPage(limit, offset int) ([]WBLinkRow, error) {
	rows, err := d.db.Query(
		`SELECT l.product_id, l.nm_id, l.barcode, l.vendor_code,
		        COALESCE(p.title, ''), COALESCE(p.sku, ''),
		        MAX(COALESCE(p.stock, 0), 0), COALESCE(p.price, 0),
		        l.price, l.stock_pushed, l.price_pushed, l.price_task != '',
		        l.stock_error, l.price_error
		 FROM wb_links l LEFT JOIN products p ON p.id = l.product_id
		 ORDER BY l.product_id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WBLinkRow
	for rows.Next() {
		var r WBLinkRow
		if err := rows.Scan(&r.ProductID, &r.NmID, &r.Barcode, &r.VendorCode,
			&r.Title, &r.SKU, &r.Stock, &r.ShopPrice, &r.Price,
			&r.StockPushed, &r.PricePushed, &r.InFlight,
			&r.StockError, &r.PriceError); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
