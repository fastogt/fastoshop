package wb

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// cabinet is a fake Wildberries account: one httptest mux for all four APIs,
// because the paths do not collide and a single server keeps the tests short.
type cabinet struct {
	mu sync.Mutex

	cards []Card
	// stocks records every batch pushed, per warehouse, so a test can assert
	// what actually went on the wire.
	stocks   []StockItem
	refuse   map[string]string // barcode -> reason, answered as a 409
	stockErr int               // non-zero: fail the whole stocks call with this status
	// noScope: answer the Marketplace section with 403, the way a token issued
	// without it does.
	noScope bool

	priceBatches [][]PriceItem
	taskStatus   int              // what /history/tasks reports
	taskErrors   map[int64]string // per-card reasons for a failed task

	orders   []Order
	statuses map[int64]OrderStatus

	// order records the sequence of endpoints hit, so pass ordering is testable.
	calls []string
}

func newCabinet(t *testing.T, cards ...Card) (*cabinet, Hosts) {
	t.Helper()
	c := &cabinet{
		cards:      cards,
		refuse:     map[string]string{},
		taskStatus: kTaskStatusDone,
		statuses:   map[int64]OrderStatus{},
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/content/v2/get/cards/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		c.record("cards")
		_ = json.NewEncoder(w).Encode(cardsResponse{Cards: c.snapshotCards()})
	})

	mux.HandleFunc("/api/v1/seller-info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(SellerInfo{Name: "Тест", TradeMark: "Bihack"})
	})

	mux.HandleFunc("/api/v3/warehouses", func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		denied := c.noScope
		c.mu.Unlock()
		if denied {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"detail":"scope is not allowed for this resource"}`))
			return
		}
		_ = json.NewEncoder(w).Encode([]Warehouse{{ID: 7, Name: "Основной"}})
	})

	mux.HandleFunc("/api/v3/stocks/", func(w http.ResponseWriter, r *http.Request) {
		c.record("stocks")
		var req stocksRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		c.mu.Lock()
		if c.stockErr != 0 {
			status := c.stockErr
			c.mu.Unlock()
			w.WriteHeader(status)
			return
		}
		c.stocks = append(c.stocks, req.Stocks...)
		var bad stocksError
		for _, it := range req.Stocks {
			if reason, ok := c.refuse[it.Sku]; ok {
				bad.Data = append(bad.Data, struct {
					Sku  string `json:"sku"`
					Text string `json:"text"`
				}{Sku: it.Sku, Text: reason})
			}
		}
		c.mu.Unlock()
		if len(bad.Data) > 0 {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(bad)
			return
		}
		// The real cabinet answers an accepted push with 204 and no body.
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/api/v2/upload/task", func(w http.ResponseWriter, r *http.Request) {
		c.record("prices")
		var req priceRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		c.mu.Lock()
		c.priceBatches = append(c.priceBatches, req.Data)
		c.mu.Unlock()
		var resp priceUploadResponse
		resp.Data.ID = 42
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/v2/history/tasks", func(w http.ResponseWriter, r *http.Request) {
		c.record("task-status")
		var resp taskStatusResponse
		c.mu.Lock()
		resp.Data.Status = c.taskStatus
		c.mu.Unlock()
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/v2/buffer/tasks", func(w http.ResponseWriter, r *http.Request) {
		var resp taskErrorsResponse
		c.mu.Lock()
		for nm, reason := range c.taskErrors {
			resp.Data.Details = append(resp.Data.Details, struct {
				NmID   int64  `json:"nmID"`
				Reason string `json:"reason"`
			}{NmID: nm, Reason: reason})
		}
		c.mu.Unlock()
		_ = json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/api/v3/orders", func(w http.ResponseWriter, r *http.Request) {
		c.record("orders")
		c.mu.Lock()
		orders := append([]Order(nil), c.orders...)
		c.mu.Unlock()
		_ = json.NewEncoder(w).Encode(ordersResponse{Orders: orders})
	})

	mux.HandleFunc("/api/v3/orders/status", func(w http.ResponseWriter, r *http.Request) {
		c.record("order-status")
		var req statusRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		var resp statusResponse
		c.mu.Lock()
		for _, id := range req.Orders {
			if s, ok := c.statuses[id]; ok {
				resp.Orders = append(resp.Orders, s)
			}
		}
		c.mu.Unlock()
		_ = json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hosts := Hosts{Content: srv.URL, Prices: srv.URL, Marketplace: srv.URL, Common: srv.URL}
	return c, hosts
}

func (c *cabinet) record(name string) {
	c.mu.Lock()
	c.calls = append(c.calls, name)
	c.mu.Unlock()
}

func (c *cabinet) snapshotCards() []Card {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Card(nil), c.cards...)
}

func (c *cabinet) sentStocks() []StockItem {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]StockItem(nil), c.stocks...)
}

func (c *cabinet) sentPrices() [][]PriceItem {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]PriceItem(nil), c.priceBatches...)
}

func (c *cabinet) callOrder() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

func card(nmID int64, vendorCode string, barcodes ...string) Card {
	c := Card{NmID: nmID, VendorCode: vendorCode, Title: vendorCode}
	for i, b := range barcodes {
		c.Sizes = append(c.Sizes, Size{
			ChrtID: nmID*10 + int64(i), TechSize: "", Skus: []string{b},
		})
	}
	return c
}

func sizedCard(nmID int64, vendorCode string, sizes map[string]string) Card {
	c := Card{NmID: nmID, VendorCode: vendorCode, Title: vendorCode}
	for label, barcode := range sizes {
		c.Sizes = append(c.Sizes, Size{TechSize: label, Skus: []string{barcode}})
	}
	return c
}

func newTest(t *testing.T, cards ...Card) (*Handlers, *database.Database, *cabinet) {
	t.Helper()
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	cab, hosts := newCabinet(t, cards...)
	w := NewWorker(d)
	w.Hosts = hosts
	h := NewHandlers(d, w)
	h.Hosts = hosts
	return h, d, cab
}

// do drives the request through the tab's real chi router, not around it.
func do(t *testing.T, h *Handlers, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	h.Routes().ServeHTTP(w, r)
	t.Logf("%s %s %s -> %d %s", method, path, body, w.Code, strings.TrimSpace(w.Body.String()))
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var env struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %s: %v", w.Body.String(), err)
	}
	return env.Data
}

func seedProduct(t *testing.T, d *database.Database, sku string, stock int, price int64) int64 {
	t.Helper()
	p := &database.Product{Title: sku, SKU: sku, Stock: stock, Price: price}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func enable(t *testing.T, d *database.Database, warehouse string) {
	t.Helper()
	if err := d.SaveWBSettings(&database.WBSettings{
		Token: "token", WarehouseID: warehouse, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsHidesTheToken(t *testing.T) {
	h, _, _ := newTest(t)

	// A fresh database is an empty form, not a 500.
	got := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if got.TokenSet || got.Enabled {
		t.Fatalf("fresh settings are not empty: %+v", got)
	}

	do(t, h, "PUT", "/settings",
		`{"enabled":true,"token":"token","warehouse_id":"7","sandbox":true}`)
	got = decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if !got.TokenSet || !got.Enabled || got.WarehouseID != "7" || !got.Sandbox {
		t.Fatalf("settings not saved: %+v", got)
	}
	if strings.Contains(strings.ToLower(string(mustJSON(t, got))), "\"token\"") {
		t.Fatal("the token itself must never travel back to the browser")
	}

	// A body without the token leaves the stored one alone: the form has nothing
	// to echo back, so it sends nil.
	do(t, h, "PUT", "/settings", `{"enabled":false,"warehouse_id":"9"}`)
	got = decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if !got.TokenSet || got.WarehouseID != "9" {
		t.Fatalf("token lost on a partial save: %+v", got)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// selection is the whole catalogue as a publish body: these tests are about how
// articles match cards, not about which rows the owner ticked.
func selection(t *testing.T, d *database.Database) string {
	t.Helper()
	products, err := d.ListProducts()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
	}
	b, err := json.Marshal(publishRequest{ProductIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPublishMatchesByArticle(t *testing.T) {
	h, d, _ := newTest(t,
		card(1, "ART-1", "2000000000011"),
		sizedCard(2, "ART-2", map[string]string{"M": "2000000000028"}),
	)
	enable(t, d, "7")
	seedProduct(t, d, "ART-1", 5, 1000)
	seedProduct(t, d, "NOPE", 5, 1000)

	res := decode[publishResponse](t, do(t, h, "POST", "/publish", selection(t, d)))
	if res.Published != 1 {
		t.Fatalf("expected one link, got %d", res.Published)
	}
	if len(res.NoCard) != 1 || res.NoCard[0].SKU != "NOPE" {
		t.Fatalf("the unmatched product must be reported: %+v", res.NoCard)
	}
	// The barcode is not ours to invent — it comes off the card.
	rows, err := d.ListWBLinksPage(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Barcode != "2000000000011" || rows[0].NmID != 1 {
		t.Fatalf("link did not take both platform keys: %+v", rows)
	}
}

// A card with several sizes has several barcodes and one article: which size the
// product means is not knowable, so it is reported instead of guessed.
func TestPublishRefusesMultiSizeCard(t *testing.T) {
	h, d, _ := newTest(t,
		sizedCard(3, "ART-3", map[string]string{"M": "2000000000035", "L": "2000000000042"}),
	)
	enable(t, d, "7")
	seedProduct(t, d, "ART-3", 5, 1000)

	res := decode[publishResponse](t, do(t, h, "POST", "/publish", selection(t, d)))
	if res.Published != 0 {
		t.Fatalf("a multi-size card must not be linked blindly: %+v", res)
	}
	if len(res.NoCard) != 1 || res.NoCard[0].Reason == "" {
		t.Fatalf("the refusal must carry a reason: %+v", res.NoCard)
	}
}

// A catalogue imported from Wildberries carries "vendorCode-<size>" in its
// article, because that is what our importer writes there.
func TestPublishMatchesImportedSizeArticle(t *testing.T) {
	h, d, _ := newTest(t,
		sizedCard(4, "ART-4", map[string]string{"M": "2000000000059", "L": "2000000000066"}),
	)
	enable(t, d, "7")
	seedProduct(t, d, "ART-4-M", 5, 1000)

	res := decode[publishResponse](t, do(t, h, "POST", "/publish", selection(t, d)))
	if res.Published != 1 {
		t.Fatalf("the sized article must match its size: %+v", res)
	}
	rows, _ := d.ListWBLinksPage(10, 0)
	if rows[0].Barcode != "2000000000059" {
		t.Fatalf("matched the wrong size: %+v", rows)
	}
}

func TestStockPushCreditsTheWholeBatch(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	seedProduct(t, d, "ART-1", 12, 1000)
	do(t, h, "POST", "/publish", selection(t, d))

	res := decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	if res.Pushed != 1 || res.Failed != 0 {
		t.Fatalf("push counters: %+v", res)
	}
	sent := cab.sentStocks()
	if len(sent) != 1 || sent[0].Sku != "2000000000011" || sent[0].Amount != 12 {
		t.Fatalf("wrong stock on the wire: %+v", sent)
	}

	// Nothing changed since: a second pass must not spend a call.
	do(t, h, "POST", "/push", "")
	if len(cab.sentStocks()) != 1 {
		t.Fatalf("an unchanged level was pushed again: %+v", cab.sentStocks())
	}
}

// A 409 names the barcodes to blame; everything else in the batch still counts.
func TestStockPushMarksOnlyRefusedBarcodes(t *testing.T) {
	h, d, cab := newTest(t,
		card(1, "ART-1", "2000000000011"),
		card(2, "ART-2", "2000000000028"),
	)
	enable(t, d, "7")
	seedProduct(t, d, "ART-1", 3, 1000)
	seedProduct(t, d, "ART-2", 4, 1000)
	do(t, h, "POST", "/publish", selection(t, d))
	cab.refuse["2000000000028"] = "нет такого товара"

	res := decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	if res.Pushed != 1 || res.Failed != 1 {
		t.Fatalf("a partial refusal must not fail the batch: %+v", res)
	}
	bad, err := d.ListWBStockErrors()
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 1 || bad[0].Barcode != "2000000000028" {
		t.Fatalf("the wrong row was blamed: %+v", bad)
	}
}

func TestPassOrdersPricesAfterStocks(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ART-1", 3, 1000)
	do(t, h, "POST", "/publish", selection(t, d))
	if _, err := d.SetWBPrice(id, 1500); err != nil {
		t.Fatal(err)
	}

	do(t, h, "POST", "/push", "")
	order := cab.callOrder()
	var seen []string
	for _, c := range order {
		if c == "orders" || c == "stocks" || c == "prices" {
			seen = append(seen, c)
		}
	}
	want := []string{"orders", "stocks", "prices"}
	if len(seen) != 3 || seen[0] != want[0] || seen[1] != want[1] || seen[2] != want[2] {
		t.Fatalf("a sale must lower stock before levels are pushed: %v", order)
	}
}

// The tab exists to answer "why can I not publish these". On the live shop the
// catalogue is 24 000 products and the cabinet holds a few dozen cards, so a
// table that lists everything and says nothing sends the owner to tick a
// hundred rows for ninety-nine refusals. These are the numbers that stop that.
//
// Wildberries adds a state Ozon does not have: a card with several sizes cannot
// be linked by vendor code alone. It must not be filed under "no card" — the
// card exists, and the owner told to create one would create a duplicate.
func TestCabinetCountsWhatCanActuallyBeLinked(t *testing.T) {
	h, d, _ := newTest(t,
		card(100, "A", "brc-a"),
		sizedCard(200, "SIZED", map[string]string{"S": "brc-s", "M": "brc-m"}),
		card(300, "ORPHAN", "brc-o"),
	)
	enable(t, d, "1")
	idA := seedProduct(t, d, "A", 5, 1000)
	seedProduct(t, d, "SIZED", 5, 1000)
	seedProduct(t, d, "NOPE", 1, 500)

	body, _ := json.Marshal(publishRequest{ProductIDs: []int64{idA}})
	do(t, h, "POST", "/publish", string(body))

	got := decode[cabinetResponse](t, do(t, h, "GET", "/cabinet", ""))
	if got.Cards != 3 || got.Products != 3 {
		t.Fatalf("cards %d products %d", got.Cards, got.Products)
	}
	if got.Linked != 1 {
		t.Errorf("linked %d, want 1", got.Linked)
	}
	if got.Ambiguous != 1 {
		t.Errorf("ambiguous %d, want 1 — a multi-size card is not a missing card",
			got.Ambiguous)
	}
	if got.NoCard != 1 {
		t.Errorf("no_card %d, want 1", got.NoCard)
	}
	if got.Orphans != 1 {
		t.Errorf("orphans %d, want 1", got.Orphans)
	}
	// Nothing here can be linked: A is already linked, SIZED is ambiguous, NOPE
	// has no card. A button offered on any of them would fail.
	if got.Ready != 0 || len(got.ReadyIDs) != 0 {
		t.Errorf("ready %d %v, want none", got.Ready, got.ReadyIDs)
	}
}

// TestCheckReportsMissingStockScope: a Wildberries token is issued per section,
// and one without «Маркетплейс» answers every stock call with 403 while cards
// and prices keep working. The tab then looks connected and the levels quietly
// stay put. Measured on a live seller who pasted exactly such a token.
func TestCheckReportsMissingStockScope(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")

	if got := decode[checkResponse](t, do(t, h, "POST", "/check", "")); got.NoStockScope {
		t.Fatalf("полный токен не должен считаться урезанным: %+v", got)
	}

	cab.mu.Lock()
	cab.noScope = true
	cab.mu.Unlock()

	got := decode[checkResponse](t, do(t, h, "POST", "/check", ""))
	if got.Total != 1 {
		t.Errorf("карточки читаются и без раздела складов: %+v", got)
	}
	if !got.NoStockScope {
		t.Error("отказ склада по правам должен доходить до владельца")
	}
}
