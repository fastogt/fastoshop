package database

import (
	"database/sql"
	"fmt"
	"strings"
)

// kImportChunk trades WAL appends per row against holding the single connection.
// ponytail: a fixed chunk; make it adaptive if a catalogue proves either end wrong.
const kImportChunk = 500

// ImportWriter holds an open transaction; the single-connection pool blocks other writers.
type ImportWriter struct {
	d     *Database
	tx    *sql.Tx
	slugs map[string]bool
	n     int
}

func (d *Database) NewImportWriter() (*ImportWriter, error) {
	rows, err := d.db.Query(`SELECT slug FROM products`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	slugs := map[string]bool{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		slugs[s] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	w := &ImportWriter{d: d, slugs: slugs}
	return w, w.begin()
}

func (w *ImportWriter) begin() error {
	tx, err := w.d.db.Begin()
	if err != nil {
		return err
	}
	w.tx = tx
	return nil
}

func (w *ImportWriter) tick() error {
	w.n++
	if w.n%kImportChunk != 0 {
		return nil
	}
	if err := w.tx.Commit(); err != nil {
		return err
	}
	return w.begin()
}

// Close commits pending work, or rolls it back on error; earlier chunks stay committed.
func (w *ImportWriter) Close(err error) error {
	if err != nil {
		_ = w.tx.Rollback()
		return err
	}
	return w.tx.Commit()
}

func (w *ImportWriter) CreateProduct(p *Product) error {
	base := slugBase(p.Title)
	slug := base
	for n := 2; w.slugs[slug]; n++ {
		slug = fmt.Sprintf("%s-%d", base, n)
	}
	p.Slug = slug
	if err := insertProduct(w.tx, p); err != nil {
		return err
	}
	w.slugs[slug] = true
	return w.tick()
}

func (w *ImportWriter) UpdateProduct(p *Product) error {
	if err := updateProduct(w.tx, p); err != nil {
		return err
	}
	return w.tick()
}

// AddImages assumes a new product: positions start at zero.
func (w *ImportWriter) AddImages(productID int64, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := make([]any, 0, 3*len(paths))
	for i, p := range paths {
		args = append(args, productID, p, i)
	}
	_, err := w.tx.Exec(`INSERT INTO product_images (product_id, path, position) VALUES `+
		strings.TrimSuffix(strings.Repeat("(?,?,?),", len(paths)), ","), args...)
	if err != nil {
		return err
	}
	return w.tick()
}

// ZeroStock takes the given products off sale in one statement per chunk.
func (w *ImportWriter) ZeroStock(ids []int64) error {
	for len(ids) > 0 {
		n := min(len(ids), kImportChunk)
		in, args := inClause(ids[:n])
		ids = ids[n:]
		if _, err := w.tx.Exec(`UPDATE products SET stock=0, updated_at=CURRENT_TIMESTAMP
			WHERE id IN (`+in+`)`, args...); err != nil {
			return err
		}
		if err := w.tick(); err != nil {
			return err
		}
	}
	return nil
}
