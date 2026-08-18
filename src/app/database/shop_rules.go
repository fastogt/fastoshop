package database

import "math"

func (d *Database) ShopPriceRules() ([]PriceRule, error) {
	return d.priceRules("shop_price_rules")
}

func (d *Database) SetShopPriceRules(rules []PriceRule) error {
	return d.setPriceRules("shop_price_rules", rules)
}

// ShelfPrice turns a supplier's price into the one on the shelf: the coefficient
// carries it into the shop's money, the ladder adds the margin. Two steps, not
// one, because they answer different questions — the rate is arithmetic, the
// markup is a decision — and because bands set in the shop's own currency are
// the only ones an owner can reason about.
//
// An empty ladder means what the shop did before it existed: cost is the price.
//
// One rounding at the end, not one per step: RecomputePrices does the same
// arithmetic as a single SQL expression, and rounding the cost first disagrees
// with it by a kopeck on almost half a live catalogue — which an import then
// reports as thousands of "changed" prices that never moved.
func ShelfPrice(rules []PriceRule, sourcePrice int64, coefficient float64) int64 {
	cost := float64(sourcePrice) * coefficient
	for _, r := range rules {
		if r.UpTo == 0 || cost < float64(r.UpTo) {
			return int64(math.Round(cost * r.Multiplier))
		}
	}
	if len(rules) == 0 {
		return int64(math.Round(cost))
	}
	return 0
}
