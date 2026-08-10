package database

import "testing"

func openTest(t *testing.T) *Database {
	t.Helper()
	d, err := OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestProductCRUD(t *testing.T) {
	d := openTest(t)
	p := &Product{SKU: "T-1", Title: "Красный чайник", Description: "Отличный",
		Price: 250000, Currency: "RUB", Stock: 3, Category: "kitchen"}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	if p.ID == 0 || p.Slug != "krasnyj-chajnik" {
		t.Fatalf("id=%d slug=%q", p.ID, p.Slug)
	}

	// Дубликат названия получает суффикс.
	p2 := &Product{SKU: "T-2", Title: "Красный чайник", Price: 1, Currency: "RUB"}
	if err := d.CreateProduct(p2); err != nil {
		t.Fatal(err)
	}
	if p2.Slug != "krasnyj-chajnik-2" {
		t.Fatalf("dup slug=%q", p2.Slug)
	}

	got, err := d.GetProductBySlug("krasnyj-chajnik")
	if err != nil || got.ID != p.ID {
		t.Fatalf("by slug: %v %+v", err, got)
	}

	p.Price = 300000
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}
	list, err := d.ListProducts("")
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v n=%d", err, len(list))
	}
	if err := d.DeleteProduct(p2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetProduct(p2.ID); err == nil {
		t.Fatal("deleted product still readable")
	}
}
