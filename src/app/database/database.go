package database

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
	db *sql.DB
	// The path is needed by the stats page: it shows the file size, while the
	// config stores it with a "~" that the caller has already expanded by now.
	path string
}

// Path — the file the database opened. For ":memory:" it returns just that.
func (d *Database) Path() string { return d.path }

// A second process touches the database now — the nightly backup
// (`VACUUM INTO`) and `-reset-password`. In rollback-journal mode a reader
// blocks the writer, so a backup would turn checkout into a 500. WAL makes
// reads non-blocking, busy_timeout waits instead of returning SQLITE_BUSY at
// once, and synchronous NORMAL is the standard companion of WAL: it saves an
// fsync per order, which is safe on RAID-1 with power-loss-protected SSDs and
// nightly backups. ":memory:" silently ignores WAL, so tests are unaffected.
const kDSNParams = "?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL"

func Open(path string) (*Database, error) {
	db, err := sql.Open("sqlite3", path+kDSNParams)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// SQLite writes single-threaded, and ":memory:" hands every new connection
	// its own empty database — a pool of one connection removes both problems.
	db.SetMaxOpenConns(1)
	d := &Database{db: db, path: path}
	if err := d.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

func OpenInMemory() (*Database, error) { return Open(":memory:") }

func (d *Database) Close() error { return d.db.Close() }

func (d *Database) migrate() error {
	_, err := d.db.Exec(`
	CREATE TABLE IF NOT EXISTS products (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		sku         TEXT NOT NULL DEFAULT '',
		title       TEXT NOT NULL,
		slug        TEXT NOT NULL UNIQUE,
		description TEXT NOT NULL DEFAULT '',
		price       INTEGER NOT NULL DEFAULT 0, -- minor units (kopecks)
		-- What the source charged us, in its own money, exactly as imported. Kept
		-- so the shop price can be recomputed from scratch when the coefficient
		-- changes: rescaling the current price instead would drift with rounding
		-- and would be wrong outright for batches imported at another rate.
		-- 0 means the product was created by hand and has no source.
		source_price INTEGER NOT NULL DEFAULT 0,
		-- 1 = the owner set this price themselves; a recompute must not overwrite
		-- their work.
		price_manual INTEGER NOT NULL DEFAULT 0,
		stock       INTEGER NOT NULL DEFAULT 0,
		category    TEXT NOT NULL DEFAULT '',
		-- Whose goods these are: a group the owner names (a supplier).
		-- Empty means nobody's but the owner's. A shop can live off two suppliers
		-- at once, and one feed must not reprice or zero out another's goods.
		-- A name rather than the feed URL: the link to an export changes, the
		-- supplier does not.
		supplier    TEXT NOT NULL DEFAULT '',
		-- 1 hides the product from the storefront: out of the catalogue, out of
		-- the sitemap and 404 on its own page. Phrased as "hidden" rather than
		-- "visible" so that the zero value — of the column and of the Go struct —
		-- is the safe one: a forgotten field must not silently hide goods.
		-- Channels are unaffected: what a shop shows and what it sells on Ozon
		-- are separate decisions.
		hidden      INTEGER NOT NULL DEFAULT 0,
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS product_images (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
		path       TEXT NOT NULL,
		position   INTEGER NOT NULL DEFAULT 0
	);
	-- The catalogue collects images per product for every card on the page:
	-- without the index that is a full table scan 60 times per render (measured
	-- on 20 000 photos).
	CREATE INDEX IF NOT EXISTS idx_product_images_product ON product_images(product_id);
	CREATE TABLE IF NOT EXISTS orders (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT NOT NULL,
		phone      TEXT NOT NULL,
		comment    TEXT NOT NULL DEFAULT '',
		items_json TEXT NOT NULL,
		status     TEXT NOT NULL DEFAULT 'new', -- new|done|cancelled
		-- 0 for orders placed before stock accounting existed: cancelling them
		-- must not return to the warehouse what was never taken from it.
		stock_applied INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	-- Duplicates the order contents from items_json deliberately: items_json is
	-- an immutable legal snapshot for the tax CSV (it outlives a deleted
	-- product), order_items is the working link to products used to take stock
	-- off and put it back.
	CREATE TABLE IF NOT EXISTS order_items (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		order_id   INTEGER NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
		product_id INTEGER REFERENCES products(id) ON DELETE SET NULL,
		qty        INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_order_items_order ON order_items(order_id);
	CREATE TABLE IF NOT EXISTS settings (
		id            INTEGER PRIMARY KEY CHECK (id = 1),
		owner_email   TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		shop_name     TEXT NOT NULL DEFAULT '',
		shop_phone    TEXT NOT NULL DEFAULT '',
		smtp_host     TEXT NOT NULL DEFAULT '',
		smtp_port     INTEGER NOT NULL DEFAULT 465,
		smtp_user     TEXT NOT NULL DEFAULT '',
		smtp_password TEXT NOT NULL DEFAULT '',
		-- Money the shop sells in: RUB for Russia, BYN for Belarus. Buyers and
		-- search engines see this, so it must not be guessed per product.
		currency      TEXT NOT NULL DEFAULT 'RUB',
		-- Uploaded logo file name in the uploads dir. Empty means the storefront
		-- shows the shop name as text, which is also the SEO-safe default.
		logo          TEXT NOT NULL DEFAULT '',
		-- Owner's language, used by the admin and by every message the server
		-- renders for them. The storefront is not affected: it speaks the
		-- language of the products.
		lang          TEXT NOT NULL DEFAULT 'ru',
		-- Multiplier from the source price to the shelf price: exchange rate,
		-- import costs and anything else the owner folds into one number.
		price_coefficient REAL NOT NULL DEFAULT 1,
		-- Last YML feed imported, so a weekly refresh is one button instead of
		-- pasting the link again. Only the feed: Ozon and WB keys are deliberately
		-- not stored — the admin promises the seller they are not kept.
		feed_url      TEXT NOT NULL DEFAULT '',
		feed_supplier TEXT NOT NULL DEFAULT '',
		-- Analytics counters. Empty means the storefront renders nothing at all:
		-- a shop that has not set them up stays without a single script.
		ga_measurement_id   TEXT NOT NULL DEFAULT '',
		metrika_counter_id  TEXT NOT NULL DEFAULT '',
		-- Legal details for the storefront footer, free-form multiline.
		requisites          TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE IF NOT EXISTS auth_tokens (
		token      TEXT PRIMARY KEY,
		purpose    TEXT NOT NULL, -- session|reset
		expires_at DATETIME NOT NULL,
		used       INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS ozon_settings (
		id           INTEGER PRIMARY KEY CHECK (id = 1),
		client_id    TEXT NOT NULL DEFAULT '',
		api_key      TEXT NOT NULL DEFAULT '',
		warehouse_id TEXT NOT NULL DEFAULT '',
		enabled      INTEGER NOT NULL DEFAULT 0,
		-- Price push currency: RUB for ozon.ru cabinets, BYN for ozon.by.
		currency     TEXT NOT NULL DEFAULT 'RUB'
	);
	-- Deliberately without an FK on products: the link must outlive the product.
	-- Once a product is removed from the shop its Ozon card is still up, and the
	-- sync must push a zero there instead of losing offer_id with the row.
	CREATE TABLE IF NOT EXISTS ozon_links (
		product_id   INTEGER PRIMARY KEY,
		offer_id     TEXT NOT NULL,
		-- Price ON OZON in kopecks; 0 = we do not manage this product's price on
		-- the platform (products.price is the shelf price, a different number).
		price        INTEGER NOT NULL DEFAULT 0,
		-- Last level pushed to the platform, -1 = never pushed. Needed by the
		-- sync; created up front so it does not need an ALTER TABLE on live
		-- installs.
		stock_pushed INTEGER NOT NULL DEFAULT -1,
		price_pushed INTEGER NOT NULL DEFAULT -1,
		stock_error  TEXT NOT NULL DEFAULT '',
		price_error  TEXT NOT NULL DEFAULT '',
		-- One retry_at for both pushes: a row's backoff postpones stock and price
		-- alike. Splitting it buys nothing — the methods share the cabinet's
		-- rate budget, and a row the platform complains about is usually bad as
		-- a whole.
		retry_at     DATETIME
	);
	-- Ledger of applied Ozon postings. Idempotency rests on UNIQUE: a posting
	-- seen twice is rejected by the constraint, not by an "did we apply it
	-- already" check — that check can be lost between SELECT and INSERT.
	-- These sales do NOT land in orders: the shop's tax CSV must not double the
	-- revenue Ozon already reports itself.
	CREATE TABLE IF NOT EXISTS ozon_orders (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		posting_number TEXT NOT NULL UNIQUE,
		status         TEXT NOT NULL,
		-- 1 = the platform sold more than we had on the books: stock hit zero and
		-- refusing after the fact is no longer possible.
		oversold       INTEGER NOT NULL DEFAULT 0,
		created_at     DATETIME NOT NULL
	);
	-- Deliberately without an FK on products: the row must outlive the product,
	-- and an unmatched item (product_id IS NULL) must reach the owner's eyes
	-- instead of getting lost.
	CREATE TABLE IF NOT EXISTS ozon_order_items (
		ozon_order_id INTEGER NOT NULL REFERENCES ozon_orders(id) ON DELETE CASCADE,
		product_id    INTEGER,
		offer_id      TEXT NOT NULL,
		qty           INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_ozon_order_items_order
		ON ozon_order_items(ozon_order_id);
	-- Markup ladder for the channel: a percentage does not survive contact with
	-- a catalogue that mixes 30-ruble and 3000-ruble goods — on the cheap end a
	-- percentage does not cover the platform's fee, so resellers price by
	-- multiples instead. up_to is the exclusive upper bound of the band in
	-- kopecks; 0 means "and everything above".
	CREATE TABLE IF NOT EXISTS ozon_price_rules (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		up_to      INTEGER NOT NULL,
		multiplier REAL NOT NULL
	);
	CREATE TABLE IF NOT EXISTS ozon_cursor (
		id           INTEGER PRIMARY KEY CHECK (id = 1),
		orders_since DATETIME NOT NULL
	);`)
	return err
}
