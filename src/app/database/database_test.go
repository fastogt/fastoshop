package database

import (
	"path/filepath"
	"testing"
)

func TestFileDatabasePragmas(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "shop.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	var mode string
	if err := d.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode %q, want wal", mode)
	}
	var sync int
	if err := d.db.QueryRow(`PRAGMA synchronous`).Scan(&sync); err != nil {
		t.Fatal(err)
	}
	if sync != 1 { // NORMAL
		t.Fatalf("synchronous %d, want 1 (NORMAL)", sync)
	}
	var fk int
	if err := d.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil {
		t.Fatal(err)
	}
	if fk != 1 {
		t.Fatal("foreign_keys must stay on")
	}
}

func TestReopenKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shop.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.CreateProduct(&Product{Title: "Товар"}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}

	d2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d2.Close() }()
	list, err := d2.ListProducts()
	if err != nil || len(list) != 1 || list[0].Title != "Товар" {
		t.Fatalf("%v %+v", err, list)
	}
}
