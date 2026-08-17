package importer

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

func TestOzonImport(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/product/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Client-Id") != "cid" || r.Header.Get("Api-Key") != "key" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"result":{"items":[{"product_id":11,"offer_id":"SKU-1"}],"total":1}}`))
	})
	mux.HandleFunc("/v3/product/info/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":11,"offer_id":"SKU-1","name":"Чайник",
			"price":"2500.00","marketing_price":"","barcodes":["4600000000001"],
			"images":["` + imgURL(r) + `"]}]}`))
	})
	mux.HandleFunc("/v4/product/info/stocks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"product_id":11,"offer_id":"SKU-1","stocks":[
			{"type":"fbo","present":100,"reserved":0},
			{"type":"fbs","present":9,"reserved":2}]}],"total":1}`))
	})
	mux.HandleFunc("/v1/product/info/description", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"description":"Хороший чайник"}}`))
	})
	mux.HandleFunc("/img.jpg", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("\xff\xd8\xffjpegdata"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	d, _ := database.OpenInMemory()
	defer func() { _ = d.Close() }()

	imp := &Ozon{ClientID: "cid", APIKey: "key", BaseURL: srv.URL}
	n, err := imp.Count()
	if err != nil || n != 1 {
		t.Fatalf("count: %v %d", err, n)
	}
	res, err := Run(imp, d, "Ромашка", 1, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 1 {
		t.Fatalf("res: %+v", res)
	}
	p, err := d.GetVisibleProductBySlug("chajnik")
	if err != nil || p.SKU != "SKU-1" || p.Price != 250000 || p.Description != "Хороший чайник" {
		t.Fatalf("%v %+v", err, p)
	}
	// FBS stock net of the reserve; FBO is not our warehouse and doesn't count.
	if p.Stock != 7 {
		t.Fatalf("stock: %d", p.Stock)
	}
	imgs, _ := d.ListImages(p.ID)
	if len(imgs) != 1 {
		t.Fatalf("images: %+v", imgs)
	}
	// A repeat import — dedup by SKU.
	res, _ = Run(imp, d, "Ромашка", 1, "", nil)
	if res.Imported != 0 || res.Skipped != 1 {
		t.Fatalf("dedup: %+v", res)
	}
}

// imgURL returns an absolute URL of a mock image on the same server.
func imgURL(r *http.Request) string { return "http://" + r.Host + "/img.jpg" }

// wbMux mocks all three WB hosts at once: in the test they point at a single httptest.
func wbMux(cards string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/content/v2/get/cards/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "tok" {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(cards))
	})
	mux.HandleFunc("/img.jpg", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("\xff\xd8\xffjpegdata"))
	})
	return mux
}

func TestWBImportSizes(t *testing.T) {
	mux := wbMux(`{"cards":[{"nmID":22,"vendorCode":"WB-1","title":"Футболка",
		"description":"Синяя","photos":[{"big":"http://127.0.0.1/img.jpg"}],
		"sizes":[
			{"chrtID":101,"techSize":"S","wbSize":"42","skus":["2000000000011"]},
			{"chrtID":102,"techSize":"M","wbSize":"44","skus":["2000000000022"]},
			{"chrtID":103,"techSize":"L","wbSize":"46","skus":["2000000000033"]}]}],
		"cursor":{"total":1}}`)
	mux.HandleFunc("/api/v2/list/goods/filter", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"listGoods":[{"nmID":22,"sizes":[
			{"sizeID":101,"discountedPrice":990.5},
			{"sizeID":102,"discountedPrice":990.5},
			{"sizeID":103,"discountedPrice":1200}]}]}}`))
	})
	mux.HandleFunc("/api/v3/warehouses", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":7,"name":"Склад","officeId":15}]`))
	})
	mux.HandleFunc("/api/v3/stocks/7", func(w http.ResponseWriter, r *http.Request) {
		var req wbStocksRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		// The method takes barcodes, not chrtID: if we query with the wrong key,
		// live WB silently returns an empty list and the whole catalogue arrives at zero.
		if len(req.Skus) != 3 || req.Skus[0] != "2000000000011" {
			t.Errorf("stocks request: %+v", req)
		}
		_, _ = w.Write([]byte(`{"stocks":[{"sku":"2000000000011","amount":3},
			{"sku":"2000000000022","amount":0},{"sku":"2000000000033","amount":5}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	d, _ := database.OpenInMemory()
	defer func() { _ = d.Close() }()

	imp := &WB{Token: "tok", ContentURL: srv.URL, PricesURL: srv.URL, MarketplaceURL: srv.URL}
	res, err := Run(imp, d, "Ромашка", 1, "", nil)
	if err != nil || res.Imported != 3 {
		t.Fatalf("%v %+v", err, res)
	}

	want := []struct {
		slug  string
		sku   string
		stock int
		price int64
	}{
		{"futbolka-s", "WB-1-S", 3, 99050},
		{"futbolka-m", "WB-1-M", 0, 99050},
		{"futbolka-l", "WB-1-L", 5, 120000},
	}
	for _, tc := range want {
		p, err := d.GetVisibleProductBySlug(tc.slug)
		if err != nil {
			t.Fatalf("%s: %v", tc.slug, err)
		}
		if p.SKU != tc.sku || p.Stock != tc.stock || p.Price != tc.price {
			t.Fatalf("%s: %+v", tc.slug, p)
		}
	}

	// A product with imported stock can actually be bought, a zero one cannot.
	live, _ := d.GetVisibleProductBySlug("futbolka-l")
	items := []database.OrderItem{{ProductID: live.ID, Qty: 5}}
	if err := d.CreateOrderWithStock(&database.Order{Name: "Иван", Phone: "+7"}, items); err != nil {
		t.Fatalf("order: %v", err)
	}
	after, _ := d.GetVisibleProductBySlug("futbolka-l")
	if after.Stock != 0 {
		t.Fatalf("stock after order: %d", after.Stock)
	}
	dead, _ := d.GetVisibleProductBySlug("futbolka-m")
	err = d.CreateOrderWithStock(&database.Order{Name: "Иван", Phone: "+7"},
		[]database.OrderItem{{ProductID: dead.ID, Qty: 1}})
	if _, ok := err.(*database.OutOfStockError); !ok {
		t.Fatalf("expected out of stock, got %v", err)
	}
}

func TestWBImportSingleSize(t *testing.T) {
	mux := wbMux(`{"cards":[{"nmID":22,"vendorCode":"WB-1","title":"Кружка",
		"description":"Синяя","photos":[],
		"sizes":[{"chrtID":101,"techSize":"","wbSize":"","skus":["2000000000011"]}]}],
		"cursor":{"total":1}}`)
	mux.HandleFunc("/api/v2/list/goods/filter", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"listGoods":[{"nmID":22,"sizes":[{"sizeID":101,"discountedPrice":990.5}]}]}}`))
	})
	mux.HandleFunc("/api/v3/warehouses", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":7,"name":"Склад","officeId":15}]`))
	})
	mux.HandleFunc("/api/v3/stocks/7", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"stocks":[{"sku":"2000000000011","amount":4}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	d, _ := database.OpenInMemory()
	defer func() { _ = d.Close() }()

	imp := &WB{Token: "tok", ContentURL: srv.URL, PricesURL: srv.URL, MarketplaceURL: srv.URL}
	res, err := Run(imp, d, "Ромашка", 1, "", nil)
	if err != nil || res.Imported != 1 {
		t.Fatalf("%v %+v", err, res)
	}
	// A single size — title and SKU without a suffix.
	p, err := d.GetVisibleProductBySlug("kruzhka")
	if err != nil || p.SKU != "WB-1" || p.Title != "Кружка" || p.Price != 99050 || p.Stock != 4 {
		t.Fatalf("%v %+v", err, p)
	}
}

const kYMLFeed = `<?xml version="1.0" encoding="utf-8"?>
<yml_catalog date="2026-08-08T20:40:01+03:00"><shop><name>Лавка</name>
<categories><category id="5665">Тёрки</category></categories>
<offers>
<offer id="059143" available="true">
  <name>Терка пластмассовая</name><price>515.2</price><currencyId>RUB</currencyId>
  <categoryId>5665</categoryId><picture>IMG</picture><picture>IMG</picture>
  <barcode>2000591430017</barcode><vendorCode>TR-1</vendorCode>
  <description>Пять насадок</description><weight>0.75</weight>
</offer>
<offer id="059144" available="true">
  <name>Ковш эмалированный</name><price>1200</price><currencyId>RUB</currencyId>
</offer>
<offer id="059145" available="false">
  <name>Снятый с продажи</name><price>10</price><vendorCode>TR-3</vendorCode>
</offer>
<offer id="059146" available="true">
  <name>Кастрюля минская</name><price>99.99</price><currencyId>BYN</currencyId>
  <vendorCode>TR-4</vendorCode>
</offer>
<offer id="059147" available="true">
  <name>Половник</name><price>80.10</price><vendorCode>TR-5</vendorCode>
</offer>
</offers></shop></yml_catalog>`

func ymlServer(t *testing.T, feed string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.ReplaceAll(feed, "IMG", "http://"+r.Host+"/img.jpg")))
	})
	mux.HandleFunc("/img.jpg", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("\xff\xd8\xffjpegdata"))
	})
	return httptest.NewServer(mux)
}

