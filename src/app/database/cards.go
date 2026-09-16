package database

// PlatformCards is what a product page needs to link to the seller's own card:
// the Wildberries number and the Ozon one. A zero means there is no card, or no
// key to have learned its number with.
func (d *Database) PlatformCards(productID int64) (nmID, sku int64) {
	_ = d.db.QueryRow(`SELECT nm_id FROM wb_links WHERE product_id = ?`, productID).Scan(&nmID)
	_ = d.db.QueryRow(`SELECT sku FROM ozon_links WHERE product_id = ?`, productID).Scan(&sku)
	return nmID, sku
}

// SetOzonSKU stores the number Ozon builds its own address from.
func (d *Database) SetOzonSKU(offerID string, sku int64) error {
	_, err := d.db.Exec(`UPDATE ozon_links SET sku = ? WHERE offer_id = ?`, sku, offerID)
	return err
}
