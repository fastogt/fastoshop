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
	t.Fatalf("связи для товара %d нет", id)
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
		t.Fatalf("неуправляемая цена уехала на площадку: %+v", got)
	}

	setPrice(t, d, id, 100000)
	if pushed, failed := pass(t, w); pushed != 1 || failed != 0 {
		t.Fatalf("первая отправка цены: %d/%d", pushed, failed)
	}
	if got := m.lastPriceBatch(t); len(got) != 1 || got[0].OfferID != "A" ||
		got[0].Price != "1000" || got[0].OldPrice != "0" || got[0].CurrencyCode != "RUB" {
		t.Fatalf("батч цен: %+v", got)
	}

	// The same value — no call.
	if pushed, _ := pass(t, w); pushed != 0 {
		t.Fatal("та же цена отправилась повторно")
	}
	if len(m.sentPrices()) != 1 {
		t.Fatalf("лишние вызовы цен: %+v", m.sentPrices())
	}

	// Changed — travels again.
	setPrice(t, d, id, 120000)
	if pushed, _ := pass(t, w); pushed != 1 {
		t.Fatal("новая цена не уехала")
	}
	if got := m.lastPriceBatch(t); got[0].Price != "1200" {
		t.Fatalf("ожидали 1200: %+v", got)
	}

	// Cleared — management stops, nothing else is sent.
	setPrice(t, d, id, 0)
	if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
		t.Fatalf("сброс цены что-то отправил: %d/%d", pushed, failed)
	}
	if len(m.sentPrices()) != 2 {
		t.Fatalf("вызовы цен: %d", len(m.sentPrices()))
	}
}

// TestPriceRoundsUpWithoutFlapping: Ozon takes whole rubles, and remembering the
// rounded value is what keeps the row from being "changed" on every pass.
func TestPriceRoundsUpWithoutFlapping(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 1)
	setPrice(t, d, id, 128850)

	if pushed, _ := pass(t, w); pushed != 2 {
		t.Fatal("остаток и цена должны уехать одним проходом")
	}
	if got := m.lastPriceBatch(t); got[0].Price != "1289" {
		t.Fatalf("округление вверх: %+v", got)
	}
	if r := linkRow(t, d, id); r.PricePushed != 128900 {
		t.Fatalf("запомнили не отправленное значение: %d", r.PricePushed)
	}

	for range 3 {
		if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
			t.Fatalf("цена дребезжит: %d/%d", pushed, failed)
		}
	}
	if len(m.sentPrices()) != 1 {
		t.Fatalf("лишние вызовы цен: %d", len(m.sentPrices()))
	}
}

func TestPriceBatchesBy1000(t *testing.T) {
	w, d, m := newSyncTest(t)
	for i := range 1500 {
		id := seedLinked(t, d, fmt.Sprintf("SKU-%04d", i), 1)
		setPrice(t, d, id, 100000)
	}
	if _, failed := pass(t, w); failed != 0 {
		t.Fatalf("проход с ошибками: %d", failed)
	}
	sizes := []int{}
	for _, b := range m.sentPrices() {
		sizes = append(sizes, len(b))
		if len(b) > kPriceBatchSize {
			t.Fatalf("батч цен больше %d: %d", kPriceBatchSize, len(b))
		}
	}
	if len(sizes) != 2 || sizes[0] != kPriceBatchSize || sizes[1] != 500 {
		t.Fatalf("ожидали 1000+500, получили %v", sizes)
	}
	t.Logf("батчи цен: %v", sizes)
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
		t.Fatalf("проход: %d/%d", pushed, failed)
	}
	r := linkRow(t, d, id)
	if r.PriceError != "цену нельзя менять чаще раза в сутки" || r.PricePushed != -1 {
		t.Fatalf("ошибка цены не записалась: %+v", r)
	}
	if r.StockError != "" || r.StockPushed != 5 {
		t.Fatalf("ошибка цены задела остаток: %+v", r)
	}
	if bad, _ := d.ListOzonStockErrors(); len(bad) != 0 {
		t.Fatalf("остаток отмечен ошибкой: %+v", bad)
	}

	// The row is in backoff — the pass leaves it alone.
	if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
		t.Fatalf("строка в бэкоффе повторилась сразу: %d/%d", pushed, failed)
	}
	if len(m.sentPrices()) != 1 {
		t.Fatalf("лишние вызовы цен: %d", len(m.sentPrices()))
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
		t.Fatalf("проход после отказа по остатку: %d/%d", pushed, failed)
	}
	r = linkRow(t, d, id)
	if r.StockError != "склад не найден" {
		t.Fatalf("ошибка остатка не записалась: %+v", r)
	}
	if r.PriceError != "цену нельзя менять чаще раза в сутки" {
		t.Fatalf("ошибка остатка затёрла ошибку цены: %+v", r)
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
		t.Fatalf("429 на остатках: %d/%d", pushed, failed)
	}
	if got := m.sentPrices(); len(got) != 0 {
		t.Fatalf("цены поехали в упавший кабинет: %+v", got)
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
		t.Fatalf("остаток уехал, цена нет: %d/%d", pushed, failed)
	}
	if r := linkRow(t, d, id); r.PriceError == "" || r.PricePushed != -1 {
		t.Fatalf("ошибка вызова не записалась: %+v", r)
	}
	m.failPriceStatus(0, "")
	if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
		t.Fatalf("бэкофф не соблюдён: %d/%d", pushed, failed)
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
		t.Fatalf("проход: %d/%d", pushed, failed)
	}
	if got := m.lastPriceBatch(t); len(got) != 1 || got[0].CurrencyCode != "BYN" {
		t.Fatalf("валюта не дошла до площадки: %+v", got)
	}
}

