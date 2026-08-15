package database

import (
	"database/sql"
	"errors"
	"strings"
)

// CategoryText is the owner's own text for a category page. Without it a
// category page is a listing like any other; with it, it is a landing page —
// the thing a shop on WordPress keeps a separate page for.
type CategoryText struct {
	Path string `json:"path"`
	Body string `json:"body"`
}

// SetCategoryText stores the text, or removes it when the owner cleared the
// field: an empty row would keep an empty block on the page.
func (d *Database) SetCategoryText(path, body string) error {
	body = strings.TrimSpace(body)
	if body == "" {
		_, err := d.db.Exec(`DELETE FROM category_texts WHERE path=?`, path)
		return err
	}
	_, err := d.db.Exec(
		`INSERT INTO category_texts (path, body) VALUES (?, ?)
		 ON CONFLICT(path) DO UPDATE SET body=excluded.body, updated_at=CURRENT_TIMESTAMP`,
		path, body)
	return err
}

func (d *Database) CategoryTextOf(path string) (string, error) {
	var body string
	err := d.db.QueryRow(`SELECT body FROM category_texts WHERE path=?`, path).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return body, err
}

// CategoryTexts returns every text at once: the admin list shows which
// categories are still without one, and 500 rows of prose weigh less than one
// catalogue page.
func (d *Database) CategoryTexts() (map[string]string, error) {
	rows, err := d.db.Query(`SELECT path, body FROM category_texts`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var path, body string
		if err := rows.Scan(&path, &body); err != nil {
			return nil, err
		}
		out[path] = body
	}
	return out, rows.Err()
}
