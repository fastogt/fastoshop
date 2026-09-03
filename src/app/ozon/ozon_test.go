package ozon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// mockCabinet serves a cabinet with the listed SKUs and requires our keys.
func mockCabinet(t *testing.T, offers ...string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/product/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Client-Id") != "cid" || r.Header.Get("Api-Key") != "key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
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
	return srv
}

func newTestHandlers(t *testing.T) (*Handlers, *database.Database) {
	t.Helper()
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	// A shop always has settings by the time a channel tab is opened, and its
	// currency is what every price here is written in.
	if err := d.CreateSettings(&database.Settings{
		ShopName: "лавка", Currency: database.ShopCurrencyRUB}); err != nil {
		t.Fatal(err)
	}
	return NewHandlers(d, NewWorker(d)), d
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

func TestSettingsRoundTrip(t *testing.T) {
	h, _ := newTestHandlers(t)

	// A fresh database: an empty form, not a 500.
	got := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if got.ClientID != "" || got.APIKeySet {
		t.Fatalf("fresh settings: %+v", got)
	}

	w := do(t, h, "PUT", "/settings",
		`{"enabled":true,"client_id":"cid","api_key":"key","warehouse_id":"wh-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), `"key"`) {
		t.Fatalf("api_key leaked into the response: %s", w.Body.String())
	}
	got = decode[settingsResponse](t, w)
	if !got.APIKeySet || !got.Enabled || got.ClientID != "cid" || got.WarehouseID != "wh-1" {
		t.Fatalf("saved: %+v", got)
	}

	// api_key = nil - leave the key alone, change the rest.
	got = decode[settingsResponse](t, do(t, h, "PUT", "/settings",
		`{"enabled":false,"client_id":"cid2","warehouse_id":"wh-2"}`))
	if !got.APIKeySet || got.Enabled || got.ClientID != "cid2" {
		t.Fatalf("nil api_key must keep old: %+v", got)
	}

	// An explicit empty string is a reset.
	got = decode[settingsResponse](t, do(t, h, "PUT", "/settings",
		`{"client_id":"cid2","api_key":""}`))
	if got.APIKeySet {
		t.Fatalf("empty api_key must clear: %+v", got)
	}
}

func TestCheckCountsCabinet(t *testing.T) {
	h, _ := newTestHandlers(t)
	srv := mockCabinet(t, "A", "B", "C")
	h.BaseURL = srv.URL

	do(t, h, "PUT", "/settings", `{"client_id":"cid","api_key":"key"}`)
	w := do(t, h, "POST", "/check", "")
	if w.Code != http.StatusOK {
		t.Fatalf("check: %d %s", w.Code, w.Body.String())
	}
	if got := decode[checkResponse](t, w); got.Total != 3 {
		t.Fatalf("total: %+v", got)
	}
}

func TestCheckAndPublishRefuseWithoutCredentials(t *testing.T) {
	h, _ := newTestHandlers(t)
	// A body that is not empty: publishing refuses a selection of nothing before
	// it ever looks at the keys, and that is not the refusal under test here.
	for _, c := range []struct{ path, body string }{
		{"/check", ""},
		{"/publish", `{"product_ids":[1]}`},
	} {
		path := c.path
		w := do(t, h, "POST", path, c.body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s without creds: %d %s", path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Client-Id") {
			t.Fatalf("%s: message must be human-readable: %s", path, w.Body.String())
		}
	}
}

func TestPublishMatchesBySKU(t *testing.T) {
	h, d := newTestHandlers(t)
	srv := mockCabinet(t, "A", "B", "C")
	h.BaseURL = srv.URL
	do(t, h, "PUT", "/settings", `{"client_id":"cid","api_key":"key"}`)

	var ids []int64
	for _, sku := range []string{"A", "B", "D"} {
		p := &database.Product{SKU: sku, Title: "Товар " + sku}
		if err := d.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}

	if n := decode[checkResponse](t, do(t, h, "POST", "/check", "")).Total; n != 3 {
		t.Fatalf("check before publish: %d", n)
	}

	body, _ := json.Marshal(publishRequest{ProductIDs: ids})
	got := decode[publishResponse](t, do(t, h, "POST", "/publish", string(body)))
	if got.Published != 2 {
		t.Fatalf("published: %+v", got)
	}
	if len(got.NoCard) != 1 || got.NoCard[0].SKU != "D" {
		t.Fatalf("the article with no card must be reported: %+v", got.NoCard)
	}

	links, err := d.ListOzonLinksPage(1000, 0)
	if err != nil || len(links) != 2 {
		t.Fatalf("links: %v %+v", err, links)
	}

	// Publishing the same selection twice is idempotent: no duplicate rows.
	got = decode[publishResponse](t, do(t, h, "POST", "/publish", string(body)))
	links, _ = d.ListOzonLinksPage(1000, 0)
	if got.Published != 2 || len(links) != 2 {
		t.Fatalf("republish: %+v %+v", got, links)
	}

	// The card nothing points at is the tab's orphan list, not a silent drop.
	cab := decode[cabinetResponse](t, do(t, h, "GET", "/cabinet", ""))
	if cab.Orphans != 1 || len(cab.OrphanSKUs) != 1 || cab.OrphanSKUs[0] != "C" {
		t.Fatalf("orphan card: %+v", cab)
	}

	// The tab's counters: 2 linked, 1 not.
	s := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if s.Linked != 2 || s.Unlinked != 1 {
		t.Fatalf("counts: %+v", s)
	}
}

func TestListWarehousesPaginates(t *testing.T) {
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/warehouse/list", func(w http.ResponseWriter, r *http.Request) {
		var req warehouseListRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Limit != kWarehousePageSize {
			t.Errorf("limit %d", req.Limit)
		}
		calls++
		if req.Cursor == "" {
			_ = json.NewEncoder(w).Encode(warehouseListResponse{
				Warehouses: []Warehouse{{ID: 1, Name: "Минск"}},
				HasNext:    true, Cursor: "next",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(warehouseListResponse{
			Warehouses: []Warehouse{{ID: 2, Name: "Москва"}},
			HasNext:    false, Cursor: "next",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &Client{ClientID: "cid", APIKey: "key", BaseURL: srv.URL}
	list, err := c.ListWarehouses()
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(list) != 2 || list[1].Name != "Москва" {
		t.Fatalf("calls=%d list=%+v", calls, list)
	}
}