// TestSettingsCurrencyValidation: only RUB and BYN are accepted, and a saved
// currency comes back on the next read.
func TestSettingsCurrencyValidation(t *testing.T) {
	h, _ := newTestHandlers(t)

	w := do(t, h, "PUT", "/settings", `{"client_id":"cid","api_key":"key","currency":"USD"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("неверная валюта: %d %s", w.Code, w.Body.String())
	}

	w = do(t, h, "PUT", "/settings", `{"client_id":"cid","api_key":"key","currency":"BYN"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("сохранение BYN: %d %s", w.Code, w.Body.String())
	}
	if got := decode[settingsResponse](t, w); got.Currency != "BYN" {
		t.Fatalf("валюта не сохранилась: %+v", got)
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
		t.Fatalf("свежая база должна давать RUB по умолчанию: %q", s.Currency)
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
		t.Fatalf("порядок вызовов: %v", got)
	}
	t.Logf("порядок вызовов: %v", m.callOrder())
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
		t.Fatalf("заполнено: %+v", got)
	}
	// 999 * 1.25 = 1248.75 kopecks — rounded up, in the seller's favour.
	if r := linkRow(t, d, filled); r.Price != 1249 {
		t.Fatalf("наценка посчиталась неверно: %d", r.Price)
	}
	if r := linkRow(t, d, kept); r.Price != 500000 {
		t.Fatalf("заполнение затёрло выставленную цену: %d", r.Price)
	}
	// A product with a zero shelf price has nothing to mark up.
	if r := linkRow(t, d, zero); r.Price != 0 {
		t.Fatalf("товар без цены получил цену: %d", r.Price)
	}

	// Second press changes nothing — everything is filled already.
	got = decode[fillPricesResponse](t, do(t, h, "POST", "/price/fill", `{"markup_bp":2500}`))
	if got.Filled != 0 {
		t.Fatalf("повторное заполнение: %+v", got)
	}
	if w := do(t, h, "POST", "/price/fill", `{"markup_bp":-1}`); w.Code != http.StatusBadRequest {
		t.Fatalf("отрицательная наценка: %d", w.Code)
	}
}

