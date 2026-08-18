package database

func (d *Database) WBPriceRules() ([]PriceRule, error) {
	return d.priceRules("wb_price_rules")
}

func (d *Database) SetWBPriceRules(rules []PriceRule) error {
	return d.setPriceRules("wb_price_rules", rules)
}

func (d *Database) FillWBPricesByRules() (int, error) {
	return d.fillPricesByRules("wb_price_rules", "wb_links", "l.nm_id != 0")
}
