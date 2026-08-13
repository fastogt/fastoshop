package database

import "database/sql"

// withTx is the only way to write within a transaction in this project.
//
// The pool has a single connection (SetMaxOpenConns(1)): while the transaction
// lives, that connection is occupied by it. Any call through d.* inside fn
// would go for a second connection and hang forever, so fn must work only
// through the passed tx.
//
// The rollback is deferred: it fires both on error and on panic; after a
// successful Commit it is a no-op (sql.ErrTxDone).
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
