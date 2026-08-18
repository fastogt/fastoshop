package database

import (
	"database/sql"
	"fmt"
)

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

// AllImages returns every product's photo paths keyed by product id, in
// position order. The feeds render the whole catalogue in one response, and
// per-product ListImages calls would be 20 000 round trips.
// ponytail: the whole table in memory (~60k rows at the proven scale); stream
// row-by-row if catalogues outgrow that.
func (d *Database) AllImages() (map[int64][]string, error) {
	rows, err := d.db.Query(
		`SELECT product_id, path FROM product_images ORDER BY product_id, position`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var path string
		if err := rows.Scan(&id, &path); err != nil {
			return nil, err
		}
		out[id] = append(out[id], path)
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

// SetImageOrder rewrites the positions of a product's photos to the order given.
// The first one is not decoration: it is the card in search results, in the
// catalogue grid and in a marketplace listing, so being able to put a good photo
// there is the whole point of ordering at all.
//
// Ids that do not belong to the product are refused rather than ignored: a
// silently dropped id would leave the gallery in an order nobody asked for.
func (d *Database) SetImageOrder(productID int64, ids []int64) error {
	return d.withTx(func(tx *sql.Tx) error {
		for i, id := range ids {
			res, err := tx.Exec(
				`UPDATE product_images SET position=? WHERE id=? AND product_id=?`,
				i, id, productID)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return fmt.Errorf("image %d does not belong to product %d", id, productID)
			}
		}
		return nil
	})
}
