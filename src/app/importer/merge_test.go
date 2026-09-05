package importer

import (
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// feed is a source backed by a ready-made list of items, no network.
type feed struct {
	name  string
	items []Item
}

func (f *feed) Name() string           { return f.name }
func (f *feed) Fetch() ([]Item, error) { return f.items, nil }

func mergeDB(t *testing.T) *database.Database {
	t.Helper()
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// Price and stock come through; title and description stay the owner's.
func TestMergeUpdatesPriceAndStockOnly(t *testing.T) {
	d := mergeDB(t)
	src := &feed{name: "yml", items: []Item{
		{SKU: "A", Title: "Чайник", Description: "из фида", Price: 10000, Stock: 5},
	}}
	if _, err := Run(src, d, "Ромашка", 2, nil); err != nil {
		t.Fatal(err)
	}

	p, _ := d.GetVisibleProductBySlug("chajnik")
	p.Title = "Чайник эмалированный 2 л"
	p.Description = "Текст владельца"
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}

	src.items = []Item{
		{SKU: "A", Title: "Чайник", Description: "из фида", Price: 12000, Stock: 3},
	}
	res, err := Run(src, d, "Ромашка", 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Updated != 1 || res.Imported != 0 {
		t.Fatalf("%+v", res)
	}
	got, _ := d.GetProduct(p.ID)
	if got.SourcePrice != 12000 || got.Price != 24000 || got.Stock != 3 {
		t.Errorf("price and stock not updated: %+v", got)
	}
	if got.Title != "Чайник эмалированный 2 л" || got.Description != "Текст владельца" {
		t.Errorf("feed overwrote the owner's work: %+v", got)
	}
}

// A manually set price is left alone by the feed recalculation.
func TestMergeKeepsManualPrice(t *testing.T) {
	d := mergeDB(t)
	src := &feed{name: "yml", items: []Item{{SKU: "A", Title: "Кружка", Price: 10000, Stock: 1}}}
	if _, err := Run(src, d, "Ромашка", 1, nil); err != nil {
		t.Fatal(err)
	}
	p, _ := d.GetVisibleProductBySlug("kruzhka")
	p.Price = 77777
	p.PriceManual = true
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}

	src.items = []Item{{SKU: "A", Title: "Кружка", Price: 20000, Stock: 1}}
	if _, err := Run(src, d, "Ромашка", 1, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetProduct(p.ID)
	if got.Price != 77777 {
		t.Errorf("manual price overwritten: %d", got.Price)
	}
	if got.SourcePrice != 20000 {
		t.Errorf("source price not updated: %d", got.SourcePrice)
	}
}

// A product gone from the feed is zeroed, not deleted: slug and links point at it.
func TestMergeZeroesMissingWithoutDeleting(t *testing.T) {
	d := mergeDB(t)
	src := &feed{name: "yml", items: []Item{
		{SKU: "A", Title: "Чайник", Price: 10000, Stock: 5},
		{SKU: "B", Title: "Кружка", Price: 20000, Stock: 7},
	}}
	if _, err := Run(src, d, "Ромашка", 1, nil); err != nil {
		t.Fatal(err)
	}
	gone, _ := d.GetVisibleProductBySlug("kruzhka")
	if err := d.UpsertOzonLink(&database.OzonLink{ProductID: gone.ID, OfferID: "B"}); err != nil {
		t.Fatal(err)
	}

	src.items = src.items[:1]
	res, err := Run(src, d, "Ромашка", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Zeroed != 1 {
		t.Fatalf("%+v", res)
	}
	still, err := d.GetVisibleProductBySlug("kruzhka")
	if err != nil {
		t.Fatalf("product deleted along with its slug: %v", err)
	}
	if still.Stock != 0 {
		t.Errorf("stock not zeroed: %d", still.Stock)
	}
	links, _ := d.ListOzonLinksPage(1000, 0)
	if len(links) != 1 || links[0].ProductID != gone.ID {
		t.Errorf("Ozon link lost: %+v", links)
	}
}

// An empty source response is its failure, not a delisting of the whole catalogue.
func TestMergeEmptyFeedDoesNotWipeShop(t *testing.T) {
	d := mergeDB(t)
	src := &feed{name: "yml", items: []Item{{SKU: "A", Title: "Чайник", Price: 10000, Stock: 5}}}
	if _, err := Run(src, d, "Ромашка", 1, nil); err != nil {
		t.Fatal(err)
	}
	src.items = nil
	res, err := Run(src, d, "Ромашка", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Zeroed != 0 {
		t.Fatalf("empty feed wiped the catalogue: %+v", res)
	}
	p, _ := d.GetVisibleProductBySlug("chajnik")
	if p.Stock != 5 {
		t.Errorf("stock: %d", p.Stock)
	}
}

// The same feed a second time - nothing gets created or updated.
func TestMergeSameFeedIsNoop(t *testing.T) {
	d := mergeDB(t)
	src := &feed{name: "yml", items: []Item{{SKU: "A", Title: "Чайник", Price: 10000, Stock: 5}}}
	if _, err := Run(src, d, "Ромашка", 1, nil); err != nil {
		t.Fatal(err)
	}
	res, err := Run(src, d, "Ромашка", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 0 || res.Updated != 0 || res.Skipped != 1 || res.Zeroed != 0 {
		t.Fatalf("%+v", res)
	}
}
