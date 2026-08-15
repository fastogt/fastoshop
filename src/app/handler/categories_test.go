package handler

import (
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// The draft exists because an empty text box is why category pages stay
// without text. It must name the real goods and the real prices — a paragraph
// of true sentences a person edits in a minute, not a template.
func TestCategoryDraft(t *testing.T) {
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	_ = d.CreateSettings(&database.Settings{OwnerEmail: "a@b.c", PasswordHash: "h", ShopName: "Лавка"})
	settings0, _ := d.GetSettings()
	settings0.Currency = database.ShopCurrencyBYN
	settings0.Lang = "ru"
	settings0.Terms = "Доставка\nКурьером по Минску за день. Почтой по стране 3–5 дней.\nОплата\nПри получении."
	if err := d.UpdateSettings(settings0); err != nil {
		t.Fatal(err)
	}
	for _, p := range []database.Product{
		{Title: "НЕСОРТ 96366 Тёрка пластмассовая 5 насадок, 12,5х30см", Price: 51520, Stock: 1, Category: "Кухня/Инвентарь"},
		{Title: "Ковш эмалированный 1,5 л, синий", Price: 120000, Stock: 1, Category: "Кухня/Посуда"},
		{Title: "Ковш эмалированный 2 л, белый", Price: 130000, Stock: 1, Category: "Кухня/Посуда"},
	} {
		if err := d.CreateProduct(&p); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{db: d}

	sample, err := d.SampleCategory("Кухня")
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := d.GetSettings()
	nodes, _ := d.VisibleCategories()
	draft := draftText("ru", "Кухня", sample, childNames(nodes, "Кухня"), settings)

	for _, want := range []string{
		"Кухня в магазине «Лавка», товаров: 3",
		"инвентарь, посуда",    // subcategories name the assortment
		"от 515.20 до 1300 Br", // the real range, in the shop's money
		"Курьером по Минску за день.",
	} {
		if !strings.Contains(draft, want) {
			t.Errorf("draft missing %q:\n%s", want, draft)
		}
	}
	// The supplier's bookkeeping has no business in a page about goods.
	for _, unwanted := range []string{"НЕСОРТ", "96366", "Доставка\n"} {
		if strings.Contains(draft, unwanted) {
			t.Errorf("draft carries %q:\n%s", unwanted, draft)
		}
	}

	// A leaf has no children, so the goods themselves name the assortment.
	sample, _ = d.SampleCategory("Кухня/Посуда")
	leaf := draftText("ru", "Кухня/Посуда", sample, nil, settings)
	if !strings.Contains(leaf, "ковш эмалированный") {
		t.Errorf("leaf draft does not name its goods:\n%s", leaf)
	}
	_ = h
}
