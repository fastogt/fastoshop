package database

import (
	"fmt"
	"net/url"
	"strings"
)

// MaxOutsideLinks per product: two buttons are a choice, five are a paralysis,
// and every one of them costs a direct order.
const MaxOutsideLinks = 2

// OutsideLink is a button leading out of the shop: the seller's own card on a
// marketplace, their Instagram, their other site.
type OutsideLink struct {
	ID    int64  `json:"id"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

const kOutsideSchema = `
	CREATE TABLE IF NOT EXISTS product_outside_links (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		product_id INTEGER NOT NULL REFERENCES products(id) ON DELETE CASCADE,
		label      TEXT NOT NULL,
		url        TEXT NOT NULL,
		position   INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_outside_links_product ON product_outside_links(product_id);`

// ValidOutsideURL keeps the button an http link: a javascript: or data: address
// on a page we render for somebody else's goods is an attack, not a shop.
func ValidOutsideURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (d *Database) OutsideLinks(productID int64) ([]OutsideLink, error) {
	rows, err := d.db.Query(`SELECT id, label, url FROM product_outside_links
		WHERE product_id = ? ORDER BY position, id`, productID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []OutsideLink{}
	for rows.Next() {
		var l OutsideLink
		if err := rows.Scan(&l.ID, &l.Label, &l.URL); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SetOutsideLinks replaces a product's buttons; an empty list removes them all.
func (d *Database) SetOutsideLinks(productID int64, links []OutsideLink) error {
	if len(links) > MaxOutsideLinks {
		return fmt.Errorf("too many outside links: %d", len(links))
	}
	for _, l := range links {
		if strings.TrimSpace(l.Label) == "" || !ValidOutsideURL(l.URL) {
			return fmt.Errorf("bad outside link: %q %q", l.Label, l.URL)
		}
	}
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM product_outside_links WHERE product_id = ?`, productID); err != nil {
		return err
	}
	for i, l := range links {
		if _, err := tx.Exec(`INSERT INTO product_outside_links (product_id, label, url, position)
			VALUES (?, ?, ?, ?)`, productID, strings.TrimSpace(l.Label), strings.TrimSpace(l.URL), i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// OutsideLinkURL is where a click on a product's own button goes; empty when the
// link is gone, or belongs to another product or a hidden one.
func (d *Database) OutsideLinkURL(id int64, slug string) string {
	var u string
	_ = d.db.QueryRow(`SELECT l.url FROM product_outside_links l
		JOIN products p ON p.id = l.product_id
		WHERE l.id = ? AND p.slug = ? AND p.hidden = 0`, id, slug).Scan(&u)
	return u
}
