package database

import (
	"cmp"
	"database/sql"
	"fmt"
	"slices"
)

// PriceRule: everything below UpTo kopecks gets Multiplier; UpTo 0 is the top band.
type PriceRule struct {
	UpTo       int64   `json:"up_to"`
	Multiplier float64 `json:"multiplier"`
}

// kMaxMultiplier guards against a typo sending a catalogue out at a hundred times price.
const kMaxMultiplier = 100

func ValidPriceRules(rules []PriceRule) error {
	if len(rules) == 0 {
		return nil
	}
	open := 0
	for _, r := range rules {
		if r.Multiplier <= 0 || r.Multiplier > kMaxMultiplier {
			return fmt.Errorf("invalid multiplier: %v", r.Multiplier)
		}
		if r.UpTo < 0 {
			return fmt.Errorf("invalid band bound: %d", r.UpTo)
		}
		if r.UpTo == 0 {
			open++
		}
	}
	// Without an open-ended band the most expensive goods would get no price at all.
	if open != 1 {
		return fmt.Errorf("exactly one open-ended band is required, got %d", open)
	}
	return nil
}

// sortRules puts bands ascending with the open-ended one last, so first match wins.
func sortRules(rules []PriceRule) {
	slices.SortStableFunc(rules, func(a, b PriceRule) int {
		if a.UpTo == 0 {
			return 1
		}
		if b.UpTo == 0 {
			return -1
		}
		return cmp.Compare(a.UpTo, b.UpTo)
	})
}

// Identifiers cannot be ? parameters; every caller interpolates a fixed constant.

func (d *Database) priceRules(table string) ([]PriceRule, error) {
	rows, err := d.db.Query(`SELECT up_to, multiplier FROM ` + table + ` ORDER BY id`)
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

// setPriceRules replaces the whole ladder: partial edits would leave it invalid.
func (d *Database) setPriceRules(table string, rules []PriceRule) error {
	if err := ValidPriceRules(rules); err != nil {
		return err
	}
	sortRules(rules)
	return d.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM ` + table); err != nil {
			return err
		}
		for _, r := range rules {
			if _, err := tx.Exec(
				`INSERT INTO `+table+` (up_to, multiplier) VALUES (?, ?)`,
				r.UpTo, r.Multiplier); err != nil {
				return err
			}
		}
		return nil
	})
}

// fillPricesByRules fills only unset prices; linkedPred is the channel's predicate on l.
func (d *Database) fillPricesByRules(rulesTable, linksTable, linkedPred string) (int, error) {
	rules, err := d.priceRules(rulesTable)
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
		`SELECT l.product_id, p.price FROM ` + linksTable + ` l
		 JOIN products p ON p.id = l.product_id
		 WHERE l.price = 0 AND ` + linkedPred + ` AND p.price > 0`)
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
			price := ShelfPrice(rules, t.price, 1)
			if price <= 0 {
				continue
			}
			if _, err := tx.Exec(
				`UPDATE `+linksTable+` SET price=? WHERE product_id=?`, price, t.id); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
