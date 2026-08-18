package ozon

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

func setPrice(t *testing.T, d *database.Database, id, price int64) {
	t.Helper()
	found, err := d.SetOzonPrice(id, price)
	if err != nil || !found {
		t.Fatalf("set price %d: %v %v", id, err, found)
	}
}

func linkRow(t *testing.T, d *database.Database, id int64) database.OzonLinkRow {
	t.Helper()
	rows, err := d.ListOzonLinksPage(1000, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ProductID == id {
			return r
		}
	}
	t.Fatalf("no link for product %d", id)
	return database.OzonLinkRow{}
}

// TestPriceOptIn is the core promise of the slice: a price we were not asked to
// manage never leaves the shop.
func TestPriceOptIn(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)

	// price = 0 — the pass pushes the stock and stays away from the price.
	pass(t, w)
	if got := m.sentPrices(); len(got) != 0 {
		t.Fatalf("unmanaged price went to the marketplace: %+v", got)
	}

	setPrice(t, d, id, 100000)
	if pushed, failed := pass(t, w); pushed != 1 || failed != 0 {
		t.Fatalf("first price push: %d/%d", pushed, failed)
	}
	if got := m.lastPriceBatch(t); len(got) != 1 || got[0].OfferID != "A" ||
		got[0].Price != "1000" || got[0].OldPrice != "0" || got[0].CurrencyCode != "RUB" {
		t.Fatalf("price batch: %+v", got)
	}

	// The same value — no call.
	if pushed, _ := pass(t, w); pushed != 0 {
		t.Fatal("same price pushed again")
	}
	if len(m.sentPrices()) != 1 {
		t.Fatalf("extra price calls: %+v", m.sentPrices())
	}

	// Changed — travels again.
	setPrice(t, d, id, 120000)
	if pushed, _ := pass(t, w); pushed != 1 {
		t.Fatal("new price not pushed")
	}
	if got := m.lastPriceBatch(t); got[0].Price != "1200" {
		t.Fatalf("want 1200: %+v", got)
	}

	// Cleared — management stops, nothing else is sent.
	setPrice(t, d, id, 0)
	if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
		t.Fatalf("clearing the price pushed something: %d/%d", pushed, failed)
	}
	if len(m.sentPrices()) != 2 {
		t.Fatalf("price calls: %d", len(m.sentPrices()))
	}
}

// TestPriceRoundsUpWithoutFlapping: Ozon takes whole rubles, and remembering the
// rounded value is what keeps the row from being "changed" on every pass.
func TestPriceRoundsUpWithoutFlapping(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 1)
	setPrice(t, d, id, 128850)

	if pushed, _ := pass(t, w); pushed != 2 {
		t.Fatal("stock and price must go in one pass")
	}
	if got := m.lastPriceBatch(t); got[0].Price != "1289" {
		t.Fatalf("rounding up: %+v", got)
	}
	if r := linkRow(t, d, id); r.PricePushed != 128900 {
		t.Fatalf("recorded a value that was not sent: %d", r.PricePushed)
	}

	for range 3 {
		if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
			t.Fatalf("price flapping: %d/%d", pushed, failed)
		}
	}
	if len(m.sentPrices()) != 1 {
		t.Fatalf("extra price calls: %d", len(m.sentPrices()))
	}
}

func TestPriceBatchesBy1000(t *testing.T) {
	w, d, m := newSyncTest(t)
	for i := range 1500 {
		id := seedLinked(t, d, fmt.Sprintf("SKU-%04d", i), 1)
		setPrice(t, d, id, 100000)
	}
	if _, failed := pass(t, w); failed != 0 {
		t.Fatalf("pass with errors: %d", failed)
	}
	sizes := []int{}
	for _, b := range m.sentPrices() {
		sizes = append(sizes, len(b))
		if len(b) > kPriceBatchSize {
			t.Fatalf("price batch over %d: %d", kPriceBatchSize, len(b))
		}
	}
	if len(sizes) != 2 || sizes[0] != kPriceBatchSize || sizes[1] != 500 {
		t.Fatalf("want 1000+500, got %v", sizes)
	}
	t.Logf("price batches: %v", sizes)
}

