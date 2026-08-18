package database

import "testing"

// Recalculation must start from the source price, not the current one:
// otherwise batches brought in at different rates drift apart, and rounding
// accumulates.
func TestApplyPriceCoefficient(t *testing.T) {
	d, err := OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if err := d.CreateSettings(&Settings{OwnerEmail: "a@b.c", PasswordHash: "h"}); err != nil {
		t.Fatal(err)
	}

	imported := &Product{Title: "Из фида", SourcePrice: 10000, Price: 10000}
	manual := &Product{Title: "Правил руками", SourcePrice: 10000, Price: 55555,
		PriceManual: true}
	byHand := &Product{Title: "Заведён руками", Price: 700}
	for _, p := range []*Product{imported, manual, byHand} {
		if err := d.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
	}

	n, err := d.ApplyPriceCoefficient(0.5)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("recalculated rows: %d, want 1", n)
	}

	got, _ := d.GetProduct(imported.ID)
	if got.Price != 5000 {
		t.Errorf("imported price: %d", got.Price)
	}
	got, _ = d.GetProduct(manual.ID)
	if got.Price != 55555 {
		t.Errorf("manual price overwritten: %d", got.Price)
	}
	got, _ = d.GetProduct(byHand.ID)
	if got.Price != 700 {
		t.Errorf("product without a source touched: %d", got.Price)
	}

	// The second recalculation starts from the source, not the already reduced price.
	if _, err := d.ApplyPriceCoefficient(2); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetProduct(imported.ID)
	if got.Price != 20000 {
		t.Errorf("recalculation started from the current price: %d", got.Price)
	}

	if c, _ := d.PriceCoefficient(); c != 2 {
		t.Errorf("coefficient not saved: %v", c)
	}
	// A typo in the field must not multiply the catalog by a hundred.
	if _, err := d.ApplyPriceCoefficient(0); err == nil {
		t.Error("zero coefficient accepted")
	}
	if _, err := d.ApplyPriceCoefficient(5000); err == nil {
		t.Error("absurd coefficient accepted")
	}
}
