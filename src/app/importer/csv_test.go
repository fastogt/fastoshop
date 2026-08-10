package importer

import (
	"strings"
	"testing"
)

// cp1251 кодирует строку так, как это делает русский Excel при «Сохранить как
// CSV»: именно на этом ломаются реальные загрузки.
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
		t.Errorf("кодировка не распознана: %+v", it)
	}
	if it.Price != 250050 {
		t.Errorf("цена с пробелом и запятой: %d", it.Price)
	}
	if it.Stock != 7 || len(it.ImageURLs) != 2 {
		t.Errorf("%+v", it)
	}
}

// UTF-8 с BOM и запятыми — то, что отдаёт наш же шаблон и любой другой редактор.
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
		t.Fatalf("ошибки на своём же шаблоне: %v", c.Problems())
	}
}

// Колонки читаются по имени: переставленный в Excel столбец не должен молча
// записать цену в остаток.
func TestCSVColumnsByName(t *testing.T) {
	c := &CSV{Data: []byte("title,price,sku\nЧайник,100.00,A-1\n")}
	items, _ := c.Fetch()
	if len(items) != 1 || items[0].SKU != "A-1" || items[0].Price != 10000 {
		t.Fatalf("%+v", items)
	}
}

// Битая строка называет свой номер и не роняет остальные.
func TestCSVBadRowIsReportedWithLineNumber(t *testing.T) {
	c := &CSV{Data: []byte("sku,title,price\nA,Первый,100\nB,Второй,дорого\nC,Третий,300\n")}
	items, _ := c.Fetch()
	if len(items) != 2 {
		t.Fatalf("битая строка утащила соседей: %+v", items)
	}
	if c.FetchErrors() != 1 {
		t.Fatalf("%v", c.Problems())
	}
	if !strings.Contains(c.Problems()[0], "row 3") {
		t.Errorf("номер строки не назван: %q", c.Problems()[0])
	}
}

// Файл без обязательной колонки — это не «ноль товаров», а ошибка.
func TestCSVMissingColumnIsAnError(t *testing.T) {
	c := &CSV{Data: []byte("title,stock\nЧайник,5\n")}
	items, _ := c.Fetch()
	if len(items) != 0 || c.FetchErrors() == 0 {
		t.Fatalf("%+v %v", items, c.Problems())
	}
}
