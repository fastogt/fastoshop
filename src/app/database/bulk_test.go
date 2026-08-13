package database

import "testing"

func bulkDB(t *testing.T) (*Database, map[string]int64) {
	t.Helper()
	d, err := OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	ids := map[string]int64{}
	for _, p := range []*Product{
		{SKU: "R-1", Title: "Хлеб", Price: 1000, Stock: 0, Supplier: "Ромашка"},
		{SKU: "R-2", Title: "Молоко", Price: 2000, Stock: 0, Supplier: "Ромашка"},
		{SKU: "O-1", Title: "Сыр", Price: 3000, Stock: 4, Supplier: "Оптбаза"},
		{SKU: "M-1", Title: "Свой товар", Price: 4000, Stock: 9},
	} {
		if err := d.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
		ids[p.SKU] = p.ID
	}
	return d, ids
}

// The main scenario: the feed brought zero stock, the owner sets it in one go —
// but only for their own group.
func TestSetStockForWholeGroup(t *testing.T) {
	d, ids := bulkDB(t)
	n, err := d.SetStockBulk(Selection{All: true, Supplier: "Ромашка"}, 7)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("обновлено строк: %d", n)
	}
	for sku, want := range map[string]int{"R-1": 7, "R-2": 7, "O-1": 4, "M-1": 9} {
		p, _ := d.GetProduct(ids[sku])
		if p.Stock != want {
			t.Errorf("%s: остаток %d, ждали %d", sku, p.Stock, want)
		}
	}
}

// "All by filter" respects the search too, otherwise the button would do
// something other than what is shown.
func TestBulkRespectsSearch(t *testing.T) {
	d, ids := bulkDB(t)
	n, err := d.SetStockBulk(Selection{All: true, Supplier: AnySupplier, Q: "Хлеб"}, 5)
	if err != nil || n != 1 {
		t.Fatalf("%v %d", err, n)
	}
	p, _ := d.GetProduct(ids["R-1"])
	if p.Stock != 5 {
		t.Errorf("остаток: %d", p.Stock)
	}
	p, _ = d.GetProduct(ids["R-2"])
	if p.Stock != 0 {
		t.Errorf("задет товар вне поиска: %d", p.Stock)
	}
}

// An empty selection without the "all" flag must touch nothing: otherwise a
// misclick on the button would rewrite the catalog.
func TestBulkEmptySelectionTouchesNothing(t *testing.T) {
	d, ids := bulkDB(t)
	n, err := d.SetStockBulk(Selection{Supplier: AnySupplier}, 99)
	if err != nil || n != 0 {
		t.Fatalf("%v %d", err, n)
	}
	p, _ := d.GetProduct(ids["M-1"])
	if p.Stock != 9 {
		t.Errorf("остаток изменился: %d", p.Stock)
	}
}

func TestBulkByIDs(t *testing.T) {
	d, ids := bulkDB(t)
	n, err := d.SetHiddenBulk(Selection{IDs: []int64{ids["R-1"], ids["M-1"]}}, true)
	if err != nil || n != 2 {
		t.Fatalf("%v %d", err, n)
	}
	for sku, want := range map[string]bool{"R-1": true, "M-1": true, "R-2": false} {
		p, _ := d.GetProduct(ids[sku])
		if p.Hidden != want {
			t.Errorf("%s: hidden %v", sku, p.Hidden)
		}
	}
}

func TestBulkRejectsNegativeStock(t *testing.T) {
	d, _ := bulkDB(t)
	if _, err := d.SetStockBulk(Selection{All: true, Supplier: AnySupplier}, -1); err == nil {
		t.Error("отрицательный остаток принят")
	}
}

// Moving between groups: the SKU changed its supplier.
func TestBulkMoveBetweenSuppliers(t *testing.T) {
	d, ids := bulkDB(t)
	if _, err := d.SetSupplierBulk(Selection{IDs: []int64{ids["R-1"]}}, "Оптбаза"); err != nil {
		t.Fatal(err)
	}
	p, _ := d.GetProduct(ids["R-1"])
	if p.Supplier != "Оптбаза" {
		t.Errorf("группа: %q", p.Supplier)
	}
}

// Deletion works only by an explicit list: there is no "wipe all by filter" mode.
func TestBulkDeleteNeedsExplicitIDs(t *testing.T) {
	d, ids := bulkDB(t)
	n, err := d.DeleteProductsBulk(nil)
	if err != nil || n != 0 {
		t.Fatalf("%v %d", err, n)
	}
	if n, err := d.DeleteProductsBulk([]int64{ids["O-1"]}); err != nil || n != 1 {
		t.Fatalf("%v %d", err, n)
	}
	if _, err := d.GetProduct(ids["O-1"]); err == nil {
		t.Error("товар не удалён")
	}
	if _, err := d.GetProduct(ids["M-1"]); err != nil {
		t.Error("удалено лишнее")
	}
}

// Sorting runs in SQL over the whole catalog, not over the loaded page.
func TestSortingIsServerSide(t *testing.T) {
	d, _ := bulkDB(t)
	asc, err := d.ListProductsSorted("", AnySupplier, "price", false, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(asc) != 2 || asc[0].Price != 1000 || asc[1].Price != 2000 {
		t.Fatalf("по возрастанию: %+v", asc)
	}
	desc, _ := d.ListProductsSorted("", AnySupplier, "price", true, 2, 0)
	if desc[0].Price != 4000 {
		t.Fatalf("по убыванию: %+v", desc)
	}
	// An unknown column must not make it into the SQL.
	if _, err := d.ListProductsSorted("", AnySupplier, "price; DROP TABLE products", false, 5, 0); err != nil {
		t.Fatalf("инъекция в ORDER BY: %v", err)
	}
	if n, _ := d.CountProducts("", AnySupplier); n != 4 {
		t.Fatalf("таблица пострадала: %d", n)
	}
}
