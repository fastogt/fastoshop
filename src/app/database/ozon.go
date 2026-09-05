package database

import (
	"database/sql"
	"time"
)

// The cabinet's currency lives in settings.currency - a second copy is a second truth.
type OzonSettings struct {
	ClientID    string
	APIKey      string
	WarehouseID string
	Enabled     bool
}

type OzonLink struct {
	ProductID int64
	OfferID   string
}

// GetOzonSettings returns empty settings when no id=1 row exists yet.
func (d *Database) GetOzonSettings() (*OzonSettings, error) {
	var s OzonSettings
	err := d.db.QueryRow(
		`SELECT client_id, api_key, warehouse_id, enabled FROM ozon_settings WHERE id=1`).
		Scan(&s.ClientID, &s.APIKey, &s.WarehouseID, &s.Enabled)
	if err == sql.ErrNoRows {
		return &s, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *Database) SaveOzonSettings(s *OzonSettings) error {
	_, err := d.db.Exec(
		`INSERT INTO ozon_settings (id, client_id, api_key, warehouse_id, enabled)
		 VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET client_id=excluded.client_id,
		   api_key=excluded.api_key, warehouse_id=excluded.warehouse_id,
		   enabled=excluded.enabled`,
		s.ClientID, s.APIKey, s.WarehouseID, s.Enabled)
	return err
}

// UpsertOzonLink leaves price and *_pushed alone: re-linking must not reset sync state.
func (d *Database) UpsertOzonLink(l *OzonLink) error {
	_, err := d.db.Exec(
		`INSERT INTO ozon_links (product_id, offer_id) VALUES (?, ?)
		 ON CONFLICT(product_id) DO UPDATE SET offer_id=excluded.offer_id`,
		l.ProductID, l.OfferID)
	return err
}

func (d *Database) DeleteOzonLink(productID int64) error {
	_, err := d.db.Exec(`DELETE FROM ozon_links WHERE product_id=?`, productID)
	return err
}

// OzonStockRow is a link due for a stock push; a deleted product means zero.
type OzonStockRow struct {
	ProductID   int64
	OfferID     string
	Stock       int64
	StockPushed int64
	Error       string
}

// stock_pushed = -1 is "no baseline yet": the first push is always allowed.
const kOzonStockGuard = `l.offer_id != ''
	 AND (l.stock_pushed = -1 OR MAX(COALESCE(p.stock, 0), 0) != l.stock_pushed)`

func (d *Database) OzonStockToPush() ([]OzonStockRow, error) {
	rows, err := d.db.Query(
		`SELECT l.product_id, l.offer_id, MAX(COALESCE(p.stock, 0), 0),
		        l.stock_pushed, l.stock_error
		 FROM ozon_links l LEFT JOIN products p ON p.id = l.product_id
		 WHERE ` + kOzonStockGuard + `
		   AND (l.retry_at IS NULL OR l.retry_at <= CURRENT_TIMESTAMP)
		 ORDER BY l.product_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonStockRow
	for rows.Next() {
		var r OzonStockRow
		if err := rows.Scan(&r.ProductID, &r.OfferID, &r.Stock, &r.StockPushed, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkOzonStockPushed clears the shared retry_at, lifting the price backoff too.
func (d *Database) MarkOzonStockPushed(productID, level int64) error {
	_, err := d.db.Exec(
		`UPDATE ozon_links SET stock_pushed=?, stock_error='', retry_at=NULL
		 WHERE product_id=?`, level, productID)
	return err
}

// MarkOzonStockError writes retry_at as UTC to match CURRENT_TIMESTAMP comparisons.
func (d *Database) MarkOzonStockError(productID int64, msg string, retryAt time.Time) error {
	_, err := d.db.Exec(
		`UPDATE ozon_links SET stock_error=?, retry_at=? WHERE product_id=?`,
		msg, retryAt.UTC().Format(time.DateTime), productID)
	return err
}

// CountOzonStockState counts pending ignoring retry_at: a row in backoff still counts.
func (d *Database) CountOzonStockState() (pending, failed int, err error) {
	err = d.db.QueryRow(
		`SELECT
		   (SELECT COUNT(*) FROM ozon_links l
		      LEFT JOIN products p ON p.id = l.product_id
		    WHERE `+kOzonStockGuard+`),
		   (SELECT COUNT(*) FROM ozon_links WHERE stock_error != '')`).
		Scan(&pending, &failed)
	return pending, failed, err
}

func (d *Database) ListOzonStockErrors() ([]OzonStockRow, error) {
	rows, err := d.db.Query(
		`SELECT l.product_id, l.offer_id, MAX(COALESCE(p.stock, 0), 0),
		        l.stock_pushed, l.stock_error
		 FROM ozon_links l LEFT JOIN products p ON p.id = l.product_id
		 WHERE l.stock_error != '' ORDER BY l.product_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonStockRow
	for rows.Next() {
		var r OzonStockRow
		if err := rows.Scan(&r.ProductID, &r.OfferID, &r.Stock, &r.StockPushed, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CountOzonLinks counts products, not links: orphaned links have no product to count.
func (d *Database) CountOzonLinks() (linked, unlinked int, err error) {
	err = d.db.QueryRow(
		`SELECT
		   (SELECT COUNT(*) FROM products p
		      WHERE EXISTS (SELECT 1 FROM ozon_links l WHERE l.product_id = p.id)),
		   (SELECT COUNT(*) FROM products p
		      WHERE NOT EXISTS (SELECT 1 FROM ozon_links l WHERE l.product_id = p.id))`).
		Scan(&linked, &unlinked)
	return linked, unlinked, err
}

// ClearOzonBackoff drops pending retries; only the manual "push now" button calls it.
func (d *Database) ClearOzonBackoff() error {
	_, err := d.db.Exec(`UPDATE ozon_links SET retry_at=NULL WHERE retry_at IS NOT NULL`)
	return err
}