// TestPriceErrorsDoNotMixWithStock: two pushes share the row, so a complaint
// about one of them must not erase what is known about the other.
func TestPriceErrorsDoNotMixWithStock(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	seedLinked(t, d, "B", 5)
	setPrice(t, d, id, 100000)
	m.failPriceOffer("A", "цену нельзя менять чаще раза в сутки")

	if pushed, failed := pass(t, w); pushed != 2 || failed != 1 {
		t.Fatalf("pass: %d/%d", pushed, failed)
	}
	r := linkRow(t, d, id)
	if r.PriceError != "цену нельзя менять чаще раза в сутки" || r.PricePushed != -1 {
		t.Fatalf("price error not recorded: %+v", r)
	}
	if r.StockError != "" || r.StockPushed != 5 {
		t.Fatalf("price error affected stock: %+v", r)
	}
	if bad, _ := d.ListOzonStockErrors(); len(bad) != 0 {
		t.Fatalf("stock flagged with an error: %+v", bad)
	}

	// The row is in backoff — the pass leaves it alone.
	if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
		t.Fatalf("row in backoff retried immediately: %d/%d", pushed, failed)
	}
	if len(m.sentPrices()) != 1 {
		t.Fatalf("extra price calls: %d", len(m.sentPrices()))
	}

	// Now the platform rejects the stock. The price error must survive.
	if err := d.MarkOzonPriceError(id, r.PriceError, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	m.failOffer("A", "склад не найден")
	setStock(t, d, id, 3)
	// One failure, not two: retry_at is shared, so the fresh stock backoff also
	// postpones the price of the same row within this pass.
	if pushed, failed := pass(t, w); pushed != 0 || failed != 1 {
		t.Fatalf("pass after a stock rejection: %d/%d", pushed, failed)
	}
	r = linkRow(t, d, id)
	if r.StockError != "склад не найден" {
		t.Fatalf("stock error not recorded: %+v", r)
	}
	if r.PriceError != "цену нельзя менять чаще раза в сутки" {
		t.Fatalf("stock error overwrote the price error: %+v", r)
	}
}

// TestPricesStopOnWholeStockCallFailure: a cabinet that is down does not get the
// price batches of the same pass on top.
func TestPricesStopOnWholeStockCallFailure(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	setPrice(t, d, id, 100000)
	m.failStatus(http.StatusTooManyRequests, "120")

	if pushed, failed := pass(t, w); pushed != 0 || failed != 1 {
		t.Fatalf("429 on stocks: %d/%d", pushed, failed)
	}
	if got := m.sentPrices(); len(got) != 0 {
		t.Fatalf("prices went to a failing cabinet: %+v", got)
	}
}

// TestPriceCallFailureBacksOff: a dead prices call backs the whole batch off
// instead of hammering the cabinet within the same pass.
func TestPriceCallFailureBacksOff(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	setPrice(t, d, id, 100000)
	m.failPriceStatus(http.StatusInternalServerError, "")

	if pushed, failed := pass(t, w); pushed != 1 || failed != 1 {
		t.Fatalf("stock pushed, price did not: %d/%d", pushed, failed)
	}
	if r := linkRow(t, d, id); r.PriceError == "" || r.PricePushed != -1 {
		t.Fatalf("call error not recorded: %+v", r)
	}
	m.failPriceStatus(0, "")
	if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
		t.Fatalf("backoff not respected: %d/%d", pushed, failed)
	}
}

// TestPriceCurrencyBYN: a real ozon.by cabinet trades in BYN, and the price
// push must carry that instead of the RUB default.
func TestPriceCurrencyBYN(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	setPrice(t, d, id, 100000)

	s, err := d.GetOzonSettings()
	if err != nil {
		t.Fatal(err)
	}
	s.Currency = database.OzonCurrencyBYN
	if err := d.SaveOzonSettings(s); err != nil {
		t.Fatal(err)
	}

	if pushed, failed := pass(t, w); pushed != 2 || failed != 0 {
		t.Fatalf("pass: %d/%d", pushed, failed)
	}
	if got := m.lastPriceBatch(t); len(got) != 1 || got[0].CurrencyCode != "BYN" {
		t.Fatalf("currency did not reach the marketplace: %+v", got)
	}
}

