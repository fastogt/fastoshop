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

	// api_key = nil — leave the key alone, change the rest.
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

func TestCheckAndLinkRefuseWithoutCredentials(t *testing.T) {
	h, _ := newTestHandlers(t)
	for _, path := range []string{"/check", "/link"} {
		w := do(t, h, "POST", path, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s without creds: %d %s", path, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Client-Id") {
			t.Fatalf("%s: message must be human-readable: %s", path, w.Body.String())
		}
	}
}

func TestLinkMatchesBySKU(t *testing.T) {
	h, d := newTestHandlers(t)
	srv := mockCabinet(t, "A", "B", "C")
	h.BaseURL = srv.URL
	do(t, h, "PUT", "/settings", `{"client_id":"cid","api_key":"key"}`)

	for _, sku := range []string{"A", "B", "D"} {
		if err := d.CreateProduct(&database.Product{SKU: sku, Title: "Товар " + sku}); err != nil {
			t.Fatal(err)
		}
	}

	if n := decode[checkResponse](t, do(t, h, "POST", "/check", "")).Total; n != 3 {
		t.Fatalf("check before link: %d", n)
	}

	got := decode[linkResponse](t, do(t, h, "POST", "/link", ""))
	if got.Linked != 2 {
		t.Fatalf("linked: %+v", got)
	}
	if len(got.UnlinkedProducts) != 1 || got.UnlinkedProducts[0].SKU != "D" {
		t.Fatalf("unlinked products: %+v", got.UnlinkedProducts)
	}
	if len(got.UnlinkedOffers) != 1 || got.UnlinkedOffers[0] != "C" {
		t.Fatalf("unlinked offers: %+v", got.UnlinkedOffers)
	}

	links, err := d.ListOzonLinksPage(1000, 0)
	if err != nil || len(links) != 2 {
		t.Fatalf("links: %v %+v", err, links)
	}

	// Relinking is idempotent: the same two rows, no duplicates.
	got = decode[linkResponse](t, do(t, h, "POST", "/link", ""))
	links, _ = d.ListOzonLinksPage(1000, 0)
	if got.Linked != 2 || len(links) != 2 {
		t.Fatalf("relink: %+v %+v", got, links)
	}

	// The tab's counters: 2 linked, 1 not.
	s := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if s.Linked != 2 || s.Unlinked != 1 {
		t.Fatalf("counts: %+v", s)
	}
}

// v2 returns warehouses without the "result" envelope and in cursor pages —
// on a live cabinet v1 answers "obsolete method". We check both pages and that
// a repeated cursor on the last page does not loop forever.
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
