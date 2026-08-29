package database

import "strings"

// CatalogParamNames lists every characteristic the catalogue states, most used
// first. The owner decides from this list what a buyer sees, so it is the names
// as the sources wrote them — renaming or merging them here would hide the fact
// that two marketplaces call one property two things.
//
// json_each over a column rather than a table of its own: characteristics travel
// with the product and are read sixty at a time on a catalogue page, and this
// query runs once on a settings screen.
func (d *Database) CatalogParamNames() ([]string, error) {
	rows, err := d.db.Query(`
		SELECT json_extract(p.value, '$.name') AS name, count(*) AS n
		FROM products, json_each(products.params) p
		WHERE json_valid(products.params) AND json_type(products.params) = 'array'
		GROUP BY name HAVING name IS NOT NULL AND name != ''
		ORDER BY n DESC, name`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// HiddenParams is the set the storefront leaves out.
func (d *Database) HiddenParams() (map[string]bool, error) {
	rows, err := d.db.Query(`SELECT name FROM hidden_params`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// SetHiddenParams replaces the set. The whole set, because the screen states it
// whole: a name missing from the request is a box the owner unticked, and
// merging would leave no way to show something again.
func (d *Database) SetHiddenParams(names []string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM hidden_params`); err != nil {
		return err
	}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO hidden_params (name) VALUES (?)`, n); err != nil {
			return err
		}
	}
	return tx.Commit()
}