func TestYMLImport(t *testing.T) {
	srv := ymlServer(t, kYMLFeed)
	defer srv.Close()

	d, _ := database.OpenInMemory()
	defer func() { _ = d.Close() }()

	imp := &YML{URL: srv.URL + "/feed.xml", DefaultStock: 7}
	// available="false" is not counted — the seller cares how many will arrive.
	n, err := imp.Count()
	if err != nil || n != 4 {
		t.Fatalf("count: %v %d", err, n)
	}
	res, err := Run(imp, d, "Ромашка", 1, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// A foreign currency is an error, not an import: BYN arriving silently would break the price list.
	if res.Imported != 3 || res.Errors != 1 {
		t.Fatalf("res: %+v", res)
	}

	p, err := d.GetVisibleProductBySlug("terka-plastmassovaya")
	if err != nil || p.SKU != "TR-1" || p.Price != 51520 || p.Stock != 7 ||
		p.Description != "Пять насадок" {
		t.Fatalf("%v %+v", err, p)
	}
	imgs, _ := d.ListImages(p.ID)
	if len(imgs) != 2 {
		t.Fatalf("images: %+v", imgs)
	}
	// vendorCode is empty — the offer id becomes the SKU, there is no description.
	kovsh, err := d.GetVisibleProductBySlug("kovsh-emalirovannyj")
	if err != nil || kovsh.SKU != "059144" || kovsh.Price != 120000 || kovsh.Description != "" {
		t.Fatalf("%v %+v", err, kovsh)
	}
	if _, err := d.GetVisibleProductBySlug("snyatyj-s-prodazhi"); err == nil {
		t.Fatal("available=false должен был отсеяться")
	}
	if _, err := d.GetVisibleProductBySlug("kastryulya-minskaya"); err == nil {
		t.Fatal("BYN не должен был импортироваться")
	}

	res, _ = Run(imp, d, "Ромашка", 1, "", nil)
	if res.Imported != 0 || res.Skipped != 3 {
		t.Fatalf("dedup: %+v", res)
	}
}

// A feed of ours states a quantity per offer; a feed from anywhere else does not.
// Both arrive through the same source, so one catalogue must be able to hold the
// counted and the uncounted at once.
func TestYMLCount(t *testing.T) {
	const feed = `<?xml version="1.0" encoding="UTF-8"?>
<yml_catalog><shop><offers>
<offer id="1" available="true">
  <name>Кружка</name><price>23.29</price><vendorCode>MG-1</vendorCode><count>13</count>
</offer>
<offer id="2" available="true">
  <name>Графин</name><price>31.40</price><vendorCode>GR-1</vendorCode><count>0</count>
</offer>
<offer id="3" available="true">
  <name>Ковш</name><price>12.00</price><vendorCode>KV-1</vendorCode>
</offer>
</offers></shop></yml_catalog>`
	srv := ymlServer(t, feed)
	defer srv.Close()

	items, err := (&YML{URL: srv.URL + "/feed.xml", DefaultStock: 7}).Fetch()
	if err != nil {
		t.Fatal(err)
	}
	want := []int{13, 0, 7}
	if len(items) != len(want) {
		t.Fatalf("items: %+v", items)
	}
	for i, w := range want {
		if items[i].Stock != w {
			t.Fatalf("%s: stock %d, want %d", items[i].SKU, items[i].Stock, w)
		}
	}
}

func TestYMLTooBig(t *testing.T) {
	srv := ymlServer(t, kYMLFeed)
	defer srv.Close()

	imp := &YML{URL: srv.URL + "/feed.xml", MaxBytes: 100}
	var ke *i18n.KeyError
	if _, err := imp.Count(); !errors.As(err, &ke) || ke.Key != i18n.KeyYMLTooBig {
		t.Fatalf("expected size error, got %v", err)
	}
}

func TestYMLBadURL(t *testing.T) {
	imp := &YML{URL: "ftp://example.com/feed.xml"}
	if _, err := imp.Count(); err == nil {
		t.Fatal("expected scheme error")
	}
}

const kBYNFeed = `<?xml version="1.0" encoding="utf-8"?>
<yml_catalog><shop><offers>
<offer id="1" available="true">
  <name>Кастрюля минская</name><price>84.50</price><currencyId>BYN</currencyId>
  <vendorCode>KM-1</vendorCode>
</offer>
<offer id="2" available="true">
  <name>Сковорода</name><price>60</price><currencyId>BYR</currencyId>
  <vendorCode>SK-1</vendorCode>
</offer>
<offer id="3" available="true">
  <name>Долларовая</name><price>10</price><currencyId>USD</currencyId>
  <vendorCode>US-1</vendorCode>
</offer>
</offers></shop></yml_catalog>`

// A Belarusian feed into a Belarusian shop needs no conversion; a currency the
// shop does not deal in is an error, not a price taken at face value.
func TestYMLFeedCurrency(t *testing.T) {
	srv := ymlServer(t, kBYNFeed)
	defer srv.Close()

	d, _ := database.OpenInMemory()
	defer func() { _ = d.Close() }()
	if err := d.CreateSettings(&database.Settings{OwnerEmail: "a@b.c"}); err != nil {
		t.Fatal(err)
	}
	s, _ := d.GetSettings()
	s.Currency = database.ShopCurrencyBYN
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}

	imp := &YML{URL: srv.URL + "/feed.xml", DefaultStock: 1}
	res, err := Run(imp, d, "Ромашка", 1, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 2 || res.Errors != 1 {
		t.Fatalf("res: %+v", res)
	}
	if c := FeedCurrency(imp); c != database.ShopCurrencyBYN {
		t.Fatalf("feed currency: %q", c)
	}
	p, err := d.GetVisibleProductBySlug("kastryulya-minskaya")
	if err != nil || p.Price != 8450 {
		t.Fatalf("%v %+v", err, p)
	}
}

// A YML feed carries the shop's tree, and it is the tree that turns a catalogue
// into landing pages. The synthetic root every Bitrix export starts with —
// "Главная / Каталог товаров" — is not a category and must not become one.
const kYMLTreeFeed = `<?xml version="1.0" encoding="UTF-8"?>
<yml_catalog><shop><categories>
<category id="1">Главная</category>
<category id="2" parentId="1">Каталог товаров</category>
<category id="10" parentId="2">Текстиль</category>
<category id="11" parentId="10">Спальня</category>
<category id="12" parentId="11">КПБ Евро</category>
<category id="20" parentId="2">Посуда</category>
</categories><offers>
<offer id="1" available="true">
  <name>КПБ сатин</name><price>100</price><vendorCode>A-1</vendorCode><categoryId>12</categoryId>
</offer>
<offer id="2" available="true">
  <name>Кастрюля</name><price>200</price><vendorCode>A-2</vendorCode><categoryId>20</categoryId>
</offer>
<offer id="3" available="true">
  <name>Без категории</name><price>300</price><vendorCode>A-3</vendorCode><categoryId>999</categoryId>
</offer>
</offers></shop></yml_catalog>`

func TestYMLCategoryTree(t *testing.T) {
	srv := ymlServer(t, kYMLTreeFeed)
	defer srv.Close()

	items, err := (&YML{URL: srv.URL + "/feed.xml"}).Fetch()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, it := range items {
		got[it.SKU] = it.Category
	}
	want := map[string]string{
		"A-1": "Текстиль/Спальня/КПБ Евро",
		"A-2": "Посуда",
		"A-3": "", // an unknown categoryId is a product without a category, not a crash
	}
	for sku, w := range want {
		if got[sku] != w {
			t.Errorf("%s: category = %q, want %q", sku, got[sku], w)
		}
	}
}

// The common root is only dropped while something else remains: a shop selling
// one category must keep it.
func TestTrimCommonRootKeepsTheLeaf(t *testing.T) {
	items := []Item{{Category: "Посуда"}, {Category: "Посуда"}}
	trimCommonRoot(items)
	if items[0].Category != "Посуда" {
		t.Errorf("the only category was trimmed away: %q", items[0].Category)
	}
}

// The Ozon taxonomy is one request per import, not one per card, and it gives a
// full path down to the type — the same shape as a YML tree.
func TestOzonCategoryTree(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/product/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"items":[{"product_id":11,"offer_id":"SKU-1"}],"total":1}}`))
	})
	mux.HandleFunc("/v3/product/info/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":11,"offer_id":"SKU-1","name":"Тарелка",
			"price":"300.00","description_category_id":17028922,"type_id":93080}]}`))
	})
	mux.HandleFunc("/v4/product/info/stocks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	})
	mux.HandleFunc("/v1/product/info/description", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"description":""}}`))
	})
	tree := `{"result":[{"description_category_id":1,"category_name":"Дом и сад","children":[
		{"description_category_id":17028922,"category_name":"Посуда для сервировки","children":[
			{"type_id":93080,"type_name":"Тарелка"}]}]}]}`
	var treeCalls int
	mux.HandleFunc("/v1/description-category/tree", func(w http.ResponseWriter, r *http.Request) {
		treeCalls++
		_, _ = w.Write([]byte(tree))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, err := (&Ozon{ClientID: "cid", APIKey: "key", BaseURL: srv.URL}).Fetch()
	if err != nil || len(items) != 1 {
		t.Fatalf("fetch: %v %+v", err, items)
	}
	if want := "Дом и сад/Посуда для сервировки/Тарелка"; items[0].Category != want {
		t.Errorf("category = %q, want %q", items[0].Category, want)
	}
	if treeCalls != 1 {
		t.Errorf("taxonomy fetched %d times, want once per import", treeCalls)
	}
}

// A taxonomy that fails to load costs the categories, not the import: the goods
// are worth more than their shelves.
func TestOzonSurvivesTaxonomyFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/product/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"items":[{"product_id":11,"offer_id":"SKU-1"}],"total":1}}`))
	})
	mux.HandleFunc("/v3/product/info/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"id":11,"offer_id":"SKU-1","name":"Тарелка","price":"300.00"}]}`))
	})
	mux.HandleFunc("/v4/product/info/stocks", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"items":[],"total":0}`))
	})
	mux.HandleFunc("/v1/product/info/description", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"description":""}}`))
	})
	mux.HandleFunc("/v1/description-category/tree", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	items, err := (&Ozon{ClientID: "cid", APIKey: "key", BaseURL: srv.URL}).Fetch()
	if err != nil || len(items) != 1 || items[0].Category != "" {
		t.Fatalf("fetch: %v %+v", err, items)
	}
}
