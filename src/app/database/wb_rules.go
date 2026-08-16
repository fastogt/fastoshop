package database

import "database/sql"

func (d *Database) WBPriceRules() ([]PriceRule, error) {
	rows, err := d.db.Query(`SELECT up_to, multiplier FROM wb_price_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []PriceRule
	for rows.Next() {
		var r PriceRule
		if err := rows.Scan(&r.UpTo, &r.Multiplier); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortRules(out)
	return out, nil
}

// SetWBPriceRules replaces the whole ladder: editing bands one by one would let
// the table pass through states that are not a valid ladder.
func (d *Database) SetWBPriceRules(rules []PriceRule) error {
	if err := ValidPriceRules(rules); err != nil {
		return err
	}
	sortRules(rules)
	return d.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM wb_price_rules`); err != nil {
			return err
		}
		for _, r := range rules {
			if _, err := tx.Exec(
				`INSERT INTO wb_price_rules (up_to, multiplier) VALUES (?, ?)`,
				r.UpTo, r.Multiplier); err != nil {
				return err
			}
		}
		return nil
	})
}

// FillWBPricesByRules fills the platform price of linked products that do not
// have one yet. Prices the owner set are left alone, same as the flat markup
// helper: the ladder is a starting point, not an override.
func (d *Database) FillWBPricesByRules() (int, error) {
	rules, err := d.WBPriceRules()
	if err != nil {
		return 0, err
	}
	if err := ValidPriceRules(rules); err != nil {
		return 0, err
	}
	if len(rules) == 0 {
		return 0, nil
	}
	rows, err := d.db.Query(
		`SELECT l.product_id, p.price FROM wb_links l
		 JOIN products p ON p.id = l.product_id
		 WHERE l.price = 0 AND l.nm_id != 0 AND p.price > 0`)
	if err != nil {
		return 0, err
	}
	type target struct {
		id    int64
		price int64
	}
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.price); err != nil {
			_ = rows.Close()
			return 0, err
		}
		targets = append(targets, t)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	n := 0
	err = d.withTx(func(tx *sql.Tx) error {
		for _, t := range targets {
			price := ApplyRule(rules, t.price)
			if price <= 0 {
				continue
			}
			if _, err := tx.Exec(
				`UPDATE wb_links SET price=? WHERE product_id=?`, price, t.id); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
