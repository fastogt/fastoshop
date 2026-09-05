package importer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// One fixture per source, compared whole: a field silently dropped fails a test.

func i64(v int64) *int64 { return &v }

func TestYMLParsesEverythingItIsGiven(t *testing.T) {
	srv := ymlServer(t, `<?xml version="1.0" encoding="utf-8"?>
<yml_catalog><shop><currencies><currency id="RUB" rate="1"/></currencies>
<categories>
  <category id="1">Посуда</category>
  <category id="2" parentId="1">Чайники</category>
</categories>
<offers>
<offer id="1001" available="true">
  <vendorCode>TK-1</vendorCode>
  <name>Чайник эмалированный</name>
  <description>Два литра, со свистком</description>
  <price>2500.50</price><currencyId>RUB</currencyId>
  <categoryId>2</categoryId>
  <picture>IMG</picture><picture>IMG</picture>
  <count>7</count>
  <weight>0.75</weight>
  <dimensions>20.1/30.5/11</dimensions>
  <param name="Цвет">белый</param>
  <param name="Объём" unit="л">2</param>
</offer>
</offers></shop></yml_catalog>`)
	defer srv.Close()

	items, err := (&YML{URL: srv.URL + "/feed.xml", DefaultStock: 3}).Fetch()
	if err != nil {
		t.Fatal(err)
	}
	img := srv.URL + "/img.jpg"
	want := []Item{{
		SKU:         "TK-1",
		Title:       "Чайник эмалированный",
		Description: "Два литра, со свистком",
		Price:       250050,
		Stock:       7,
		ImageURLs:   []string{img, img},
		// One category deep: the shared root "Посуда" is trimmed.
		Category: "Чайники",
		WeightG:  i64(750),
		LengthMM: i64(201),
		WidthMM:  i64(305),
		HeightMM: i64(110),
		Params: []database.Param{
			{Name: "Цвет", Value: "белый"},
			{Name: "Объём, л", Value: 2.0},
		},
	}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("YML lost or changed a field:\ngot  %+v\nwant %+v", items, want)
	}
}

func TestCSVParsesEverythingItIsGiven(t *testing.T) {
	items, err := (&CSV{Data: []byte(
		"sku;title;description;price;stock;category;images;Цвет;Материал\n" +
			"TK-1;Чайник;Со свистком;2500.50;7;Посуда > Чайники;" +
			"http://a/1.jpg|http://a/2.jpg;белый;эмаль\n")}).Fetch()
	if err != nil {
		t.Fatal(err)
	}
	// No weight and no dimensions: a spreadsheet has no agreed column for them.
	want := []Item{{
		SKU:         "TK-1",
		Title:       "Чайник",
		Description: "Со свистком",
		Price:       250050,
		Stock:       7,
		Category:    "Посуда/Чайники",
		ImageURLs:   []string{"http://a/1.jpg", "http://a/2.jpg"},
		Params: []database.Param{
			{Name: "Цвет", Value: "белый"},
			{Name: "Материал", Value: "эмаль"},
		},
	}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("CSV lost or changed a field:\ngot  %+v\nwant %+v", items, want)
	}
}

