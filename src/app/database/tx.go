package database

import "database/sql"

// withTx: the pool has one connection, so fn must write only through the passed tx.
func (d *Database) withTx(fn func(*sql.Tx) error) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
