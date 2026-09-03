package database

import "testing"

// The list is what the owner ticks boxes against, so it must hold every name the
// catalogue states and nothing else - including from rows written before
// characteristics were a list, which hold an object and must not crash the query.
func TestCatalogParamNames(t *testing.T) {
	d := openTest(t)
	for _, p := range []*Product{
		{SKU: "a", Title: "Чайник", Params: []Param{
			{Name: "Цвет", Value: "белый"}, {Name: "Ставка НДС", Value: "20"}}},
		{SKU: "b", Title: "Ковш", Params: []Param{{Name: "Цвет", Value: "синий"}}},
		{SKU: "c", Title: "Без свойств"},
	} {
		if err := d.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.db.Exec(`UPDATE products SET params='{}' WHERE sku='c'`); err != nil {
		t.Fatal(err)
	}

	names, err := d.CatalogParamNames()
	if err != nil {
		t.Fatal(err)
	}
	// Most used first: the owner meets the decisions that matter at the top.
	if len(names) != 2 || names[0] != "Цвет" || names[1] != "Ставка НДС" {
		t.Fatalf("имена: %+v", names)
	}

	if err := d.SetHiddenParams([]string{"Ставка НДС", "  ", "Ставка НДС"}); err != nil {
		t.Fatal(err)
	}
	hidden, err := d.HiddenParams()
	if err != nil {
		t.Fatal(err)
	}
	if len(hidden) != 1 || !hidden["Ставка НДС"] {
		t.Fatalf("скрытые: %+v", hidden)
	}

	// The set is stated whole: an empty request shows everything again.
	if err := d.SetHiddenParams(nil); err != nil {
		t.Fatal(err)
	}
	if hidden, _ := d.HiddenParams(); len(hidden) != 0 {
		t.Fatalf("набор не очистился: %+v", hidden)
	}
}
