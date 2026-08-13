package database

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func openFile(t *testing.T) *Database {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "shop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func product(t *testing.T, d *Database, title string, stock int) *Product {
	t.Helper()
	p := &Product{Title: title, Price: 100, Stock: stock}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func stockOf(t *testing.T, d *Database, id int64) int {
	t.Helper()
	p, err := d.GetProduct(id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Stock
}

func order(items ...OrderItem) (*Order, []OrderItem) {
	return &Order{Name: "Иван", Phone: "+7999", ItemsJSON: "[]"}, items
}

// Race for the last unit: exactly one winner, stock does not go negative.
func TestConcurrentCheckoutLastUnit(t *testing.T) {
	d := openFile(t)
	p := product(t, d, "Чайник", 1)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			o, items := order(OrderItem{ProductID: p.ID, Qty: 1})
			errs[i] = d.CreateOrderWithStock(o, items)
		}()
	}
	wg.Wait()

	failed := 0
	for _, err := range errs {
		if err == nil {
			continue
		}
		var oos *OutOfStockError
		if !errors.As(err, &oos) {
			t.Fatalf("unexpected error: %v", err)
		}
		failed++
	}
	if failed != 1 {
		t.Fatalf("exactly one checkout must fail, %d did", failed)
	}
	if got := stockOf(t, d, p.ID); got != 0 {
		t.Fatalf("stock %d, want 0", got)
	}
	orders, _ := d.ListOrders()
	if len(orders) != 1 {
		t.Fatalf("orders %d, want 1", len(orders))
	}
}

// One of three lines fell short — no order, no lines, and no stock movement on
// the neighboring products must remain.
func TestCheckoutRollsBackWholeCart(t *testing.T) {
	d := openFile(t)
	a := product(t, d, "Чайник", 5)
	short := product(t, d, "Стакан", 1)
	c := product(t, d, "Ложка", 5)

	o, items := order(
		OrderItem{ProductID: a.ID, Qty: 2},
		OrderItem{ProductID: short.ID, Qty: 3},
		OrderItem{ProductID: c.ID, Qty: 1})
	var oos *OutOfStockError
	if err := d.CreateOrderWithStock(o, items); !errors.As(err, &oos) {
		t.Fatalf("want OutOfStockError, got %v", err)
	}
	if oos.ProductID != short.ID || oos.Name() != "Стакан" {
		t.Fatalf("error must name the short product: %+v", oos)
	}
	for _, tc := range []struct {
		id   int64
		want int
	}{{a.ID, 5}, {short.ID, 1}, {c.ID, 5}} {
		if got := stockOf(t, d, tc.id); got != tc.want {
			t.Errorf("stock of %d: %d, want %d", tc.id, got, tc.want)
		}
	}
	if orders, _ := d.ListOrders(); len(orders) != 0 {
		t.Fatalf("no order must survive: %+v", orders)
	}
	var n int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM order_items`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("order_items rows left: %d", n)
	}
}

func TestOrderStatusStockMovesOnce(t *testing.T) {
	d := openFile(t)
	p := product(t, d, "Чайник", 5)
	o, items := order(OrderItem{ProductID: p.ID, Qty: 2})
	if err := d.CreateOrderWithStock(o, items); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		status string
		want   int
	}{
		{"done", 3},
		{"cancelled", 5},
		{"cancelled", 5}, // a repeat does not return a second time
		{"new", 3},
		{"done", 3}, // movement only on transitions through cancelled
		{"cancelled", 5},
	}
	for _, s := range steps {
		if err := d.SetOrderStatus(o.ID, s.status); err != nil {
			t.Fatalf("%s: %v", s.status, err)
		}
		if got := stockOf(t, d, p.ID); got != s.want {
			t.Fatalf("after %s stock %d, want %d", s.status, got, s.want)
		}
	}
}

func TestUncancelRefusedWhenStockGone(t *testing.T) {
	d := openFile(t)
	p := product(t, d, "Чайник", 1)
	o, items := order(OrderItem{ProductID: p.ID, Qty: 1})
	if err := d.CreateOrderWithStock(o, items); err != nil {
		t.Fatal(err)
	}
	if err := d.SetOrderStatus(o.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	// The returned unit was sold to another buyer.
	o2, items2 := order(OrderItem{ProductID: p.ID, Qty: 1})
	if err := d.CreateOrderWithStock(o2, items2); err != nil {
		t.Fatal(err)
	}

	var oos *OutOfStockError
	if err := d.SetOrderStatus(o.ID, "new"); !errors.As(err, &oos) {
		t.Fatalf("want OutOfStockError, got %v", err)
	}
	if got := stockOf(t, d, p.ID); got != 0 {
		t.Errorf("stock %d, want 0", got)
	}
	orders, _ := d.ListOrders()
	for _, got := range orders {
		if got.ID == o.ID && got.Status != "cancelled" {
			t.Errorf("order must stay cancelled, got %q", got.Status)
		}
	}
}

// An order from the days before stock tracking: nothing was deducted, so a
// cancellation returns nothing.
func TestLegacyOrderDoesNotRestock(t *testing.T) {
	d := openFile(t)
	p := product(t, d, "Чайник", 3)
	o := &Order{Name: "Иван", Phone: "+7999", ItemsJSON: "[]"}
	if err := d.CreateOrder(o); err != nil {
		t.Fatal(err)
	}
	if err := d.SetOrderStatus(o.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if got := stockOf(t, d, p.ID); got != 3 {
		t.Fatalf("stock %d, want 3", got)
	}
}

// The product was deleted after the order: the order history stays intact, and
// there is nowhere to return the stock.
func TestRestockSkipsDeletedProduct(t *testing.T) {
	d := openFile(t)
	p := product(t, d, "Чайник", 2)
	o, items := order(OrderItem{ProductID: p.ID, Qty: 1})
	if err := d.CreateOrderWithStock(o, items); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteProduct(p.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.SetOrderStatus(o.ID, "cancelled"); err != nil {
		t.Fatalf("cancel must survive a deleted product: %v", err)
	}
	var n int
	if err := d.db.QueryRow(
		`SELECT COUNT(*) FROM order_items WHERE order_id=?`, o.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("order history lost: %d rows", n)
	}
}
