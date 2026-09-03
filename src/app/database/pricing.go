package database

import (
	"fmt"
	"strings"
)

// kMaxCoefficient is a sanity ceiling, not a business rule: a typo like 3000
// instead of 30 would silently multiply a whole catalogue by a hundred, and the
// owner would find out from the storefront.
const kMaxCoefficient = 1000

func ValidCoefficient(c float64) bool { return c > 0 && c <= kMaxCoefficient }

func (d *Database) PriceCoefficient() (float64, error) {
	var c float64
	err := d.db.QueryRow(`SELECT price_coefficient FROM settings WHERE id=1`).Scan(&c)
	if err != nil {
		// A shop without a settings row yet is not broken: it simply has no
		// coefficient, and 1 leaves the source price as it is.
		return 1, nil
	}
	return c, nil
}

func (d *Database) SetPriceCoefficient(c float64) error {
	if !ValidCoefficient(c) {
		return fmt.Errorf("invalid price coefficient: %v", c)
	}
	// UPDATE only: an INSERT here would forge a settings row with no owner, and
	// the settings row is where the owner's credentials live.
	_, err := d.db.Exec(`UPDATE settings SET price_coefficient=? WHERE id=1`, c)
	return err
}

// ApplyPriceCoefficient recomputes shelf prices from the source prices in one
// statement - 20 000 rows is a single UPDATE, not twenty thousand round trips.
// Rows the owner priced by hand and rows with no source are left alone.
func (d *Database) ApplyPriceCoefficient(c float64) (int, error) {
	rules, err := d.ShopPriceRules()
	if err != nil {
		return 0, err
	}
	return d.RecomputePrices(c, rules)
}

// RecomputePrices rebuilds every shelf price from the source price: the
// coefficient carries it into the shop's money, the ladder adds the margin.
//
// One UPDATE with the ladder inlined as a CASE, not a loop: a catalogue of
// twenty thousand would otherwise be twenty thousand round trips to do
// arithmetic SQLite can do in one pass. The bands are compared against the cost,
// which is what the owner sees in the price column.
func (d *Database) RecomputePrices(c float64, rules []PriceRule) (int, error) {
	if !ValidCoefficient(c) {
		return 0, fmt.Errorf("invalid price coefficient: %v", c)
	}
	if err := ValidPriceRules(rules); err != nil {
		return 0, err
	}
	if err := d.SetPriceCoefficient(c); err != nil {
		return 0, err
	}
	sortRules(rules)

	expr := "source_price * ?"
	args := []any{c}
	// A single band is the plain "percent" case from the Products screen: the
	// ladder has one row, "and above", and CASE with no WHEN is a syntax error.
	if len(rules) == 1 {
		expr = "(source_price * ?) * ?"
		args = []any{c, rules[0].Multiplier}
	} else if len(rules) > 1 {
		var sb strings.Builder
		sb.WriteString("(source_price * ?) * CASE")
		args = []any{c}
		for _, r := range rules {
			if r.UpTo == 0 {
				continue
			}
			sb.WriteString(" WHEN source_price * ? < ? THEN ?")
			args = append(args, c, r.UpTo, r.Multiplier)
		}
		for _, r := range rules {
			if r.UpTo == 0 {
				sb.WriteString(" ELSE ?")
				args = append(args, r.Multiplier)
			}
		}
		sb.WriteString(" END")
		expr = sb.String()
	}

	res, err := d.db.Exec(
		`UPDATE products
		 SET price = CAST(ROUND(`+expr+`) AS INTEGER),
		     updated_at = CURRENT_TIMESTAMP
		 WHERE price_manual = 0 AND source_price > 0`, args...)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