func TestXLSXParsesEverythingItIsGiven(t *testing.T) {
	strings := `<?xml version="1.0"?><sst>
<si><t>Прайс ООО «Поставщик»</t></si>
<si><t>Артикул</t></si><si><t>Наименование</t></si><si><t>Описание</t></si>
<si><t>Цена</t></si><si><t>Остаток</t></si><si><t>Группа</t></si>
<si><t>Фото</t></si><si><t>Цвет</t></si><si><t>Материал</t></si>
<si><t>TK-1</t></si><si><t>Чайник</t></si><si><t>Со свистком</t></si>
<si><t>Посуда &gt; Чайники</t></si><si><t>http://a/1.jpg</t></si>
<si><t>белый</t></si><si><t>эмаль</t></si>
</sst>`
	sheet := `<?xml version="1.0"?><worksheet><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c></row>
<row r="2"><c r="A2" t="s"><v>1</v></c><c r="B2" t="s"><v>2</v></c><c r="C2" t="s"><v>3</v></c><c r="D2" t="s"><v>4</v></c><c r="E2" t="s"><v>5</v></c><c r="F2" t="s"><v>6</v></c><c r="G2" t="s"><v>7</v></c><c r="H2" t="s"><v>8</v></c><c r="I2" t="s"><v>9</v></c></row>
<row r="3"><c r="A3" t="s"><v>10</v></c><c r="B3" t="s"><v>11</v></c><c r="C3" t="s"><v>12</v></c><c r="D3"><v>2500.5</v></c><c r="E3"><v>7</v></c><c r="F3" t="s"><v>13</v></c><c r="G3" t="s"><v>14</v></c><c r="H3" t="s"><v>15</v></c><c r="I3" t="s"><v>16</v></c></row>
</sheetData></worksheet>`
	data := build(t, map[string]string{
		"xl/sharedStrings.xml":     strings,
		"xl/worksheets/sheet1.xml": sheet,
	}, nil)

	items, err := (&XLSX{Data: data}).Fetch()
	if err != nil {
		t.Fatal(err)
	}
	want := []Item{{
		SKU:         "TK-1",
		Title:       "Чайник",
		Description: "Со свистком",
		Price:       250050,
		Stock:       7,
		Category:    "Посуда/Чайники",
		ImageURLs:   []string{"http://a/1.jpg"},
		Params: []database.Param{
			{Name: "Цвет", Value: "белый"},
			{Name: "Материал", Value: "эмаль"},
		},
	}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("XLSX lost or changed a field:\ngot  %+v\nwant %+v", items, want)
	}
}

