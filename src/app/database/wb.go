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
	ProductID  int64
	NmID       int64
	Barcode    string
	VendorCode string
}

// GetWBSettings returns empty settings rather than an error on a fresh database:
// there is no id=1 row until the owner saves a token once, and the tab must open
// as an empty form, not a 500.
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

// UpsertWBLink leaves price and the push state alone: re-linking must reset
// neither the platform price the owner set nor the state of the sync.
func (d *Database) UpsertWBLink(l *WBLink) error {
	_, err := d.db.Exec(
		`INSERT INTO wb_links (product_id, nm_id, barcode, vendor_code)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(product_id) DO UPDATE SET nm_id=excluded.nm_id,
		   barcode=excluded.barcode, vendor_code=excluded.vendor_code`,
		l.ProductID, l.NmID, l.Barcode, l.VendorCode)
	return err
}

func (d *Database) DeleteWBLink(productID int64) error {
	_, err := d.db.Exec(`DELETE FROM wb_links WHERE product_id=?`, productID)
	return err
}

// WBStockRow is a link whose stock is due for a push. Stock is already the level
// the platform should hold: a deleted product does not exist, so zero.
type WBStockRow struct {
	ProductID   int64
	Barcode     string
	Stock       int64
	StockPushed int64
	Error       string
	// RetryAt is when the row is due again. Shown to the owner: an error with no
	// "and then what" reads like the sync gave up.
	RetryAt sql.NullTime
}

// kWBStockGuard selects rows whose wanted level diverged from the last pushed
// one. stock_pushed = -1 means there is no baseline yet, so the first push is
// always allowed.
const kWBStockGuard = `l.barcode != ''
	 AND (l.stock_pushed = -1 OR MAX(COALESCE(p.stock, 0), 0) != l.stock_pushed)`

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
		`SELECT l.product_id, l.barcode, MAX(COALESCE(p.stock, 0), 0),
		        l.stock_pushed, l.stock_error, l.retry_at
		 FROM wb_links l LEFT JOIN products p ON p.id = l.product_id ` +
			where + ` ORDER BY l.product_id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []WBStockRow
	for rows.Next() {
		var r WBStockRow
		if err := rows.Scan(&r.ProductID, &r.Barcode, &r.Stock, &r.StockPushed,
			&r.Error, &r.RetryAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkWBStockPushed clears retry_at, which is shared with the price push, for
// the same reason it is shared on Ozon: at worst the price is retried one pass
// early and backs off again.
func (d *Database) MarkWBStockPushed(productID, level int64) error {
	_, err := d.db.Exec(
		`UPDATE wb_links SET stock_pushed=?, stock_error='', retry_at=NULL
		 WHERE product_id=?`, level, productID)
	return err
}

// MarkWBStockError writes retry_at as a UTC string in CURRENT_TIMESTAMP format,
// otherwise the comparison in WBStockToPush drifts by the timezone offset.
func (d *Database) MarkWBStockError(productID int64, msg string, retryAt time.Time) error {
	_, err := d.db.Exec(
		`UPDATE wb_links SET stock_error=?, retry_at=? WHERE product_id=?`,
		msg, retryAt.UTC().Format(kSQLiteTime), productID)
	return err
}

// CountWBStockState counts pending without regard to retry_at — to the owner a
// row in backoff is still "waiting to be sent", not gone.
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

// CountWBLinks counts shop products, not wb_links rows: links of orphaned cards
// outlive their products and have no place in "linked / not linked".
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

// ClearWBBackoff drops every pending retry. Only the manual "push now" button
// calls it: after a failure the owner fixes what the platform complained about
// and presses the button, and a backoff they cannot see makes it look like the
// button does nothing. The ticker keeps waiting, which is what backoff is for.
func (d *Database) ClearWBBackoff() error {
	_, err := d.db.Exec(`UPDATE wb_links SET retry_at=NULL WHERE retry_at IS NOT NULL`)
	return err
}
