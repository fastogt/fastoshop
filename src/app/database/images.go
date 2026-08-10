package database

type ProductImage struct {
	ID        int64  `json:"id"`
	ProductID int64  `json:"product_id"`
	Path      string `json:"path"`
	Position  int    `json:"position"`
}

func (d *Database) AddImage(productID int64, path string) error {
	_, err := d.db.Exec(
		`INSERT INTO product_images (product_id, path, position)
		 VALUES (?, ?, COALESCE((SELECT MAX(position)+1 FROM product_images WHERE product_id=?), 0))`,
		productID, path, productID)
	return err
}

func (d *Database) ListImages(productID int64) ([]ProductImage, error) {
	rows, err := d.db.Query(
		`SELECT id, product_id, path, position FROM product_images
		 WHERE product_id=? ORDER BY position`, productID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ProductImage
	for rows.Next() {
		var im ProductImage
		if err := rows.Scan(&im.ID, &im.ProductID, &im.Path, &im.Position); err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, rows.Err()
}

// GetImage is what deletion needs to know whether a file has to go with the
// row: an imported photo is a link to the supplier's server, an uploaded one is
// a file of ours.
func (d *Database) GetImage(id int64) (*ProductImage, error) {
	var im ProductImage
	err := d.db.QueryRow(
		`SELECT id, product_id, path, position FROM product_images WHERE id=?`, id).
		Scan(&im.ID, &im.ProductID, &im.Path, &im.Position)
	if err != nil {
		return nil, err
	}
	return &im, nil
}

// ListRemoteImages returns the photos still living on someone else's server for
// the products in the selection. It is what "download the photos" works from:
// the rows are already in the right order, and only their path changes.
func (d *Database) ListRemoteImages(s Selection) ([]ProductImage, error) {
	where, args, ok := s.where()
	if !ok {
		return nil, nil
	}
	rows, err := d.db.Query(
		`SELECT i.id, i.product_id, i.path, i.position FROM product_images i
		 WHERE i.path LIKE 'http%' AND i.product_id IN (SELECT id FROM products`+where+`)
		 ORDER BY i.product_id, i.position`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []ProductImage
	for rows.Next() {
		var im ProductImage
		if err := rows.Scan(&im.ID, &im.ProductID, &im.Path, &im.Position); err != nil {
			return nil, err
		}
		out = append(out, im)
	}
	return out, rows.Err()
}

// SetImagePath swaps a downloaded photo in without touching its position: the
// first photo is the card in search results and on the channel, and parallel
// downloads reinserting rows would shuffle that order.
func (d *Database) SetImagePath(id int64, path string) error {
	_, err := d.db.Exec(`UPDATE product_images SET path=? WHERE id=?`, path, id)
	return err
}

func (d *Database) DeleteImage(id int64) error {
	_, err := d.db.Exec(`DELETE FROM product_images WHERE id=?`, id)
	return err
}