func TestWBParsesEverythingItIsGiven(t *testing.T) {
	mux := wbMux(`{"cards":[{"nmID":22,"vendorCode":"TK-1","title":"Чайник",
		"description":"Со свистком","subjectID":5,"subjectName":"Чайники",
		"photos":[{"big":"http://a/1.jpg"},{"big":"http://a/2.jpg"}],
		"dimensions":{"length":30.5,"width":20.1,"height":11,"weightBrutto":0.75},
		"characteristics":[
			{"name":"Цвет","value":["белый","синий"]},
			{"name":"Объём","value":2},
			{"name":"Со свистком","value":true},
			{"name":"Материал","value":"эмаль"}],
		"sizes":[{"chrtID":101,"techSize":"0","skus":["2000000000011"]}]}],
		"cursor":{"total":1}}`)
	mux.HandleFunc("/api/v2/list/goods/filter", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"listGoods":[{"nmID":22,
			"sizes":[{"sizeID":101,"discountedPrice":2500.5}]}]}}`))
	})
	mux.HandleFunc("/api/v3/warehouses", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":7,"name":"Склад","officeId":15}]`))
	})
	mux.HandleFunc("/api/v3/stocks/7", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"stocks":[{"sku":"2000000000011","amount":7}]}`))
	})
	mux.HandleFunc("/content/v2/object/parent/all", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, err := (&WB{Token: "tok", ContentURL: srv.URL,
		PricesURL: srv.URL, MarketplaceURL: srv.URL}).Fetch()
	if err != nil {
		t.Fatal(err)
	}
	// Centimetres and kilograms in, millimetres and grams out.
	want := []Item{{
		SKU:         "TK-1",
		Title:       "Чайник",
		Description: "Со свистком",
		Price:       250050,
		Stock:       7,
		ImageURLs:   []string{"http://a/1.jpg", "http://a/2.jpg"},
		Category:    "Чайники",
		WeightG:     i64(750),
		LengthMM:    i64(305),
		WidthMM:     i64(201),
		HeightMM:    i64(110),
		Params: []database.Param{
			{Name: "Цвет", Value: []any{"белый", "синий"}},
			{Name: "Объём", Value: 2.0},
			{Name: "Со свистком", Value: true},
			{Name: "Материал", Value: "эмаль"},
		},
	}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("WB lost or changed a field:\ngot  %+v\nwant %+v", items, want)
	}
}

func TestOzonParsesEverythingItIsGiven(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/product/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"items":[{"product_id":11,"offer_id":"TK-1"}],"total":1}}`))
	})
	mux.HandleFunc("/v3/product/info/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":11,"offer_id":"TK-1","name":"Чайник",
			"price":"2500.50","images":["http://a/1.jpg","http://a/2.jpg"],
			"description_category_id":17,"type_id":91,
			"weight":750,"weight_unit":"g",
			"depth":305,"width":201,"height":110,"dimension_unit":"mm"}]}`))
	})
	mux.HandleFunc("/v4/product/info/stocks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"product_id":11,
			"stocks":[{"type":"fbs","present":9,"reserved":2}]}]}`))
	})
	mux.HandleFunc("/v1/product/info/description", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"description":"Со свистком"}}`))
	})
	mux.HandleFunc("/v1/description-category/tree", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":[{"description_category_id":17,"category_name":"Посуда",
			"children":[{"type_id":91,"type_name":"Чайники"}]}]}`))
	})
	mux.HandleFunc("/v4/product/info/attributes", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":[{"id":11,"attributes":[
			{"id":85,"values":[{"dictionary_value_id":1,"value":"Гжель"}]},
			{"id":8229,"values":[{"dictionary_value_id":9,"value":"эмаль"}]},
			{"id":11254,"values":[{"value":"{\"content\":[],\"version\":0.3}"}]},
			{"id":10096,"values":[
				{"dictionary_value_id":2,"value":"белый"},
				{"dictionary_value_id":3,"value":"синий"}]},
			{"id":4383,"values":[{"value":"2"}]},
			{"id":9999,"values":[{"value":"со свистком"}]},
			{"id":7777,"values":[{"value":"true"}]}]}]}`))
	})
	mux.HandleFunc("/v1/description-category/attribute", func(w http.ResponseWriter, r *http.Request) {
		var req ozonAttributeDictRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		// The dictionary is asked for by category and type together.
		if req.CategoryID != 17 || req.TypeID != 91 {
			t.Errorf("attribute dictionary asked for %d/%d", req.CategoryID, req.TypeID)
		}
		_, _ = w.Write([]byte(`{"result":[
			{"id":85,"name":"Бренд","type":"String"},
			{"id":8229,"name":"Материал","type":"String"},
			{"id":11254,"name":"Rich-контент JSON","type":"String"},
			{"id":10096,"name":"Цвет","type":"String","is_collection":true},
			{"id":4383,"name":"Объём, л","type":"Decimal"},
			{"id":7777,"name":"Со свистком","type":"Boolean"}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, err := (&Ozon{ClientID: "cid", APIKey: "key", BaseURL: srv.URL}).Fetch()
	if err != nil {
		t.Fatal(err)
	}
	// Typed as Ozon declared; 85 is the brand and 11254 rich content, neither a param.
	want := []Item{{
		SKU:         "TK-1",
		Title:       "Чайник",
		Description: "Со свистком",
		Price:       250050,
		Stock:       7,
		ImageURLs:   []string{"http://a/1.jpg", "http://a/2.jpg"},
		Category:    "Посуда/Чайники",
		Brand:       "Гжель",
		WeightG:     i64(750),
		LengthMM:    i64(305),
		WidthMM:     i64(201),
		HeightMM:    i64(110),
		Params: []database.Param{
			{Name: "Материал", Value: "эмаль"},
			{Name: "Цвет", Value: []any{"белый", "синий"}},
			{Name: "Объём, л", Value: 2.0},
			{Name: "attribute 9999", Value: "со свистком"},
			{Name: "Со свистком", Value: true},
		},
	}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("Ozon lost or changed a field:\ngot  %+v\nwant %+v", items, want)
	}
}
