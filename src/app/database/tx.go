package database

import "database/sql"

// withTx — единственный способ писать транзакцией в этом проекте.
//
// Пул из одного соединения (SetMaxOpenConns(1)): пока транзакция жива, это
// соединение занято ею. Любой вызов через d.* внутри fn уйдёт за вторым
// соединением и повиснет навсегда, поэтому fn обязан работать только через
// переданный tx.
//
// Откат отложен: он срабатывает и на ошибке, и на панике; после успешного
// Commit это no-op (sql.ErrTxDone).
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
