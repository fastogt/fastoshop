package database

import "database/sql"

// Feed is the YML source and its supplier group, kept in the single settings row.
type Feed struct {
	URL      string
	Supplier string
}

func (d *Database) SaveFeed(f *Feed) error {
	_, err := d.db.Exec(
		`UPDATE settings SET feed_url=?, feed_supplier=? WHERE id=1`, f.URL, f.Supplier)
	return err
}

// GetFeed returns nil when nothing was imported from a feed yet.
func (d *Database) GetFeed() (*Feed, error) {
	var f Feed
	err := d.db.QueryRow(
		`SELECT feed_url, feed_supplier FROM settings WHERE id=1`).Scan(&f.URL, &f.Supplier)
	if err == sql.ErrNoRows || (err == nil && f.URL == "") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}
