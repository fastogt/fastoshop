package database

import "math"

func (d *Database) ShopPriceRules() ([]PriceRule, error) {
	return d.priceRules("shop_price_rules")
}

func (d *Database) SetShopPriceRules(rules []PriceRule) error {
	return d.setPriceRules("shop_price_rules", rules)
}

// ShelfPrice: coefficient then ladder, rounded once at the end to match RecomputePrices.
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
