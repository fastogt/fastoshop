package database

func (d *Database) OzonPriceRules() ([]PriceRule, error) {
	return d.priceRules("ozon_price_rules")
}

func (d *Database) SetOzonPriceRules(rules []PriceRule) error {
	return d.setPriceRules("ozon_price_rules", rules)
}

func (d *Database) FillOzonPricesByRules() (int, error) {
	return d.fillPricesByRules("ozon_price_rules", "ozon_links", "l.offer_id != ''")
}