// TestSettingsCurrencyValidation: only RUB and BYN are accepted, and a saved
// currency comes back on the next read.
func TestSettingsCurrencyValidation(t *testing.T) {
	h, _ := newTestHandlers(t)

	w := do(t, h, "PUT", "/settings", `{"client_id":"cid","api_key":"key","currency":"USD"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid currency: %d %s", w.Code, w.Body.String())
	}

	w = do(t, h, "PUT", "/settings", `{"client_id":"cid","api_key":"key","currency":"BYN"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("saving BYN: %d %s", w.Code, w.Body.String())
	}
	if got := decode[settingsResponse](t, w); got.Currency != "BYN" {
		t.Fatalf("currency not saved: %+v", got)
	}
}

// TestSettingsCurrencyDefaultRUB: a fresh install with no ozon_settings row
// must read back as RUB, not an empty string.
func TestSettingsCurrencyDefaultRUB(t *testing.T) {
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	s, err := d.GetOzonSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Currency != database.OzonCurrencyRUB {
		t.Fatalf("fresh database must default to RUB: %q", s.Currency)
	}
}

// TestPassOrder pins the order the pass must keep: sales first (they lower our
// stock), then stocks, then prices.
func TestPassOrder(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	setPrice(t, d, id, 100000)

	pass(t, w)
	if got := m.callOrder(); !slices.Equal(got, []string{"postings", "stocks", "prices"}) {
		t.Fatalf("call order: %v", got)
	}
	t.Logf("call order: %v", m.callOrder())
}

func TestFillPricesOnlyEmpty(t *testing.T) {
	h, d := newTestHandlers(t)
	kept := seedLinked(t, d, "A", 1)
	filled := seedLinked(t, d, "B", 1)
	zero := seedLinked(t, d, "C", 1)

	for id, price := range map[int64]int64{kept: 100000, filled: 999, zero: 0} {
		p, err := d.GetProduct(id)
		if err != nil {
			t.Fatal(err)
		}
		p.Price = price
		if err := d.UpdateProduct(p); err != nil {
			t.Fatal(err)
		}
	}
	setPrice(t, d, kept, 500000)

	got := decode[fillPricesResponse](t, do(t, h, "POST", "/price/fill", `{"markup_bp":2500}`))
	if got.Filled != 1 {
		t.Fatalf("filled: %+v", got)
	}
	// 999 * 1.25 = 1248.75 kopecks — rounded up, in the seller's favour.
	if r := linkRow(t, d, filled); r.Price != 1249 {
		t.Fatalf("markup computed incorrectly: %d", r.Price)
	}
	if r := linkRow(t, d, kept); r.Price != 500000 {
		t.Fatalf("fill overwrote a manually set price: %d", r.Price)
	}
	// A product with a zero shelf price has nothing to mark up.
	if r := linkRow(t, d, zero); r.Price != 0 {
		t.Fatalf("product without a price got one: %d", r.Price)
	}

	// Second press changes nothing — everything is filled already.
	got = decode[fillPricesResponse](t, do(t, h, "POST", "/price/fill", `{"markup_bp":2500}`))
	if got.Filled != 0 {
		t.Fatalf("repeated fill: %+v", got)
	}
	if w := do(t, h, "POST", "/price/fill", `{"markup_bp":-1}`); w.Code != http.StatusBadRequest {
		t.Fatalf("negative markup: %d", w.Code)
	}
}

func TestPriceEndpointValidation(t *testing.T) {
	h, d := newTestHandlers(t)
	id := seedLinked(t, d, "A", 1)
	target := "/price/" + strconv.FormatInt(id, 10)

	if w := do(t, h, "PUT", target, `{"price":128850}`); w.Code != http.StatusOK {
		t.Fatalf("setting the price: %d %s", w.Code, w.Body.String())
	}
	if r := linkRow(t, d, id); r.Price != 128850 {
		t.Fatalf("price not saved: %+v", r)
	}
	if w := do(t, h, "PUT", target, `{"price":-1}`); w.Code != http.StatusBadRequest {
		t.Fatalf("negative price: %d", w.Code)
	}
	if w := do(t, h, "PUT", "/price/999999", `{"price":100}`); w.Code != http.StatusNotFound {
		t.Fatalf("unlinked product: %d", w.Code)
	}
	if w := do(t, h, "PUT", target, `{`); w.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: %d", w.Code)
	}

	links := decode[ozonLinksResponse](t, do(t, h, "GET", "/links", ""))
	if links.Total != 1 || len(links.Links) != 1 || links.Links[0].Price != 128850 ||
		links.Links[0].OfferID != "A" || links.PageSize != kLinksPageSize {
		t.Fatalf("links table: %+v", links)
	}
}

