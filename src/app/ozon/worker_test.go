package ozon

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fastogt/fastoshop/app/channel"
	"github.com/fastogt/fastoshop/app/database"
)

// ozonMock is a cabinet accepting stocks and prices: it remembers the batches
// and can answer like the platform on a bad day (429, an error on one article).
type ozonMock struct {
	URL   string
	calls chan struct{}

	mu           sync.Mutex
	batches      [][]StockItem
	priceBatches [][]PriceItem
	// order records which endpoints were hit and in what sequence - the pass has
	// to poll orders before it pushes, and push stocks before prices.
	order        []string
	status       int
	priceStatus  int
	header       string
	itemErr      map[string]string
	priceItemErr map[string]string
	postings     []Posting
	// postingsRaw replaces the /v3/posting/fbs/list answer wholesale - that is
	// how we check behaviour on a format we did not expect.
	postingsRaw string
	pollFilters []postingFilter
	// offers is the cabinet's card list, needed by publication: linking matches
	// products.sku against these offer_ids.
	offers []string
}

func (m *ozonMock) setOffers(offers ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.offers = offers
}

func newOzonMock(t *testing.T) *ozonMock {
	t.Helper()
	m := &ozonMock{calls: make(chan struct{}, 64),
		itemErr: map[string]string{}, priceItemErr: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/posting/fbs/list", func(w http.ResponseWriter, r *http.Request) {
		var req postingListRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.mu.Lock()
		m.order = append(m.order, "postings")
		m.pollFilters = append(m.pollFilters, req.Filter)
		raw, postings := m.postingsRaw, append([]Posting(nil), m.postings...)
		m.mu.Unlock()
		if raw != "" {
			_, _ = w.Write([]byte(raw))
			return
		}
		var resp postingListResponse
		if req.Offset < len(postings) {
			resp.Result.Postings = postings[req.Offset:]
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/v2/products/stocks", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Client-Id") != "cid" || r.Header.Get("Api-Key") != "key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req stocksRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		m.mu.Lock()
		m.order = append(m.order, "stocks")
		m.batches = append(m.batches, req.Stocks)
		status, header, itemErr := m.status, m.header, maps.Clone(m.itemErr)
		m.mu.Unlock()
		select {
		case m.calls <- struct{}{}:
		default:
		}

		if status != 0 {
			if header != "" {
				w.Header().Set("Retry-After", header)
			}
			w.WriteHeader(status)
			return
		}
		var resp stocksResponse
		for _, it := range req.Stocks {
			res := ItemResult{OfferID: it.OfferID, Updated: true}
			if msg, bad := itemErr[it.OfferID]; bad {
				res.Updated = false
				res.Errors = []itemError{{Code: "ERR", Message: msg}}
			}
			resp.Result = append(resp.Result, res)
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/v1/product/import/prices", func(w http.ResponseWriter, r *http.Request) {
		var req pricesRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		m.mu.Lock()
		m.order = append(m.order, "prices")
		m.priceBatches = append(m.priceBatches, req.Prices)
		status, header, itemErr := m.priceStatus, m.header, maps.Clone(m.priceItemErr)
		m.mu.Unlock()

		if status != 0 {
			if header != "" {
				w.Header().Set("Retry-After", header)
			}
			w.WriteHeader(status)
			return
		}
		var resp pricesResponse
		for _, it := range req.Prices {
			res := ItemResult{OfferID: it.OfferID, Updated: true}
			if msg, bad := itemErr[it.OfferID]; bad {
				res.Updated = false
				res.Errors = []itemError{{Code: "ERR", Message: msg}}
			}
			resp.Result = append(resp.Result, res)
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/v3/product/list", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		offers := append([]string(nil), m.offers...)
		m.mu.Unlock()
		var page listResponse
		for i, o := range offers {
			page.Result.Items = append(page.Result.Items,
				Offer{ProductID: int64(100 + i), OfferID: o})
		}
		page.Result.Total = len(offers)
		_ = json.NewEncoder(w).Encode(page)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	m.URL = srv.URL
	return m
}

func (m *ozonMock) sent() [][]StockItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]StockItem(nil), m.batches...)
}

func (m *ozonMock) lastBatch(t *testing.T) []StockItem {
	t.Helper()
	all := m.sent()
	if len(all) == 0 {
		t.Fatal("cabinet received no calls")
	}
	return all[len(all)-1]
}

func (m *ozonMock) failStatus(status int, retryAfter string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status, m.header = status, retryAfter
}

func (m *ozonMock) failOffer(offerID, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.itemErr[offerID] = msg
}

func (m *ozonMock) sentPrices() [][]PriceItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([][]PriceItem(nil), m.priceBatches...)
}

