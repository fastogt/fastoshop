package importer

import (
	"fmt"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

func shopProduct(sku string, sourcePrice int64) database.Product {
	return database.Product{SKU: sku, Title: "Товар " + sku, SourcePrice: sourcePrice,
		Supplier: "П"}
}

// On an empty shop the whole feed is new arrivals, and nothing has gone.
func TestDiffFirstImport(t *testing.T) {
	items := []Item{{SKU: "A", Title: "Чайник", Price: 10000}}
	d := Compare(items, nil, "П", 0.5, nil)
	if d.New != 1 || d.Gone != 0 || d.Unchanged != 0 {
		t.Fatalf("%+v", d)
	}
	if len(d.NewItems) != 1 || d.NewItems[0].Shelf != 5000 {
		t.Fatalf("storefront price leaked into a new item: %+v", d.NewItems)
	}
}

// Re-uploading the same feed must not look like a change.
func TestDiffSameFeedIsQuiet(t *testing.T) {
	items := []Item{{SKU: "A", Price: 10000}, {SKU: "B", Price: 20000}}
	existing := []database.Product{shopProduct("A", 10000), shopProduct("B", 20000)}
	d := Compare(items, existing, "П", 1, nil)
	if d.Unchanged != 2 || d.New != 0 || d.Gone != 0 || d.PriceUp != 0 || d.PriceDown != 0 {
		t.Fatalf("%+v", d)
	}
	if len(d.PriceChanges) != 0 {
		t.Fatalf("changes out of nowhere: %+v", d.PriceChanges)
	}
}

func TestDiffClassifiesChanges(t *testing.T) {
	items := []Item{
		{SKU: "UP", Price: 12000},   // +20%
		{SKU: "DOWN", Price: 5000},  // −50%
		{SKU: "SAME", Price: 10000}, // unchanged
		{SKU: "NEW", Price: 30000},
	}
	existing := []database.Product{
		shopProduct("UP", 10000), shopProduct("DOWN", 10000),
		shopProduct("SAME", 10000), shopProduct("GONE", 10000),
	}
	d := Compare(items, existing, "П", 1, nil)
	if d.New != 1 || d.Gone != 1 || d.PriceUp != 1 || d.PriceDown != 1 || d.Unchanged != 1 {
		t.Fatalf("%+v", d)
	}
	if d.GoneItems[0].SKU != "GONE" {
		t.Fatalf("gone item: %+v", d.GoneItems)
	}
	// The biggest change comes first, regardless of sign.
	if d.PriceChanges[0].SKU != "DOWN" || d.PriceChanges[0].Percent != -50 {
		t.Fatalf("change order: %+v", d.PriceChanges)
	}
	if d.PriceChanges[1].SKU != "UP" || d.PriceChanges[1].Percent != 20 {
		t.Fatalf("change order: %+v", d.PriceChanges)
	}
}

// A zero source price must not take first place with infinite growth.
func TestDiffZeroSourcePriceHasNoPercent(t *testing.T) {
	items := []Item{{SKU: "OLD", Price: 50000}, {SKU: "REAL", Price: 11000}}
	existing := []database.Product{shopProduct("OLD", 0), shopProduct("REAL", 10000)}
	d := Compare(items, existing, "П", 1, nil)
	if d.PriceChanges[0].SKU != "REAL" {
		t.Fatalf("zero base pushed out a real change: %+v", d.PriceChanges)
	}
	if d.PriceChanges[1].Percent != 0 {
		t.Fatalf("percentage over a zero base: %+v", d.PriceChanges[1])
	}
}

// The counter must be complete even when the list is truncated.
func TestDiffCountsSurviveTruncation(t *testing.T) {
	var items []Item
	var existing []database.Product
	for i := range 120 {
		sku := fmt.Sprintf("S%03d", i)
		items = append(items, Item{SKU: sku, Price: int64(20000 + i)})
		existing = append(existing, shopProduct(sku, 10000))
	}
	d := Compare(items, existing, "П", 1, nil)
	if d.PriceUp != 120 {
		t.Fatalf("price-up counter: %d", d.PriceUp)
	}
	if len(d.PriceChanges) != kListLimit {
		t.Fatalf("list not truncated: %d", len(d.PriceChanges))
	}
}
