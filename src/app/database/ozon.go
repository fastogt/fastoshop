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
	ID        int64
	ProductID int64
	// Qty is how many units of the product one card sells.
	Qty     int64
	OfferID string
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

// UpsertOzonLink keys on offer_id; a card moved to another product or pack size starts over.
func (d *Database) UpsertOzonLink(l *OzonLink) error {
	if l.Qty < 1 {
		l.Qty = 1
	}
	_, err := d.db.Exec(
		`INSERT INTO ozon_links (offer_id, product_id, qty) VALUES (?, ?, ?)
		 ON CONFLICT(offer_id) DO UPDATE SET product_id=excluded.product_id, qty=excluded.qty,
		   stock_pushed=CASE WHEN ozon_links.product_id = excluded.product_id
		     AND ozon_links.qty = excluded.qty THEN ozon_links.stock_pushed ELSE -1 END`,
		l.OfferID, l.ProductID, l.Qty)
	return err
}

func (d *Database) DeleteOzonLink(id int64) error {
	_, err := d.db.Exec(`DELETE FROM ozon_links WHERE id=?`, id)
	return err
}

// OzonLinkByID returns nil when there is no such link.
func (d *Database) OzonLinkByID(id int64) (*OzonLinkState, error) {
	var l OzonLinkState
	err := scanOzonLinkState(d.db.QueryRow(
		`SELECT `+kOzonLinkStateCols+` FROM ozon_links WHERE id=?`, id), &l)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// AllOzonLinks is every link keyed by offer_id, for matching against the cabinet's cards.
func (d *Database) AllOzonLinks() (map[string]OzonLinkState, error) {
	rows, err := d.db.Query(`SELECT ` + kOzonLinkStateCols + ` FROM ozon_links`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]OzonLinkState)
	for rows.Next() {
		var l OzonLinkState
		if err := scanOzonLinkState(rows, &l); err != nil {
			return nil, err
		}
		out[l.OfferID] = l
	}
	return out, rows.Err()
}

// OzonStockRow is a link due for a stock push; a deleted product means zero.
type OzonStockRow struct {
	ID          int64
	ProductID   int64
	OfferID     string
	Stock       int64
	StockPushed int64
	Error       string
}

// kOzonCardStock: whole packs only - integer division rounds down.
const kOzonCardStock = `MAX(COALESCE(p.stock, 0), 0) / l.qty`

// stock_pushed = -1 is "no baseline yet": the first push is always allowed.
const kOzonStockGuard = `l.offer_id != ''
	 AND (l.stock_pushed = -1 OR ` + kOzonCardStock + ` != l.stock_pushed)`

func (d *Database) OzonStockToPush() ([]OzonStockRow, error) {
	rows, err := d.db.Query(
		`SELECT l.id, l.product_id, l.offer_id, ` + kOzonCardStock + `,
		        l.stock_pushed, l.stock_error
		 FROM ozon_links l LEFT JOIN products p ON p.id = l.product_id
		 WHERE ` + kOzonStockGuard + `
		   AND (l.retry_at IS NULL OR l.retry_at <= CURRENT_TIMESTAMP)
		 ORDER BY l.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonStockRow
	for rows.Next() {
		var r OzonStockRow
		if err := rows.Scan(&r.ID, &r.ProductID, &r.OfferID, &r.Stock, &r.StockPushed, &r.Error); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkOzonStockPushed clears the shared retry_at, lifting the price backoff too.
func (d *Database) MarkOzonStockPushed(id, level int64) error {
	_, err := d.db.Exec(
		`UPDATE ozon_links SET stock_pushed=?, stock_error='', retry_at=NULL
		 WHERE id=?`, level, id)
	return err
}

// MarkOzonStockError writes retry_at as UTC to match CURRENT_TIMESTAMP comparisons.
func (d *Database) MarkOzonStockError(id int64, msg string, retryAt time.Time) error {
	_, err := d.db.Exec(
		`UPDATE ozon_links SET stock_error=?, retry_at=? WHERE id=?`,
		msg, retryAt.UTC().Format(time.DateTime), id)
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
		`SELECT l.id, l.product_id, l.offer_id, ` + kOzonCardStock + `,
		        l.stock_pushed, l.stock_error
		 FROM ozon_links l LEFT JOIN products p ON p.id = l.product_id
		 WHERE l.stock_error != '' ORDER BY l.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonStockRow
	for rows.Next() {
		var r OzonStockRow
		if err := rows.Scan(&r.ID, &r.ProductID, &r.OfferID, &r.Stock, &r.StockPushed, &r.Error); err != nil {
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
