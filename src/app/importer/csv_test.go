package importer

import (
	"testing"
)

// cp1251 encodes a string the way Russian Excel does on "Save as CSV":
// that is exactly what breaks real uploads.
func cp1251(s string) []byte {
	back := map[rune]byte{}
	for i, r := range kCP1251 {
		back[r] = byte(0x80 + i)
	}
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r < 0x80 {
			out = append(out, byte(r))
			continue
		}
		out = append(out, back[r])
	}
	return out
}

func TestCSVReadsRussianExcelFile(t *testing.T) {
	text := "sku;title;description;price;stock;category;images\n" +
		"CH-201;Чайник эмалированный;Объём 2 л;2 500,50;7;kuhnya;https://e.com/1.jpg|https://e.com/2.jpg\n"
	c := &CSV{Data: cp1251(text)}

	items, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("%+v", items)
	}
	it := items[0]
	if it.Title != "Чайник эмалированный" || it.Description != "Объём 2 л" {
		t.Errorf("encoding not detected: %+v", it)
	}
	if it.Price != 250050 {
		t.Errorf("price with space and comma: %d", it.Price)
	}
	if it.Stock != 7 || len(it.ImageURLs) != 2 {
		t.Errorf("%+v", it)
	}
}

// UTF-8 with a BOM and commas is what our own template and any other editor produce.
func TestCSVReadsUTF8Template(t *testing.T) {
	c := &CSV{Data: Template()}
	items, err := c.Fetch()
	if err != nil || len(items) != 1 {
		t.Fatalf("%v %+v", err, items)
	}
	if items[0].SKU != "CH-201" || items[0].Price != 250000 {
		t.Fatalf("%+v", items[0])
	}
	if c.FetchErrors() != 0 {
		t.Fatalf("errors on our own template: %d errors", c.FetchErrors())
	}
}

// Columns are read by name: a column rearranged in Excel must not silently
// write the price into the stock.
func TestCSVColumnsByName(t *testing.T) {
	c := &CSV{Data: []byte("title,price,sku\nЧайник,100.00,A-1\n")}
	items, _ := c.Fetch()
	if len(items) != 1 || items[0].SKU != "A-1" || items[0].Price != 10000 {
		t.Fatalf("%+v", items)
	}
}

// A broken row counts as an error and does not take down the rest.
func TestCSVBadRowIsCountedAndSkipped(t *testing.T) {
	c := &CSV{Data: []byte("sku,title,price\nA,Первый,100\nB,Второй,дорого\nC,Третий,300\n")}
	items, _ := c.Fetch()
	if len(items) != 2 {
		t.Fatalf("broken row dragged neighbours down: %+v", items)
	}
	if c.FetchErrors() != 1 {
		t.Fatalf("%d errors", c.FetchErrors())
	}
}

// A file missing a required column is an error, not "zero products".
func TestCSVMissingColumnIsAnError(t *testing.T) {
	c := &CSV{Data: []byte("title,stock\nЧайник,5\n")}
	items, _ := c.Fetch()
	if len(items) != 0 || c.FetchErrors() == 0 {
		t.Fatalf("%+v %d errors", items, c.FetchErrors())
	}
}

// A price list writes nesting inside one cell, and that is data, not a guess.
// A slash between digits is a size, not a level.
func TestCSVCategoryCell(t *testing.T) {
	cases := map[string]string{
		"Текстиль > Спальня": "Текстиль/Спальня",
		"Текстиль/Спальня":   "Текстиль/Спальня",
		"Текстиль | Спальня": "Текстиль/Спальня",
		"КПБ 1,5/2 сп":       "КПБ 1,5-2 сп",
		"Хвойные растения":   "Хвойные растения",
		"":                   "",
	}
	for in, want := range cases {
		if got := cellCategory(in); got != want {
			t.Errorf("cellCategory(%q) = %q, want %q", in, got, want)
		}
	}
}

// The column has been in the template since day one and was quietly dropped.
func TestCSVReadsCategory(t *testing.T) {
	c := &CSV{Data: []byte("sku;title;price;category\nA-1;Чайник;100;Посуда > Чайники\n")}
	items, err := c.Fetch()
	if err != nil || len(items) != 1 {
		t.Fatalf("fetch: %v %+v", err, items)
	}
	if items[0].Category != "Посуда/Чайники" {
		t.Errorf("category = %q", items[0].Category)
	}
}

// TestCSVParams: a column the shop does not know by name is a characteristic.
// In a spreadsheet a property is a column - that is what the format already
// offers, and packing pairs into one cell would invent a second one beside it.
func TestCSVParams(t *testing.T) {
	c := &CSV{Data: []byte(
		"sku;title;price;Цвет;Материал;;Объём\n" +
			"A;Чайник;2500.00;белый;эмаль;мусор;2 л\n" +
			"B;Ковш;1000.00;;;;\n" +
			"C;Кружка;900.00;синий;;;\n")}
	items, err := c.Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("строк %d, ожидалось 3", len(items))
	}
	if got := items[0].Params; len(got) != 3 || param(got, "Цвет") != "белый" ||
		param(got, "Материал") != "эмаль" || param(got, "Объём") != "2 л" {
		t.Errorf("колонки не стали характеристиками: %+v", got)
	}
	if items[1].Params != nil {
		t.Errorf("пустые ячейки должны давать ничего, а не пустой набор: %+v", items[1].Params)
	}
	// A column with no heading is not a characteristic, and an empty cell adds
	// nothing: both are ordinary in a spreadsheet and neither may reach a card.
	if got := items[2].Params; len(got) != 1 || param(got, "Цвет") != "синий" {
		t.Errorf("пустые и безымянные не отсеялись: %+v", got)
	}
}

// TestPriceWithNote: a price list annotates a price in the same cell - a
// discount in brackets, a footnote star, a currency. The number is the price;
// a cell holding a second number is two prices and stays refused.
func TestPriceWithNote(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int64
		ok   bool
	}{
		{"4,16", 416, true},
		{"4,16(-9,2%)", 416, true},
		{"5,40*", 540, true},
		{"1 234,50", 123450, true},
		{"12.30 руб", 1230, true},
		{"4,16 / 5,20", 0, false},
		{"", 0, false},
		{"по запросу", 0, false},
	} {
		got, err := parseMoney(c.in)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("%q: получили %d, %v; ожидали %d", c.in, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Errorf("%q: разобралось как %d, а не должно", c.in, got)
		}
	}
}
