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
		Price: 250000, Stock: 3, Category: "kitchen"}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	if p.ID == 0 || p.Slug != "krasnyj-chajnik" {
		t.Fatalf("id=%d slug=%q", p.ID, p.Slug)
	}

	// A duplicate title gets a suffix.
	p2 := &Product{SKU: "T-2", Title: "Красный чайник", Price: 1}
	if err := d.CreateProduct(p2); err != nil {
		t.Fatal(err)
	}
	if p2.Slug != "krasnyj-chajnik-2" {
		t.Fatalf("dup slug=%q", p2.Slug)
	}

	got, err := d.GetVisibleProductBySlug("krasnyj-chajnik")
	if err != nil || got.ID != p.ID {
		t.Fatalf("by slug: %v %+v", err, got)
	}

	p.Price = 300000
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}
	list, err := d.ListProducts()
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

// TestParamsKeepTheirTypes: a characteristic is stored as its source stated it,
// and comes back the same. A number that made the round trip as a string is a
// number no filter can compare and no platform card will accept.
func TestParamsKeepTheirTypes(t *testing.T) {
	d := openTest(t)
	p := &Product{SKU: "P-1", Title: "Кружка", Price: 100, Params: []Param{
		{Name: "Материал", Value: "керамика"},
		{Name: "Высота, см", Value: 10.0},
		{Name: "Подходит для СВЧ", Value: true},
		{Name: "Цвет", Value: []any{"синий", "белый"}},
	}}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetProduct(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Params) != 4 {
		t.Fatalf("характеристик %d, ожидалось 4: %+v", len(got.Params), got.Params)
	}
	// The order is the source's, so position is part of what was stored.
	if got.Params[0].Value != "керамика" || got.Params[1].Value != 10.0 ||
		got.Params[2].Value != true {
		t.Errorf("типы не пережили запись и чтение: %+v", got.Params)
	}
	colour, ok := got.Params[3].Value.([]any)
	if !ok || len(colour) != 2 || colour[0] != "синий" {
		t.Errorf("список не пережил запись и чтение: %+v", got.Params[3].Value)
	}
}

// TestParamsDropTheUnreadable: the column is not written by this code alone —
// an older row, a hand-edited database, a source that starts sending objects.
// A characteristic nobody can render is dropped; the ones beside it survive,
// because a card is not worth losing over a colour.
func TestParamsDropTheUnreadable(t *testing.T) {
	d := openTest(t)
	p := &Product{SKU: "P-2", Title: "Ковш", Price: 100}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`[{"name":"Цвет","value":"белый"},{"name":"Бяка","value":{"id":7}},` +
			`{"name":"","value":"без имени"},{"name":"Пусто","value":""},` +
			`{"name":"Нет значения","value":null}]`,
		`{"Цвет":"белый"}`, // the object this column held before the list
		`не json вовсе`,
	} {
		if _, err := d.db.Exec(`UPDATE products SET params = ? WHERE id = ?`, raw, p.ID); err != nil {
			t.Fatal(err)
		}
		got, err := d.GetProduct(p.ID)
		if err != nil {
			t.Fatalf("%q свалило чтение товара: %v", raw, err)
		}
		if len(got.Params) > 1 {
			t.Errorf("мусор доехал до карточки из %q: %+v", raw, got.Params)
		}
	}
}
