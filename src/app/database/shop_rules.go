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
func ShelfPrice(rules []PriceRule, sourcePrice int64, coefficient float64) int64 {
	cost := int64(math.Round(float64(sourcePrice) * coefficient))
	if len(rules) == 0 {
		return cost
	}
	return ApplyRule(rules, cost)
}