// TestSmokePriceSlice is the transcript of the whole slice end to end: link,
// fill, push, edit, per-item rejection.
func TestSmokePriceSlice(t *testing.T) {
	h, d := newTestHandlers(t)
	m := newOzonMock(t)
	h.worker.BaseURL = m.URL
	if err := d.SaveOzonSettings(&database.OzonSettings{
		ClientID: "cid", APIKey: "key", WarehouseID: "77", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	a, b := seedLinked(t, d, "A", 3), seedLinked(t, d, "B", 7)
	for id, price := range map[int64]int64{a: 100000, b: 128850} {
		p, err := d.GetProduct(id)
		if err != nil {
			t.Fatal(err)
		}
		p.Price = price
		if err := d.UpdateProduct(p); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("linked 2 products: A (storefront 1000.00 ₽), B (storefront 1288.50 ₽)")

	filled := decode[fillPricesResponse](t, do(t, h, "POST", "/price/fill", `{"markup_bp":2500}`))
	t.Logf("fill +25%%: filled=%d", filled.Filled)
	for _, id := range []int64{a, b} {
		r := linkRow(t, d, id)
		t.Logf("  %s: Ozon price %s ₽", r.OfferID, decimal(r.Price))
	}

	res := decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	t.Logf("pass: pushed=%d failed=%d, call order %v",
		res.Pushed, res.Failed, m.callOrder())
	for _, it := range m.lastPriceBatch(t) {
		t.Logf("  cabinet received %s: price=%q old_price=%q", it.OfferID, it.Price, it.OldPrice)
	}

	res = decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	t.Logf("second pass: pushed=%d failed=%d, price calls %d",
		res.Pushed, res.Failed, len(m.sentPrices()))

	do(t, h, "PUT", "/price/"+strconv.FormatInt(a, 10), `{"price":149900}`)
	m.failPriceOffer("B", "цена ниже минимальной")
	res = decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	t.Logf("after editing price A: pushed=%d failed=%d", res.Pushed, res.Failed)
	for _, it := range m.lastPriceBatch(t) {
		t.Logf("  cabinet received %s: price=%q", it.OfferID, it.Price)
	}

	setPrice(t, d, b, 200000)
	res = decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	t.Logf("after rejection on B: pushed=%d failed=%d", res.Pushed, res.Failed)
	s := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	t.Logf("tab: prices pending %d, failed %d; stocks pending %d, failed %d",
		s.PricePending, s.PriceFailed, s.Pending, s.Failed)
	for _, e := range s.PriceErrors {
		t.Logf("  price error %s: %s", e.OfferID, e.Error)
	}
	if len(s.StockErrors) != 0 {
		t.Fatalf("price error affected stocks: %+v", s.StockErrors)
	}
}

func decimal(minor int64) string {
	return fmt.Sprintf("%d.%02d", minor/100, minor%100)
}

// The cabinet may not yet have an FBS warehouse — prices don't depend on it and
// must go out, or the seller sees a silent "nothing happened".
func TestPricesPushWithoutWarehouse(t *testing.T) {
	w, d, m := newSyncTest(t)
	if err := d.SaveOzonSettings(&database.OzonSettings{
		ClientID: "cid", APIKey: "key", WarehouseID: "", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	id := seedLinked(t, d, "A", 5)
	setPrice(t, d, id, 100000)

	if pushed, failed := pass(t, w); pushed != 1 || failed != 0 {
		t.Fatalf("price without a warehouse: %d/%d", pushed, failed)
	}
	if got := m.lastPriceBatch(t); len(got) != 1 || got[0].OfferID != "A" {
		t.Fatalf("price batch: %+v", got)
	}
	if got := m.sent(); len(got) != 0 {
		t.Fatalf("stocks pushed without a warehouse: %+v", got)
	}
}

// The tab's errors are read by the owner, so they come in the owner's language —
// otherwise an English-language admin shows Russian text on every failure.
func TestErrorsFollowOwnerLanguage(t *testing.T) {
	h, d := newTestHandlers(t)
	if err := d.CreateSettings(&database.Settings{
		OwnerEmail: "a@b.c", PasswordHash: "h"}); err != nil {
		t.Fatal(err)
	}

	// Russian by default.
	w := do(t, h, "PUT", "/price/1", `{"price":-5}`)
	if w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "отрицательной") {
		t.Fatalf("ru: %d %s", w.Code, w.Body.String())
	}

	if err := d.UpdateSettings(&database.Settings{
		OwnerEmail: "a@b.c", PasswordHash: "h",
		Currency: database.ShopCurrencyRUB, Lang: i18n.LangEN}); err != nil {
		t.Fatal(err)
	}
	w = do(t, h, "PUT", "/price/1", `{"price":-5}`)
	if !strings.Contains(w.Body.String(), "price cannot be negative") {
		t.Fatalf("en: %d %s", w.Code, w.Body.String())
	}
}
