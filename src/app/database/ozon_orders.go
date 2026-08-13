package database

import (
	"database/sql"
	"time"
)

// kSQLiteTime — the format SQLite emits for CURRENT_TIMESTAMP and the one the
// driver uses to parse DATETIME columns. Time is written only this way: mixing
// in RFC3339 breaks date comparisons right inside SQL.
const kSQLiteTime = "2006-01-02 15:04:05"

// OzonPostingItem — a posting line in the marketplace's terms: matching it to
// a shop product happens later, inside the apply transaction.
type OzonPostingItem struct {
	OfferID string
	Qty     int
}

// OzonPosting — an Ozon posting in the shape the ladder applies it. Cancelled
// is computed by the caller: the set of "cancelled" statuses is knowledge
// about the marketplace, not about the database.
type OzonPosting struct {
	PostingNumber string
	Status        string
	Cancelled     bool
	CreatedAt     time.Time
	Items         []OzonPostingItem
}

// OzonStatusCancelled — the ladder stores any cancellation under it, whichever
// of the cancelling statuses Ozon sent. Otherwise the cancelled → not_accepted
// transition would look like a fresh cancellation and return the stock a
// second time; this should not cost a separate "stock returned" column.
const OzonStatusCancelled = "cancelled"

func (p *OzonPosting) storedStatus() string {
	if p.Cancelled {
		return OzonStatusCancelled
	}
	return p.Status
}

// ApplyOzonPosting applies a posting exactly once and reports whether stock
// moved in the process.
//
// Idempotency rests on UNIQUE(posting_number): the insert either creates the
// row (the posting is new) or does nothing (already applied). Checking with a
// SELECT before the insert is not an option — two sync passes would slip
// through that gap.
//
// A cancellation returns stock exactly on the "applied → cancelled"
// transition: the same cancelled posting reappearing in the cursor's overlap
// window no longer moves anything.
func (d *Database) ApplyOzonPosting(p *OzonPosting) (moved bool, err error) {
	err = d.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO ozon_orders (posting_number, status, created_at)
			 VALUES (?, ?, ?) ON CONFLICT(posting_number) DO NOTHING`,
			p.PostingNumber, p.storedStatus(), p.CreatedAt.UTC().Format(kSQLiteTime))
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			moved, err = applySeenPosting(tx, p)
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		moved, err = applyNewPosting(tx, id, p)
		return err
	})
	if err != nil {
		return false, err
	}
	return moved, nil
}

// applyNewPosting records the lines and deducts stock. A posting we first see
// already cancelled is only recorded: deducting and immediately returning the
// same product is a pointless stock movement out of thin air.
func applyNewPosting(tx *sql.Tx, id int64, p *OzonPosting) (bool, error) {
	moved, oversold := false, false
	for _, it := range p.Items {
		productID, err := resolveOffer(tx, it.OfferID)
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(
			`INSERT INTO ozon_order_items (ozon_order_id, product_id, offer_id, qty)
			 VALUES (?, ?, ?, ?)`, id, productID, it.OfferID, it.Qty); err != nil {
			return false, err
		}
		if productID == nil || p.Cancelled || it.Qty <= 0 {
			continue
		}
		var have int
		if err := tx.QueryRow(
			`SELECT stock FROM products WHERE id = ?`, *productID).Scan(&have); err != nil {
			return false, err
		}
		if have < it.Qty {
			oversold = true
		}
		// MAX(0, ...) — refusing the deduction is not an option: the marketplace
		// has already sold, and negative stock on the storefront is worse than zero.
		if _, err := tx.Exec(
			`UPDATE products SET stock = MAX(0, stock - ?), updated_at = CURRENT_TIMESTAMP
			 WHERE id = ?`, it.Qty, *productID); err != nil {
			return false, err
		}
		moved = true
	}
	if oversold {
		if _, err := tx.Exec(`UPDATE ozon_orders SET oversold = 1 WHERE id = ?`, id); err != nil {
			return false, err
		}
	}
	return moved, nil
}

// applySeenPosting handles a posting we already know: only the transition into
// a cancelled status is interesting, everything else is a status update with
// no stock movement.
func applySeenPosting(tx *sql.Tx, p *OzonPosting) (bool, error) {
	var id int64
	var status string
	if err := tx.QueryRow(
		`SELECT id, status FROM ozon_orders WHERE posting_number = ?`,
		p.PostingNumber).Scan(&id, &status); err != nil {
		return false, err
	}
	next := p.storedStatus()
	if status == next {
		return false, nil
	}
	moved := false
	// Return only on the "not cancelled → cancelled" transition. A posting seen
	// as cancelled from the start never deducted stock — there is nothing to return.
	if p.Cancelled && status != OzonStatusCancelled {
		items, err := ozonOrderStock(tx, id)
		if err != nil {
			return false, err
		}
		if err := returnStock(tx, items); err != nil {
			return false, err
		}
		moved = len(items) > 0
	}
	_, err := tx.Exec(`UPDATE ozon_orders SET status = ? WHERE id = ?`, next, id)
	return moved, err
}

// ozonOrderStock reads all lines before the first Exec: the transaction has a
// single connection, and an open Rows must not be held while writing.
// Unmatched lines drop out — there is nowhere to return their stock.
//
// ponytail: we return the ordered qty, not what was actually deducted. They can
// diverge only on an oversell (deducted less than sold) — if that starts to
// hurt, add an applied_qty column to ozon_order_items: the table is new, no
// ALTER TABLE on live instances will be needed.
func ozonOrderStock(tx *sql.Tx, id int64) ([]OrderItem, error) {
	rows, err := tx.Query(
		`SELECT product_id, qty FROM ozon_order_items
		 WHERE ozon_order_id = ? AND product_id IS NOT NULL`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OrderItem
	for rows.Next() {
		var it OrderItem
		if err := rows.Scan(&it.ProductID, &it.Qty); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// resolveOffer looks up a product by the marketplace SKU. nil — no link: the
// line is recorded anyway, so the owner sees an unrecognized sale, not a blank.
func resolveOffer(tx *sql.Tx, offerID string) (*int64, error) {
	var id int64
	err := tx.QueryRow(
		`SELECT product_id FROM ozon_links WHERE offer_id = ? LIMIT 1`, offerID).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

type OzonOrderItem struct {
	ProductID *int64
	OfferID   string
	Title     string
	Qty       int
}

type OzonOrder struct {
	ID            int64
	PostingNumber string
	Status        string
	Oversold      bool
	CreatedAt     time.Time
	Items         []OzonOrderItem
}

func (d *Database) CountOzonOrders() (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM ozon_orders`).Scan(&n)
	return n, err
}

