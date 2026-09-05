package importer

import (
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// One feed has no right to touch another supplier's price or stock.
func TestImportTouchesOnlyItsOwnGroup(t *testing.T) {
	d := mergeDB(t)

	// A second supplier's product and the owner's own product.
	other := &database.Product{SKU: "OTHER-1", Title: "Чужой", Price: 5000,
		SourcePrice: 5000, Stock: 9, Supplier: "Второй"}
	mine := &database.Product{SKU: "MY-1", Title: "Мой", Price: 7000, Stock: 4}
	for _, p := range []*database.Product{other, mine} {
		if err := d.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
	}

	src := &feed{name: "yml", items: []Item{{SKU: "P-1", Title: "Хлеб", Price: 1000, Stock: 3}}}
	if _, err := Run(src, d, "Ромашка", 1, nil); err != nil {
		t.Fatal(err)
	}
	// A second pass with no products of this supplier: zeroing stays in its group.
	src.items = []Item{{SKU: "P-2", Title: "Молоко", Price: 2000, Stock: 1}}
	res, err := Run(src, d, "Ромашка", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Zeroed != 1 {
		t.Fatalf("only our own items should be zeroed: %+v", res)
	}

	got, _ := d.GetProduct(other.ID)
	if got.Stock != 9 || got.Price != 5000 {
		t.Errorf("second supplier's product was harmed: %+v", got)
	}
	got, _ = d.GetProduct(mine.ID)
	if got.Stock != 4 || got.Price != 7000 {
		t.Errorf("owner's own product was harmed: %+v", got)
	}
}

// One SKU across two suppliers is a conflict, not a silent takeover.
func TestArticleOwnedByAnotherGroupIsAConflict(t *testing.T) {
	d := mergeDB(t)
	mine := &database.Product{SKU: "A", Title: "Мой чайник", Price: 9999,
		SourcePrice: 9999, Stock: 2, Supplier: "Своё"}
	if err := d.CreateProduct(mine); err != nil {
		t.Fatal(err)
	}

	src := &feed{name: "yml", items: []Item{{SKU: "A", Title: "Чайник", Price: 1000, Stock: 50}}}
	res, err := Run(src, d, "Ромашка", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || res.Imported != 0 || res.Updated != 0 {
		t.Fatalf("%+v", res)
	}
	got, _ := d.GetProduct(mine.ID)
	if got.Price != 9999 || got.Stock != 2 || got.Supplier != "Своё" {
		t.Errorf("foreign feed hijacked the product: %+v", got)
	}

	// And in the preview it is also a conflict, not a new arrival.
	existing, _ := d.ListProducts()
	dif := Compare(src.items, existing, "Ромашка", 1, nil)
	if dif.Conflicts != 1 || dif.New != 0 || dif.Gone != 0 {
		t.Fatalf("diff: %+v", dif)
	}
}

// Garbage in the export must neither crash nor spawn products.
func TestImportSkipsUnusableRows(t *testing.T) {
	d := mergeDB(t)
	src := &feed{name: "yml", items: []Item{
		{Title: "Без артикула", Price: 1000, Stock: 1},
		{SKU: "DUP", Title: "Дубль", Price: 1000, Stock: 1},
		{SKU: "DUP", Title: "Дубль ещё раз", Price: 1000, Stock: 1},
		{SKU: "FREE", Title: "Без цены", Price: 0, Stock: 1},
		{SKU: "NEG", Title: "Минус на складе", Price: 1000, Stock: -5},
	}}
	res, err := Run(src, d, "Ромашка", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 3 || res.Imported != 2 {
		t.Fatalf("%+v", res)
	}
	neg, err := d.GetVisibleProductBySlug("minus-na-sklade")
	if err != nil || neg.Stock != 0 {
		t.Fatalf("negative stock: %v %+v", err, neg)
	}

	// Re-uploading the same garbage creates nothing anew.
	res, err = Run(src, d, "Ромашка", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 0 {
		t.Fatalf("duplicates on re-import: %+v", res)
	}
}
