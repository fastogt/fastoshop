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

// priceAt returns the price of the n-th product by price — the cheap way to a
// percentile when the rows are already indexed.
func (d *Database) priceAt(where string, args []any, offset int) int64 {
	var price int64
	_ = d.db.QueryRow(`SELECT price FROM products`+where+` ORDER BY price LIMIT 1 OFFSET ?`,
		append(append([]any{}, args...), offset)...).Scan(&price)
	return price
}

// CategorySample is what a category looks like from the outside: how many goods
// it holds, what they cost and a few of their names. Enough to write the first
// draft of a page about it without inventing anything.
type CategorySample struct {
	Count    int
	MinPrice int64
	MaxPrice int64
	Titles   []string
	Children []string
}

// SampleCategory reads the node and everything below it in one pass.
func (d *Database) SampleCategory(path string) (*CategorySample, error) {
	where, args := productWhere(path, "", supplierAny, true)
	out := &CategorySample{}
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM products`+where, args...).
		Scan(&out.Count); err != nil {
		return nil, err
	}
	if out.Count == 0 {
		return out, nil
	}
	// Percentiles, not MIN and MAX: a wholesale catalogue always holds one
	// 17-kopeck cap and one 4590-rouble wardrobe, and "prices from 0.17 to 4590"
	// describes nothing. The tenth and ninetieth are the goods the buyer sees.
	out.MinPrice = d.priceAt(where, args, out.Count/10)
	out.MaxPrice = d.priceAt(where, args, out.Count*9/10)
	// The most expensive first: the cheap end of a wholesale catalogue is
	// packaging and oddments, and a draft should name the goods, not the tape.
	rows, err := d.db.Query(`SELECT title FROM products`+where+
		` ORDER BY price DESC LIMIT 40`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, err
		}
		out.Titles = append(out.Titles, title)
	}
	return out, rows.Err()
}
