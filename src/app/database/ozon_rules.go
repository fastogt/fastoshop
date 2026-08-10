package database

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
)

// OzonPriceRule is one band of the markup ladder: everything below UpTo (in
// kopecks) is multiplied by Multiplier. UpTo 0 is the open-ended top band.
type OzonPriceRule struct {
	UpTo       int64   `json:"up_to"`
	Multiplier float64 `json:"multiplier"`
}

// kMaxMultiplier is the same kind of guard as the import coefficient: a typo
// must not send a catalogue to the platform at a hundred times its price.
const kMaxMultiplier = 100

func ValidPriceRules(rules []OzonPriceRule) error {
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
	// Without an open-ended band the most expensive goods would silently get no
	// price at all, which looks exactly like "the ladder did not work".
	if open != 1 {
		return fmt.Errorf("exactly one open-ended band is required, got %d", open)
	}
	return nil
}

// sortRules puts the bands in ascending order with the open-ended one last, so
// the first match is the right one regardless of how the owner typed them in.
func sortRules(rules []OzonPriceRule) {
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].UpTo == 0 {
			return false
		}
		if rules[j].UpTo == 0 {
			return true
		}
		return rules[i].UpTo < rules[j].UpTo
	})
}

func (d *Database) OzonPriceRules() ([]OzonPriceRule, error) {
	rows, err := d.db.Query(`SELECT up_to, multiplier FROM ozon_price_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []OzonPriceRule
	for rows.Next() {
		var r OzonPriceRule
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

// SetOzonPriceRules replaces the whole ladder: editing bands one by one would
// let the table pass through states that are not a valid ladder.
func (d *Database) SetOzonPriceRules(rules []OzonPriceRule) error {
	if err := ValidPriceRules(rules); err != nil {
		return err
	}
	sortRules(rules)
	return d.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM ozon_price_rules`); err != nil {
			return err
		}
		for _, r := range rules {
			if _, err := tx.Exec(
				`INSERT INTO ozon_price_rules (up_to, multiplier) VALUES (?, ?)`,
				r.UpTo, r.Multiplier); err != nil {
				return err
			}
		}
		return nil
	})
}

// ApplyRule returns the platform price for a shelf price, or 0 when no band
// matches — the caller must not invent a price the ladder does not define.
func ApplyRule(rules []OzonPriceRule, shelf int64) int64 {
	for _, r := range rules {
		if r.UpTo == 0 || shelf < r.UpTo {
			return int64(math.Round(float64(shelf) * r.Multiplier))
		}
	}
	return 0
}

// FillOzonPricesByRules fills the platform price of published products that do
// not have one yet. Prices the owner set are left alone, same as the flat
// markup helper: the ladder is a starting point, not an override.
func (d *Database) FillOzonPricesByRules() (int, error) {
	rules, err := d.OzonPriceRules()
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
		`SELECT l.product_id, p.price FROM ozon_links l
		 JOIN products p ON p.id = l.product_id
		 WHERE l.price = 0 AND l.offer_id != '' AND p.price > 0`)
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
				`UPDATE ozon_links SET price=? WHERE product_id=?`, price, t.id); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}
