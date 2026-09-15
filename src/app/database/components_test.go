package database

import (
	"errors"
	"testing"
	"time"
)

// A set's stock follows its components through checkout, cancel, bulk edits and a WB sale.
func TestSetStockFollowsComponents(t *testing.T) {
	d := openFile(t)
	pork := product(t, d, "Свинина", 120)
	beef := product(t, d, "Говядина", 80)
	box := product(t, d, "Свинина ×12", 0)
	mix := product(t, d, "Ассорти 3+3", 0)
	if err := d.SetComponents(box.ID, []Component{{ProductID: pork.ID, Qty: 12}}); err != nil {
		t.Fatal(err)
	}
	if err := d.SetComponents(mix.ID, []Component{{ProductID: pork.ID, Qty: 3}, {ProductID: beef.ID, Qty: 3}}); err != nil {
		t.Fatal(err)
	}
	want := func(step string, p, b, bx, mx int) {
		t.Helper()
		got := []int{stockOf(t, d, pork.ID), stockOf(t, d, beef.ID), stockOf(t, d, box.ID), stockOf(t, d, mix.ID)}
		if got[0] != p || got[1] != b || got[2] != bx || got[3] != mx {
			t.Fatalf("%s: pork/beef/box/mix = %v, want %v", step, got, []int{p, b, bx, mx})
		}
	}
	want("start", 120, 80, 10, 26)

	o, items := order(OrderItem{ProductID: mix.ID, Qty: 2})
	if err := d.CreateOrderWithStock(o, items); err != nil {
		t.Fatal(err)
	}
	want("two mixes sold", 114, 74, 9, 24)
	if err := d.SetOrderStatus(o.ID, "cancelled"); err != nil {
		t.Fatal(err)
	}
	want("cancelled", 120, 80, 10, 26)

	if err := d.SetComponents(mix.ID, []Component{{ProductID: box.ID, Qty: 1}}); !errors.Is(err, ErrNested) {
		t.Fatalf("set inside a set: %v", err)
	}
	if err := d.SetComponents(pork.ID, []Component{{ProductID: beef.ID, Qty: 1}}); !errors.Is(err, ErrNested) {
		t.Fatalf("component given a composition: %v", err)
	}

	if _, err := d.SetStockBulk(Selection{All: true}, 999); err != nil {
		t.Fatal(err)
	}
	want("bulk 999", 999, 999, 83, 333)
	mix.Stock = 5
	if err := d.UpdateProduct(mix); err != nil {
		t.Fatal(err)
	}
	want("hand edit of a set", 999, 999, 83, 333)

	beef.Stock = -5
	if err := d.UpdateProduct(beef); err != nil {
		t.Fatal(err)
	}
	want("negative component", 999, -5, 83, 0)
	beef.Stock = 80
	pork.Stock = 120
	for _, p := range []*Product{beef, pork} {
		if err := d.UpdateProduct(p); err != nil {
			t.Fatal(err)
		}
	}

	if err := d.UpsertWBLink(&WBLink{ProductID: mix.ID, NmID: 1, Barcode: "mix"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ApplyWBOrder(&WBOrder{OrderID: 7, Status: "new", Barcode: "mix", Qty: 1, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	want("WB sold a mix", 117, 77, 9, 25)
	if _, err := d.SetWBOrderStatus(7, "", true); err != nil {
		t.Fatal(err)
	}
	want("WB cancel", 120, 80, 10, 26)

	if err := d.DeleteProduct(beef.ID); err != nil {
		t.Fatal(err)
	}
	if got := stockOf(t, d, mix.ID); got != 40 {
		t.Fatalf("mix without beef is pork/3: %d", got)
	}
}

// A packed set keeps its own stock and sells itself; unpacking hands the stock back to the triggers.
func TestPackedSetKeepsItsOwnStock(t *testing.T) {
	d := openFile(t)
	pork := product(t, d, "Свинина", 120)
	beef := product(t, d, "Говядина", 80)
	mix := &Product{Title: "Ассорти 3+3", Price: 100, Stock: 5, Packed: true}
	if err := d.CreateProduct(mix); err != nil {
		t.Fatal(err)
	}
	if err := d.SetComponents(mix.ID, []Component{{ProductID: pork.ID, Qty: 3}, {ProductID: beef.ID, Qty: 3}}); err != nil {
		t.Fatal(err)
	}
	if got := stockOf(t, d, mix.ID); got != 5 {
		t.Fatalf("packed set took the derived stock: %d", got)
	}
	o, items := order(OrderItem{ProductID: mix.ID, Qty: 2})
	if err := d.CreateOrderWithStock(o, items); err != nil {
		t.Fatal(err)
	}
	if stockOf(t, d, mix.ID) != 3 || stockOf(t, d, pork.ID) != 120 || stockOf(t, d, beef.ID) != 80 {
		t.Fatalf("packed sale: mix %d pork %d beef %d", stockOf(t, d, mix.ID), stockOf(t, d, pork.ID), stockOf(t, d, beef.ID))
	}
	if _, err := d.SetStockBulk(Selection{IDs: []int64{mix.ID}}, 9); err != nil {
		t.Fatal(err)
	}
	if got := stockOf(t, d, mix.ID); got != 9 {
		t.Fatalf("bulk stock skipped a packed set: %d", got)
	}

	mix, _ = d.GetProduct(mix.ID)
	mix.Packed = false
	if err := d.UpdateProduct(mix); err != nil {
		t.Fatal(err)
	}
	if got := stockOf(t, d, mix.ID); got != 26 {
		t.Fatalf("unpacked set must count from components: %d", got)
	}

	mix, _ = d.GetProduct(mix.ID)
	mix.Packed, mix.Stock = true, 4
	if err := d.UpdateProduct(mix); err != nil {
		t.Fatal(err)
	}
	if got := stockOf(t, d, mix.ID); got != 4 {
		t.Fatalf("packing must keep the stock typed with it: %d", got)
	}
}
