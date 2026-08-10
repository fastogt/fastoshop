package importer

import (
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// feed — источник из готового списка позиций, без сети.
type feed struct {
	name  string
	items []Item
}

func (f *feed) Name() string           { return f.name }
func (f *feed) Count() (int, error)    { return len(f.items), nil }
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

// Еженедельный фид: цена поставщика доезжает, остаток обновляется, а название и
// описание остаются такими, какими их сделал владелец.
func TestMergeUpdatesPriceAndStockOnly(t *testing.T) {
	d := mergeDB(t)
	src := &feed{name: "yml", items: []Item{
		{SKU: "A", Title: "Чайник", Description: "из фида", Price: 10000, Stock: 5},
	}}
	if _, err := Run(src, d, "Ромашка", 2, nil); err != nil {
		t.Fatal(err)
	}

	p, _ := d.GetProductBySlug("chajnik")
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
		t.Errorf("цена и остаток не обновились: %+v", got)
	}
	if got.Title != "Чайник эмалированный 2 л" || got.Description != "Текст владельца" {
		t.Errorf("фид затёр работу владельца: %+v", got)
	}
}

// Цену, выставленную руками, пересчёт фида не трогает.
func TestMergeKeepsManualPrice(t *testing.T) {
	d := mergeDB(t)
	src := &feed{name: "yml", items: []Item{{SKU: "A", Title: "Кружка", Price: 10000, Stock: 1}}}
	if _, err := Run(src, d, "Ромашка", 1, nil); err != nil {
		t.Fatal(err)
	}
	p, _ := d.GetProductBySlug("kruzhka")
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
		t.Errorf("ручная цена затёрта: %d", got.Price)
	}
	if got.SourcePrice != 20000 {
		t.Errorf("цена источника не обновилась: %d", got.SourcePrice)
	}
}

// Исчезнувший из фида товар снимается с продажи остатком, но остаётся собой:
// слаг проиндексирован, а на product_id ссылается связь с Ozon.
func TestMergeZeroesMissingWithoutDeleting(t *testing.T) {
	d := mergeDB(t)
	src := &feed{name: "yml", items: []Item{
		{SKU: "A", Title: "Чайник", Price: 10000, Stock: 5},
		{SKU: "B", Title: "Кружка", Price: 20000, Stock: 7},
	}}
	if _, err := Run(src, d, "Ромашка", 1, nil); err != nil {
		t.Fatal(err)
	}
	gone, _ := d.GetProductBySlug("kruzhka")
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
	still, err := d.GetProductBySlug("kruzhka")
	if err != nil {
		t.Fatalf("товар удалён вместе со слагом: %v", err)
	}
	if still.Stock != 0 {
		t.Errorf("остаток не обнулён: %d", still.Stock)
	}
	links, _ := d.ListOzonLinks()
	if len(links) != 1 || links[0].ProductID != gone.ID {
		t.Errorf("связь с Ozon потеряна: %+v", links)
	}
}

// Пустой ответ источника — это его сбой, а не снятие всего каталога с продажи.
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
		t.Fatalf("пустой фид обнулил каталог: %+v", res)
	}
	p, _ := d.GetProductBySlug("chajnik")
	if p.Stock != 5 {
		t.Errorf("остаток: %d", p.Stock)
	}
}

// Тот же фид второй раз — ничего не создаётся и не обновляется.
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