// CountOzonOrderState — counters for the tab header: total sales, of those how
// many oversold and how many with unmatched lines.
func (d *Database) CountOzonOrderState() (total, oversold, unresolved int, err error) {
	err = d.db.QueryRow(
		`SELECT (SELECT COUNT(*) FROM ozon_orders),
		        (SELECT COUNT(*) FROM ozon_orders WHERE oversold = 1),
		        (SELECT COUNT(DISTINCT ozon_order_id) FROM ozon_order_items
		           WHERE product_id IS NULL)`).Scan(&total, &oversold, &unresolved)
	return total, oversold, unresolved, err
}

// ListOzonOrdersPage returns a page of sales with their lines. Lines are read
// by a second query over the already collected ids: a JOIN would multiply rows
// and break pagination.
func (d *Database) ListOzonOrdersPage(limit, offset int) ([]OzonOrder, error) {
	rows, err := d.db.Query(
		`SELECT id, posting_number, status, oversold, created_at
		 FROM ozon_orders ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonOrder
	byID := map[int64]int{}
	for rows.Next() {
		var o OzonOrder
		if err := rows.Scan(&o.ID, &o.PostingNumber, &o.Status, &o.Oversold,
			&o.CreatedAt); err != nil {
			return nil, err
		}
		o.Items = []OzonOrderItem{}
		byID[o.ID] = len(out)
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	if err := d.loadOzonOrderItems(out, byID); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *Database) loadOzonOrderItems(orders []OzonOrder, byID map[int64]int) error {
	lo, hi := orders[len(orders)-1].ID, orders[0].ID
	if lo > hi {
		lo, hi = hi, lo
	}
	// An id range instead of IN (...): a page is always contiguous by id within
	// its bounds, and there is no reason to glue placeholders together by hand.
	rows, err := d.db.Query(
		`SELECT i.ozon_order_id, i.product_id, i.offer_id, i.qty, COALESCE(p.title, '')
		 FROM ozon_order_items i LEFT JOIN products p ON p.id = i.product_id
		 WHERE i.ozon_order_id BETWEEN ? AND ? ORDER BY i.rowid`, lo, hi)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var orderID int64
		var it OzonOrderItem
		if err := rows.Scan(&orderID, &it.ProductID, &it.OfferID, &it.Qty, &it.Title); err != nil {
			return err
		}
		if idx, ok := byID[orderID]; ok {
			orders[idx].Items = append(orders[idx].Items, it)
		}
	}
	return rows.Err()
}

// OzonOrdersSince returns the zero time if polling has never run — the caller
// decides itself which window to start with the first time.
func (d *Database) OzonOrdersSince() (time.Time, error) {
	var t time.Time
	err := d.db.QueryRow(`SELECT orders_since FROM ozon_cursor WHERE id = 1`).Scan(&t)
	if err == sql.ErrNoRows {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

func (d *Database) SetOzonOrdersSince(t time.Time) error {
	_, err := d.db.Exec(
		`INSERT INTO ozon_cursor (id, orders_since) VALUES (1, ?)
		 ON CONFLICT(id) DO UPDATE SET orders_since = excluded.orders_since`,
		t.UTC().Format(kSQLiteTime))
	return err
}