func TestPriceEndpointValidation(t *testing.T) {
	h, d := newTestHandlers(t)
	id := seedLinked(t, d, "A", 1)
	target := "/price/" + strconv.FormatInt(id, 10)

	if w := do(t, h, "PUT", target, `{"price":128850}`); w.Code != http.StatusOK {
		t.Fatalf("установка цены: %d %s", w.Code, w.Body.String())
	}
	if r := linkRow(t, d, id); r.Price != 128850 {
		t.Fatalf("цена не сохранилась: %+v", r)
	}
	if w := do(t, h, "PUT", target, `{"price":-1}`); w.Code != http.StatusBadRequest {
		t.Fatalf("отрицательная цена: %d", w.Code)
	}
	if w := do(t, h, "PUT", "/price/999999", `{"price":100}`); w.Code != http.StatusNotFound {
		t.Fatalf("несвязанный товар: %d", w.Code)
	}
	if w := do(t, h, "PUT", target, `{`); w.Code != http.StatusBadRequest {
		t.Fatalf("битое тело: %d", w.Code)
	}

	links := decode[ozonLinksResponse](t, do(t, h, "GET", "/links", ""))
	if links.Total != 1 || len(links.Links) != 1 || links.Links[0].Price != 128850 ||
		links.Links[0].OfferID != "A" || links.PageSize != kLinksPageSize {
		t.Fatalf("таблица связей: %+v", links)
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
	t.Logf("связано 2 товара: A (витрина 1000.00 ₽), B (витрина 1288.50 ₽)")

	filled := decode[fillPricesResponse](t, do(t, h, "POST", "/price/fill", `{"markup_bp":2500}`))
	t.Logf("заполнение +25%%: filled=%d", filled.Filled)
	for _, id := range []int64{a, b} {
		r := linkRow(t, d, id)
		t.Logf("  %s: цена на Ozon %s ₽", r.OfferID, decimal(r.Price))
	}

	res := decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	t.Logf("проход: pushed=%d failed=%d, порядок вызовов %v",
		res.Pushed, res.Failed, m.callOrder())
	for _, it := range m.lastPriceBatch(t) {
		t.Logf("  кабинет получил %s: price=%q old_price=%q", it.OfferID, it.Price, it.OldPrice)
	}

	res = decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	t.Logf("повторный проход: pushed=%d failed=%d, вызовов цен %d",
		res.Pushed, res.Failed, len(m.sentPrices()))

	do(t, h, "PUT", "/price/"+strconv.FormatInt(a, 10), `{"price":149900}`)
	m.failPriceOffer("B", "цена ниже минимальной")
	res = decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	t.Logf("после правки цены A: pushed=%d failed=%d", res.Pushed, res.Failed)
	for _, it := range m.lastPriceBatch(t) {
		t.Logf("  кабинет получил %s: price=%q", it.OfferID, it.Price)
	}

	setPrice(t, d, b, 200000)
	res = decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	t.Logf("после отказа по B: pushed=%d failed=%d", res.Pushed, res.Failed)
	s := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	t.Logf("вкладка: цены ждут %d, с ошибкой %d; остатки ждут %d, с ошибкой %d",
		s.PricePending, s.PriceFailed, s.Pending, s.Failed)
	for _, e := range s.PriceErrors {
		t.Logf("  ошибка цены %s: %s", e.OfferID, e.Error)
	}
	if len(s.StockErrors) != 0 {
		t.Fatalf("ошибка цены задела остатки: %+v", s.StockErrors)
	}
}

func decimal(minor int64) string {
	return fmt.Sprintf("%d.%02d", minor/100, minor%100)
}

// Склада FBS в кабинете может ещё не быть — цены от него не зависят и должны
// уезжать, иначе продавец видит молчаливое «ничего не произошло».
func TestPricesPushWithoutWarehouse(t *testing.T) {
	w, d, m := newSyncTest(t)
	if err := d.SaveOzonSettings(&database.OzonSettings{
		ClientID: "cid", APIKey: "key", WarehouseID: "", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	id := seedLinked(t, d, "A", 5)
	setPrice(t, d, id, 100000)

	if pushed, failed := pass(t, w); pushed != 1 || failed != 0 {
		t.Fatalf("цена без склада: %d/%d", pushed, failed)
	}
	if got := m.lastPriceBatch(t); len(got) != 1 || got[0].OfferID != "A" {
		t.Fatalf("батч цен: %+v", got)
	}
	if got := m.sent(); len(got) != 0 {
		t.Fatalf("остатки уехали без склада: %+v", got)
	}
}

// Ошибки вкладки читает владелец, поэтому они идут на его языке — иначе
// англоязычная админка показывает русский текст на каждую неудачу.
func TestErrorsFollowOwnerLanguage(t *testing.T) {
	h, d := newTestHandlers(t)
	if err := d.CreateSettings(&database.Settings{
		OwnerEmail: "a@b.c", PasswordHash: "h"}); err != nil {
		t.Fatal(err)
	}

	// По умолчанию русский.
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