func (m *ozonMock) lastPriceBatch(t *testing.T) []PriceItem {
	t.Helper()
	all := m.sentPrices()
	if len(all) == 0 {
		t.Fatal("cabinet received no price calls")
	}
	return all[len(all)-1]
}

func (m *ozonMock) failPriceOffer(offerID, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg == "" {
		delete(m.priceItemErr, offerID)
		return
	}
	m.priceItemErr[offerID] = msg
}

func (m *ozonMock) failPriceStatus(status int, retryAfter string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.priceStatus, m.header = status, retryAfter
}

func (m *ozonMock) callOrder() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.order...)
}

// setPostings defines what the cabinet returns on the next order poll.
func (m *ozonMock) setPostings(p ...Posting) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postings = p
}

func (m *ozonMock) filters() []postingFilter {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]postingFilter(nil), m.pollFilters...)
}

func posting(number, status string, items ...PostingProduct) Posting {
	return Posting{
		PostingNumber: number, Status: status,
		InProcessAt: time.Now().UTC().Format(time.RFC3339), Products: items,
	}
}

func line(offerID string, qty int) PostingProduct {
	return PostingProduct{OfferID: offerID, Quantity: qty}
}

func stockOf(t *testing.T, d *database.Database, id int64) int {
	t.Helper()
	p, err := d.GetProduct(id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Stock
}

func newSyncTest(t *testing.T) (*Worker, *database.Database, *ozonMock) {
	t.Helper()
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	m := newOzonMock(t)
	// A real shop always has settings, and its currency is what prices are
	// labelled with on the way out.
	if err := d.CreateSettings(&database.Settings{
		ShopName: "лавка", Currency: database.ShopCurrencyRUB}); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveOzonSettings(&database.OzonSettings{
		ClientID: "cid", APIKey: "key", WarehouseID: "77", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(d)
	w.BaseURL = m.URL
	return w, d, m
}

// seedLinked creates a product with a stock and links it right away to the
// cabinet card of the same name.
func seedLinked(t *testing.T, d *database.Database, sku string, stock int) int64 {
	t.Helper()
	p := &database.Product{SKU: sku, Title: "Товар " + sku, Stock: stock}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertOzonLink(&database.OzonLink{ProductID: p.ID, OfferID: sku}); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func setStock(t *testing.T, d *database.Database, id int64, stock int) {
	t.Helper()
	p, err := d.GetProduct(id)
	if err != nil {
		t.Fatal(err)
	}
	p.Stock = stock
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}
}

func pass(t *testing.T, w *Worker) (int, int) {
	t.Helper()
	pushed, failed, err := w.Pass()
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	t.Logf("pass -> pushed=%d failed=%d", pushed, failed)
	return pushed, failed
}

func waitIdle(t *testing.T, w *Worker) {
	t.Helper()
	for range 200 {
		if !w.running.Load() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("sync pass did not finish")
}

// TestPushFollowsLevelBothWays: the sync runs both ways - platform sales arrive
// by polling, and a push upwards must not overwrite a sale we have not polled
// yet.
func TestPushFollowsLevelBothWays(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)

	// The first push is allowed at any level: it is what sets the baseline.
	if pushed, failed := pass(t, w); pushed != 1 || failed != 0 {
		t.Fatalf("first pass: %d/%d", pushed, failed)
	}
	if got := m.lastBatch(t); len(got) != 1 || got[0].Stock != 5 ||
		got[0].OfferID != "A" || got[0].WarehouseID != 77 {
		t.Fatalf("first batch: %+v", got)
	}

	// The level did not change - there must be no call.
	if pushed, _ := pass(t, w); pushed != 0 {
		t.Fatalf("same level pushed again")
	}
	if len(m.sent()) != 1 {
		t.Fatalf("extra calls: %+v", m.sent())
	}

	// Downwards - travels.
	setStock(t, d, id, 3)
	if pushed, _ := pass(t, w); pushed != 1 {
		t.Fatal("stock decrease not pushed")
	}
	if got := m.lastBatch(t); got[0].Stock != 3 {
		t.Fatalf("want 3: %+v", got)
	}

	// Upwards - travels too.
	setStock(t, d, id, 7)
	if pushed, _ := pass(t, w); pushed != 1 {
		t.Fatal("stock increase not pushed")
	}
	if got := m.lastBatch(t); got[0].Stock != 7 {
		t.Fatalf("want 7: %+v", got)
	}
	if len(m.sent()) != 3 {
		t.Fatalf("extra calls: %d", len(m.sent()))
	}
}

func TestPushBatchesBy100(t *testing.T) {
	w, d, m := newSyncTest(t)
	for i := range 250 {
		seedLinked(t, d, fmt.Sprintf("SKU-%03d", i), 1)
	}
	if pushed, failed := pass(t, w); pushed != 250 || failed != 0 {
		t.Fatalf("250 links: %d/%d", pushed, failed)
	}
	sizes := []int{}
	for _, b := range m.sent() {
		sizes = append(sizes, len(b))
		if len(b) > kBatchSize {
			t.Fatalf("batch over %d: %d", kBatchSize, len(b))
		}
	}
	if len(sizes) != 3 {
		t.Fatalf("want 3 calls, got %v", sizes)
	}
	t.Logf("batches: %v", sizes)
}

func TestPushPerItemErrorIsIsolated(t *testing.T) {
	w, d, m := newSyncTest(t)
	seedLinked(t, d, "A", 5)
	seedLinked(t, d, "B", 5)
	m.failOffer("B", "склад не найден")

	if pushed, failed := pass(t, w); pushed != 1 || failed != 1 {
		t.Fatalf("want 1 accepted and 1 rejected: %d/%d", pushed, failed)
	}
	bad, err := d.ListOzonStockErrors()
	if err != nil || len(bad) != 1 || bad[0].OfferID != "B" ||
		bad[0].Error != "склад не найден" {
		t.Fatalf("error not recorded: %v %+v", err, bad)
	}

	// A row in backoff does not make the next selection, and neither does the
	// healthy one (its level is already pushed), so there are no more calls.
	if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
		t.Fatalf("row in backoff retried immediately: %d/%d", pushed, failed)
	}
	if len(m.sent()) != 1 {
		t.Fatalf("extra calls: %d", len(m.sent()))
	}

	// The backoff has expired - retry, successfully this time.
	if err := d.MarkOzonStockError(bad[0].ProductID, "склад не найден",
		time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	m.failOffer("B", "")
	m.mu.Lock()
	delete(m.itemErr, "B")
	m.mu.Unlock()
	if pushed, failed := pass(t, w); pushed != 1 || failed != 0 {
		t.Fatalf("retry after backoff: %d/%d", pushed, failed)
	}
	if bad, _ := d.ListOzonStockErrors(); len(bad) != 0 {
		t.Fatalf("error not cleared: %+v", bad)
	}
}

// TestPushHonoursRetryAfter checks the recorded retry_at for real, hence a file
// database: with ":memory:" a second connection gets its own empty database.
func TestPushHonoursRetryAfter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shop.db")
	d, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	m := newOzonMock(t)
	// A real shop always has settings, and its currency is what prices are
	// labelled with on the way out.
	if err := d.CreateSettings(&database.Settings{
		ShopName: "лавка", Currency: database.ShopCurrencyRUB}); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveOzonSettings(&database.OzonSettings{
		ClientID: "cid", APIKey: "key", WarehouseID: "77", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	w := NewWorker(d)
	w.BaseURL = m.URL

	seedLinked(t, d, "A", 5)
	seedLinked(t, d, "B", 5)
	m.failStatus(http.StatusTooManyRequests, "120")

	before := time.Now().UTC()
	if pushed, failed := pass(t, w); pushed != 0 || failed != 2 {
		t.Fatalf("429: %d/%d", pushed, failed)
	}
	bad, err := d.ListOzonStockErrors()
	if err != nil || len(bad) != 2 {
		t.Fatalf("errors not recorded: %v %+v", err, bad)
	}

	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	// CAST to TEXT: otherwise the driver parses a DATETIME column into a
	// time.Time and hands it back in its own format, while what must be checked
	// is exactly what lies in the database - the comparison against
	// CURRENT_TIMESTAMP runs on that string.
	rows, err := raw.Query(
		`SELECT offer_id, CAST(retry_at AS TEXT) FROM ozon_links ORDER BY offer_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var offer, at string
		if err := rows.Scan(&offer, &at); err != nil {
			t.Fatal(err)
		}
		parsed, err := time.Parse("2006-01-02 15:04:05", at)
		if err != nil {
			t.Fatalf("retry_at %q: %v", at, err)
		}
		delay := parsed.Sub(before)
		t.Logf("%s retry_at=%s (+%s)", offer, at, delay.Truncate(time.Second))
		if delay < 110*time.Second || delay > 130*time.Second {
			t.Fatalf("%s: Retry-After: 120 ignored, deferred by %s", offer, delay)
		}
		seen++
	}
	if seen != 2 {
		t.Fatalf("rows with retry_at: %d", seen)
	}

	// Before the deadline we do not touch the platform.
	m.failStatus(0, "")
	if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
		t.Fatalf("pass inside Retry-After not empty: %d/%d", pushed, failed)
	}
	if len(m.sent()) != 1 {
		t.Fatalf("extra calls inside Retry-After: %d", len(m.sent()))
	}
}

func TestPushZeroForDeletedProduct(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	pass(t, w)

	if err := d.DeleteProduct(id); err != nil {
		t.Fatal(err)
	}
	if pushed, _ := pass(t, w); pushed != 1 {
		t.Fatal("deleted product not zeroed on the marketplace")
	}
	if got := m.lastBatch(t); got[0].Stock != 0 {
		t.Fatalf("want zero: %+v", got)
	}
	// The zero is sent - after that the link stays quiet instead of sending a
	// zero every tick.
	if pushed, _ := pass(t, w); pushed != 0 {
		t.Fatal("zero pushed again")
	}
	if len(m.sent()) != 2 {
		t.Fatalf("extra calls: %d", len(m.sent()))
	}
}

func TestWorkerWakesOnStockChange(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The mock signals a call before the worker writes the result to the
	// database: leaving the test while a pass is in flight would close the
	// database under its hands.
	defer waitIdle(t, w)
	go w.Run(ctx)

	// The tick is every 5 minutes; waiting for a call within a second is only
	// possible through the wake channel.
	w.StockChanged()
	select {
	case <-m.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not wake up on StockChanged")
	}
	if got := m.lastBatch(t); got[0].Stock != 5 {
		t.Fatalf("first batch: %+v", got)
	}

	setStock(t, d, id, 2)
	w.StockChanged()
	select {
	case <-m.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not wake up on a sale")
	}
	if got := m.lastBatch(t); got[0].Stock != 2 {
		t.Fatalf("second batch: %+v", got)
	}
}

func TestPassNoopWhenNotConfigured(t *testing.T) {
	w, d, m := newSyncTest(t)
	seedLinked(t, d, "A", 5)

	for _, s := range []*database.OzonSettings{
		{ClientID: "cid", APIKey: "key", WarehouseID: "77", Enabled: false},
		{ClientID: "", APIKey: "key", WarehouseID: "77", Enabled: true},
		{ClientID: "cid", APIKey: "", WarehouseID: "77", Enabled: true},
		{ClientID: "cid", APIKey: "key", WarehouseID: "", Enabled: true},
	} {
		if err := d.SaveOzonSettings(s); err != nil {
			t.Fatal(err)
		}
		if pushed, failed := pass(t, w); pushed != 0 || failed != 0 {
			t.Fatalf("%+v: %d/%d", s, pushed, failed)
		}
	}
	if len(m.sent()) != 0 {
		t.Fatalf("half-configured integration called the cabinet: %+v", m.sent())
	}

	// A non-numeric warehouse is not "quietly do nothing" but a clear error.
	if err := d.SaveOzonSettings(&database.OzonSettings{
		ClientID: "cid", APIKey: "key", WarehouseID: "склад", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.Pass(); err == nil {
		t.Fatal("non-numeric warehouse_id must be an error")
	}
}

func TestPushEndpoint(t *testing.T) {
	h, d := newTestHandlers(t)
	m := newOzonMock(t)
	h.worker.BaseURL = m.URL
	if err := d.SaveOzonSettings(&database.OzonSettings{
		ClientID: "cid", APIKey: "key", WarehouseID: "77", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	id := seedLinked(t, d, "A", 5)

	got := decode[channel.PushResponse](t, do(t, h, "POST", "/push", ""))
	if got.Pushed != 1 || got.Failed != 0 {
		t.Fatalf("push: %+v", got)
	}
	// The button drives the level both ways.
	setStock(t, d, id, 9)
	if got = decode[channel.PushResponse](t, do(t, h, "POST", "/push", "")); got.Pushed != 1 {
		t.Fatalf("button did not push the stock increase: %+v", got)
	}

	s := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if s.Pending != 0 || s.Failed != 0 || len(s.StockErrors) != 0 {
		t.Fatalf("status after a successful push: %+v", s)
	}

	m.failOffer("A", "нет такого склада")
	setStock(t, d, id, 1)
	if got = decode[channel.PushResponse](t, do(t, h, "POST", "/push", "")); got.Failed != 1 {
		t.Fatalf("error did not reach the button: %+v", got)
	}
	s = decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if s.Failed != 1 || len(s.StockErrors) != 1 || s.StockErrors[0].Error != "нет такого склада" {
		t.Fatalf("status with an error: %+v", s)
	}
	if s.Pending != 1 {
		t.Fatalf("row in backoff must stay pending: %+v", s)
	}
}

func TestRetryAfterHeader(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"120":                           2 * time.Minute,
		"0":                             0,
		"":                              0,
		"-5":                            0,
		"Wed, 21 Oct 2015 07:28:00 GMT": 0,
	} {
		if got := retryAfter(header); got != want {
			t.Fatalf("retryAfter(%q) = %s, want %s", header, got, want)
		}
	}
}
