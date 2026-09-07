package storefront

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A buyer arrives from Google, walks the catalogue, then orders. The order must
// name Google and the page they landed on - not /cart, which is what the
// referer says by the time the form is posted.
func TestSourceSurvivesTheWalkToCheckout(t *testing.T) {
	h := (&Storefront{}).TrackSource(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {}))

	land := "/c/interer/kovriki-i-dorozhki"
	rec := httptest.NewRecorder()
	first := httptest.NewRequest(http.MethodGet, land, nil)
	first.Host = "shop.example"
	first.Header.Set("Referer", "https://www.google.com/")
	h.ServeHTTP(rec, first)

	cookie := rec.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != kSourceCookie {
		t.Fatalf("first touch set no source cookie: %v", cookie)
	}

	// Three hops inside the shop, each refering to our own host. None of them
	// may overwrite the first touch.
	for _, path := range []string{"/p/dorozhka-pvh", "/cart", "/cart"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Host = "shop.example"
		r.Header.Set("Referer", "https://shop.example/whatever")
		r.AddCookie(cookie[0])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if got := w.Result().Cookies(); len(got) != 0 {
			t.Fatalf("%s rewrote the source cookie: %v", path, got)
		}
	}

	order := httptest.NewRequest(http.MethodPost, "/cart/order", nil)
	order.Host = "shop.example"
	order.Header.Set("Referer", "https://shop.example/cart")
	order.AddCookie(cookie[0])

	got := sourceOf(order)
	if !strings.HasPrefix(got, "www.google.com ") {
		t.Errorf("source lost the referrer: %q", got)
	}
	if !strings.Contains(got, land) {
		t.Errorf("source lost the landing page: %q", got)
	}
	if strings.Contains(got, "/cart") {
		t.Errorf("source took the checkout page for the landing one: %q", got)
	}
}

func TestSourceDirectAndUTM(t *testing.T) {
	h := (&Storefront{}).TrackSource(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {}))

	// No referer and no utm: nothing to record, and the order reads "direct".
	rec := httptest.NewRecorder()
	bare := httptest.NewRequest(http.MethodGet, "/", nil)
	bare.Host = "shop.example"
	h.ServeHTTP(rec, bare)
	if c := rec.Result().Cookies(); len(c) != 0 {
		t.Errorf("a direct visit wrote a cookie: %v", c)
	}
	if got := sourceOf(bare); got != "direct" {
		t.Errorf("direct visit read as %q", got)
	}

	// utm alone is enough, even without a referer.
	rec = httptest.NewRecorder()
	utm := httptest.NewRequest(http.MethodGet, "/?utm_source=telegram&utm_medium=post", nil)
	utm.Host = "shop.example"
	h.ServeHTTP(rec, utm)
	c := rec.Result().Cookies()
	if len(c) != 1 {
		t.Fatalf("utm visit set no cookie: %v", c)
	}
	next := httptest.NewRequest(http.MethodPost, "/cart/order", nil)
	next.AddCookie(c[0])
	if got := sourceOf(next); !strings.Contains(got, "source=telegram") {
		t.Errorf("utm lost: %q", got)
	}
}

// Our own pages are not a source: a referer from the shop itself is ignored, or
// every internal click would look like a fresh arrival.
func TestOwnHostIsNotASource(t *testing.T) {
	if got := externalHost("https://shop.example/c/x", "shop.example"); got != "" {
		t.Errorf("own host taken as source: %q", got)
	}
	if got := externalHost("https://www.google.com/", "shop.example"); got != "www.google.com" {
		t.Errorf("external host lost: %q", got)
	}
	if got := externalHost("", "shop.example"); got != "" {
		t.Errorf("empty referer produced %q", got)
	}
}
