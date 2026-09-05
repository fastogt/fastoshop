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

// ImagesFor returns photos for a whole page of products in one query.
func (d *Database) ImagesFor(ids []int64) (map[int64][]ProductImage, error) {
	out := map[int64][]ProductImage{}
	if len(ids) == 0 {
		return out, nil
	}
	in, args := inClause(ids)
	rows, err := d.db.Query(
		`SELECT id, product_id, path, position FROM product_images
		 WHERE product_id IN (`+in+`) ORDER BY product_id, position`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var im ProductImage
		if err := rows.Scan(&im.ID, &im.ProductID, &im.Path, &im.Position); err != nil {
			return nil, err
		}
		out[im.ProductID] = append(out[im.ProductID], im)
	}
	return out, rows.Err()
}

// AllImages feeds the whole-catalogue exports, which render in one response.
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

// GetImage: path is a supplier URL for imported photos, a local file for uploads.
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

func (d *Database) CountRemoteImages(s Selection) (main, total int, err error) {
	where, args, ok := s.where()
	if !ok {
		return 0, 0, nil
	}
	row := d.db.QueryRow(
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN i.position = (SELECT MIN(j.position)
		            FROM product_images j WHERE j.product_id = i.product_id)
		          THEN 1 ELSE 0 END), 0)
		 FROM product_images i
		 WHERE i.path LIKE 'http%' AND i.product_id IN (SELECT id FROM products`+where+`)`,
		args...)
	err = row.Scan(&total, &main)
	return main, total, err
}

// ListRemoteImages lists photos still on a supplier's server; mainOnly keeps the first.
func (d *Database) ListRemoteImages(s Selection, mainOnly bool) ([]ProductImage, error) {
	where, args, ok := s.where()
	if !ok {
		return nil, nil
	}
	main := ""
	if mainOnly {
		main = ` AND i.position = (SELECT MIN(j.position) FROM product_images j
			WHERE j.product_id = i.product_id)`
	}
	rows, err := d.db.Query(
		`SELECT i.id, i.product_id, i.path, i.position FROM product_images i
		 WHERE i.path LIKE 'http%' AND i.product_id IN (SELECT id FROM products`+where+`)`+
			main+`
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

// SetImagePath updates in place: position picks the main photo, so rows must not move.
func (d *Database) SetImagePath(id int64, path string) error {
	_, err := d.db.Exec(`UPDATE product_images SET path=? WHERE id=?`, path, id)
	return err
}

func (d *Database) DeleteImage(id int64) error {
	_, err := d.db.Exec(`DELETE FROM product_images WHERE id=?`, id)
	return err
}

// SetImageOrder renumbers positions; position 0 is the main photo. Foreign ids error.
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
