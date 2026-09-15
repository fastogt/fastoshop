package database

import (
	"database/sql"
	"time"
)

type WBSettings struct {
	Token       string
	WarehouseID string
	Enabled     bool
	Sandbox     bool
}

// WBLink carries both platform keys: stock is set per barcode, price per card.
type WBLink struct {
	ID        int64
	ProductID int64
	// Qty is how many units of the product one card sells.
	Qty        int64
	NmID       int64
	Barcode    string
	VendorCode string
}

// GetWBSettings returns empty settings when the id=1 row does not exist yet.
func (d *Database) GetWBSettings() (*WBSettings, error) {
	var s WBSettings
	err := d.db.QueryRow(
		`SELECT token, warehouse_id, enabled, sandbox FROM wb_settings WHERE id=1`).
		Scan(&s.Token, &s.WarehouseID, &s.Enabled, &s.Sandbox)
	if err == sql.ErrNoRows {
		return &s, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (d *Database) SaveWBSettings(s *WBSettings) error {
	_, err := d.db.Exec(
		`INSERT INTO wb_settings (id, token, warehouse_id, enabled, sandbox)
		 VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET token=excluded.token,
		   warehouse_id=excluded.warehouse_id, enabled=excluded.enabled,
		   sandbox=excluded.sandbox`,
		s.Token, s.WarehouseID, s.Enabled, s.Sandbox)
	return err
}

// UpsertWBLink keys on the barcode; a card moved to another product or pack size starts over.
func (d *Database) UpsertWBLink(l *WBLink) error {
	if l.Qty < 1 {
		l.Qty = 1
	}
	_, err := d.db.Exec(
		`INSERT INTO wb_links (barcode, product_id, qty, nm_id, vendor_code)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(barcode) DO UPDATE SET nm_id=excluded.nm_id,
		   vendor_code=excluded.vendor_code, product_id=excluded.product_id,
		   qty=excluded.qty,
		   stock_pushed=CASE WHEN wb_links.product_id = excluded.product_id
		     AND wb_links.qty = excluded.qty THEN wb_links.stock_pushed ELSE -1 END`,
		l.Barcode, l.ProductID, l.Qty, l.NmID, l.VendorCode)
	return err
}

func (d *Database) DeleteWBLink(id int64) error {
	_, err := d.db.Exec(`DELETE FROM wb_links WHERE id=?`, id)
	return err
}

// WBLinkByID returns nil when there is no such link.
func (d *Database) WBLinkByID(id int64) (*WBLinkState, error) {
	var l WBLinkState
	err := scanWBLinkState(d.db.QueryRow(
		`SELECT `+kWBLinkStateCols+` FROM wb_links WHERE id=?`, id), &l)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// AllWBLinks is every link keyed by barcode, for matching against the cabinet's cards.
func (d *Database) AllWBLinks() (map[string]WBLinkState, error) {
	rows, err := d.db.Query(`SELECT ` + kWBLinkStateCols + ` FROM wb_links`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]WBLinkState)
	for rows.Next() {
		var l WBLinkState
		if err := scanWBLinkState(rows, &l); err != nil {
			return nil, err
		}
		out[l.Barcode] = l
	}
	return out, rows.Err()
}

// WBStockRow is a link whose stock is due for a push; Stock is already in cards, not units.
type WBStockRow struct {
	ID          int64
	ProductID   int64
	Barcode     string
	Stock       int64
	StockPushed int64
	Error       string
	// RetryAt is when the row is due again.
	RetryAt sql.NullTime
}

// kWBCardStock: whole packs only - integer division rounds down.
const kWBCardStock = `MAX(COALESCE(p.stock, 0), 0) / l.qty`

// kWBStockGuard: stock_pushed = -1 means no baseline yet, so the first push passes.
const kWBStockGuard = `l.barcode != ''
	 AND (l.stock_pushed = -1 OR ` + kWBCardStock + ` != l.stock_pushed)`

func (d *Database) WBStockToPush() ([]WBStockRow, error) {
	return d.wbStockRows(
		`WHERE ` + kWBStockGuard + `
		   AND (l.retry_at IS NULL OR l.retry_at <= CURRENT_TIMESTAMP)`)
}

func (d *Database) ListWBStockErrors() ([]WBStockRow, error) {
	return d.wbStockRows(`WHERE l.stock_error != ''`)
}

func (d *Database) wbStockRows(where string) ([]WBStockRow, error) {
	rows, err := d.db.Query(
		`SELECT l.id, l.product_id, l.barcode, ` + kWBCardStock + `,
		        l.stock_pushed, l.stock_error, l.retry_at
		 FROM wb_links l LEFT JOIN products p ON p.id = l.product_id ` +
			where + ` ORDER BY l.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WBStockRow
	for rows.Next() {
		var r WBStockRow
		if err := rows.Scan(&r.ID, &r.ProductID, &r.Barcode, &r.Stock, &r.StockPushed,
			&r.Error, &r.RetryAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkWBStockPushed clears retry_at, which is shared with the price push.
func (d *Database) MarkWBStockPushed(id, level int64) error {
	_, err := d.db.Exec(
		`UPDATE wb_links SET stock_pushed=?, stock_error='', retry_at=NULL
		 WHERE id=?`, level, id)
	return err
}

// MarkWBStockError writes retry_at as a UTC string to match CURRENT_TIMESTAMP.
func (d *Database) MarkWBStockError(id int64, msg string, retryAt time.Time) error {
	_, err := d.db.Exec(
		`UPDATE wb_links SET stock_error=?, retry_at=? WHERE id=?`,
		msg, retryAt.UTC().Format(time.DateTime), id)
	return err
}

// CountWBStockState counts pending regardless of retry_at: backoff still counts.
func (d *Database) CountWBStockState() (pending, failed int, err error) {
	err = d.db.QueryRow(
		`SELECT
		   (SELECT COUNT(*) FROM wb_links l
		      LEFT JOIN products p ON p.id = l.product_id
		    WHERE `+kWBStockGuard+`),
		   (SELECT COUNT(*) FROM wb_links WHERE stock_error != '')`).
		Scan(&pending, &failed)
	return pending, failed, err
}

// CountWBLinks counts shop products, not wb_links rows: orphan links are excluded.
func (d *Database) CountWBLinks() (linked, unlinked int, err error) {
	err = d.db.QueryRow(
		`SELECT
		   (SELECT COUNT(*) FROM products p
		      WHERE EXISTS (SELECT 1 FROM wb_links l WHERE l.product_id = p.id)),
		   (SELECT COUNT(*) FROM products p
		      WHERE NOT EXISTS (SELECT 1 FROM wb_links l WHERE l.product_id = p.id))`).
		Scan(&linked, &unlinked)
	return linked, unlinked, err
}

// ClearWBBackoff drops every pending retry; only the manual push button calls it.
func (d *Database) ClearWBBackoff() error {
	_, err := d.db.Exec(`UPDATE wb_links SET retry_at=NULL WHERE retry_at IS NOT NULL`)
	return err
}
