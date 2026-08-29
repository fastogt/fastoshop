package storefront

import (
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/media"
	"time"
)

var kLDJSON = regexp.MustCompile(`(?s)<script type="application/ld\+json">.*?</script>`)

func setup(t *testing.T) (*database.Database, http.Handler) {
	t.Helper()
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_ = d.CreateSettings(&database.Settings{OwnerEmail: "a@b.c", PasswordHash: "h", ShopName: "Лавка"})
	p := &database.Product{Title: "Красный чайник", Description: "Хорош",
		Price: 250000, Stock: 3, Category: "kitchen"}
	_ = d.CreateProduct(p)
	sf := New(d, "https://shop.example.com", t.TempDir())
	return d, sf.Router()
}

func get(t *testing.T, h http.Handler, path string) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", path, w.Code)
	}
	return w.Body.String()
}

// The zero-JS storefront is a promise, so an unconfigured shop must carry no
// counter at all; a configured one must carry ids the providers can actually
// read back — html/template escapes inside <script>, and a mangled id is a
// counter that silently collects nothing.
func TestCounters(t *testing.T) {
	d, h := setup(t)
	var body string
	for _, path := range []string{"/", "/p/krasnyj-chajnik", "/cart"} {
		// JSON-LD is data, not code, and lives on the product card by design.
		if strings.Contains(kLDJSON.ReplaceAllString(get(t, h, path), ""), "<script") {
			t.Fatalf("%s serves a script without counters configured", path)
		}
	}
	s, _ := d.GetSettings()
	s.GAMeasurementID = "G-ABC123"
	s.MetrikaCounterID = "12345678"
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	// Every page the buyer can land on, not just the catalogue: a product card
	// is the page search engines send traffic to, and a counter missing there
	// undercounts exactly the visits worth measuring.
	for _, path := range []string{"/", "/p/krasnyj-chajnik", "/cart"} {
		body = get(t, h, path)
		for _, want := range []string{
			"gtag/js?id=G-ABC123",
			"gtag('config','G-ABC123')",
			"ym('12345678','init'",
			"mc.yandex.ru/watch/12345678",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
	}
}

func TestCatalogPage(t *testing.T) {
	_, h := setup(t)
	body := get(t, h, "/")
	for _, want := range []string{"Лавка", "Красный чайник", "/p/krasnyj-chajnik", "2500"} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
}

func TestProductPageSEO(t *testing.T) {
	_, h := setup(t)
	body := get(t, h, "/p/krasnyj-chajnik")
	for _, want := range []string{
		"<title>Красный чайник",
		`name="description"`,
		`property="og:title"`,
		`rel="canonical" href="https://shop.example.com/p/krasnyj-chajnik"`,
		`"@type": "Product"`,
		`"price": "2500.00"`,
		`schema.org/InStock`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("product page missing %q", want)
		}
	}
}

func TestJSONLDValidWithHostileTitle(t *testing.T) {
	d, h := setup(t)
	p := &database.Product{Title: `Чайник "Гром" <script>&`, Description: `1" > 2`,
		Price: 100, Stock: 1}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	body := get(t, h, "/p/"+p.Slug)
	_, rest, ok := strings.Cut(body, `<script type="application/ld+json">`)
	if !ok {
		t.Fatal("no ld+json block")
	}
	raw, _, _ := strings.Cut(rest, "</script>")
	if strings.Contains(raw, "<script>") {
		t.Errorf("unescaped markup in ld+json: %s", raw)
	}
	var ld struct {
		Type string `json:"@type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(raw), &ld); err != nil {
		t.Fatalf("ld+json invalid: %v\n%s", err, raw)
	}
	if ld.Type != "Product" || ld.Name != p.Title {
		t.Errorf("ld+json: %+v", ld)
	}
}

// ldImage extracts image from the JSON-LD. We compare the parsed value, not a
// substring: in a JS context html/template escapes slashes (\/), which is valid
// JSON but does not match the original string.
func ldImage(t *testing.T, body string) string {
	t.Helper()
	_, rest, ok := strings.Cut(body, `<script type="application/ld+json">`)
	if !ok {
		t.Fatal("no ld+json block")
	}
	raw, _, _ := strings.Cut(rest, "</script>")
	var ld struct {
		Image string `json:"image"`
	}
	if err := json.Unmarshal([]byte(raw), &ld); err != nil {
		t.Fatalf("ld+json invalid: %v\n%s", err, raw)
	}
	return ld.Image
}

// Import no longer downloads photos — product_images.path holds the absolute
// source URL. It must reach <img>, og:image and JSON-LD as is.
func TestRemoteImageURLRenderedAsIs(t *testing.T) {
	d, h := setup(t)
	p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik")
	const remote = "https://cdn.example/x.jpg"
	if err := d.AddImage(p.ID, remote); err != nil {
		t.Fatal(err)
	}
	body := get(t, h, "/p/krasnyj-chajnik")
	for _, want := range []string{
		`src="https://cdn.example/x.jpg"`,
		`property="og:image" content="https://cdn.example/x.jpg"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("product page missing %q\n%s", want, body)
		}
	}
	if got := ldImage(t, body); got != remote {
		t.Errorf("ld+json image = %q, want %q", got, remote)
	}
	if strings.Contains(body, "/uploads/https") {
		t.Error("remote URL must not be prefixed with /uploads/")
	}
	if !strings.Contains(get(t, h, "/"), `src="https://cdn.example/x.jpg"`) {
		t.Error("catalog card must use the remote URL")
	}
}

// Local uploads from the admin keep living under /uploads/.
func TestLocalImageStillUnderUploads(t *testing.T) {
	d, h := setup(t)
	p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik")
	if err := d.AddImage(p.ID, "p1-abc.jpg"); err != nil {
		t.Fatal(err)
	}
	body := get(t, h, "/p/krasnyj-chajnik")
	for _, want := range []string{
		`src="/uploads/p1-abc.jpg"`,
		`property="og:image" content="https://shop.example.com/uploads/p1-abc.jpg"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("product page missing %q\n%s", want, body)
		}
	}
	if got, want := ldImage(t, body), "https://shop.example.com/uploads/p1-abc.jpg"; got != want {
		t.Errorf("ld+json image = %q, want %q", got, want)
	}
}

func seedCatalog(t *testing.T, d *database.Database, n int) {
	t.Helper()
	for i := range n {
		p := &database.Product{Title: fmt.Sprintf("Товар %d", i), Price: 100, Stock: 1}
		if err := d.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCatalogPagination(t *testing.T) {
	d, h := setup(t)
	seedCatalog(t, d, 130) // +1 from setup = 131 products, three pages of 60

	first := get(t, h, "/")
	if n := strings.Count(first, `<li><a href="/p/`); n != kCatalogPageSize {
		t.Fatalf("page 1 must hold %d cards, got %d", kCatalogPageSize, n)
	}
	for _, want := range []string{
		`rel="canonical" href="https://shop.example.com/"`,
		`href="/?page=2"`, "Страница 1 из 3",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("page 1 missing %q", want)
		}
	}
	if strings.Contains(first, "<script") {
		t.Error("storefront must stay JS-free")
	}

	second := get(t, h, "/?page=2")
	for _, want := range []string{
		`rel="canonical" href="https://shop.example.com/?page=2"`,
		`href="/"`, `href="/?page=3"`, "Страница 2 из 3",
	} {
		if !strings.Contains(second, want) {
			t.Errorf("page 2 missing %q", want)
		}
	}
	// The third page is the catalogue tail, no "Next" anymore.
	last := get(t, h, "/?page=3")
	if strings.Contains(last, `href="/?page=4"`) {
		t.Error("last page must not link forward")
	}
	if n := strings.Count(last, `<li><a href="/p/`); n != 11 {
		t.Errorf("last page cards: %d", n)
	}
}

func TestCatalogPageEdgeCases(t *testing.T) {
	_, h := setup(t)
	code := func(path string) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w.Code
	}
	// Past the last page is a 404, not an empty 200: soft 404s clutter the index.
	if c := code("/?page=99999"); c != http.StatusNotFound {
		t.Errorf("page beyond last: %d, want 404", c)
	}
	if c := code("/?page=abc"); c != http.StatusNotFound {
		t.Errorf("non-numeric page: %d, want 404", c)
	}
	if c := code("/?page=0"); c != http.StatusNotFound {
		t.Errorf("page=0: %d, want 404", c)
	}
	// One set of products has exactly one URL.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/?page=1", nil))
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/" {
		t.Errorf("page=1 must 301 to /: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestOutOfStockAvailability(t *testing.T) {
	d, h := setup(t)
	p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik")
	p.Stock = 0
	_ = d.UpdateProduct(p)
	body := get(t, h, "/p/krasnyj-chajnik")
	if !strings.Contains(body, "schema.org/OutOfStock") {
		t.Error("must show OutOfStock")
	}
}

func TestSitemapRobots404(t *testing.T) {
	_, h := setup(t)
	sm := get(t, h, "/sitemap.xml")
	if !strings.Contains(sm, "https://shop.example.com/p/krasnyj-chajnik") {
		t.Errorf("sitemap: %s", sm)
	}
	rb := get(t, h, "/robots.txt")
	if !strings.Contains(rb, "Disallow: /admin") || !strings.Contains(rb, "Sitemap:") {
		t.Errorf("robots: %s", rb)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/p/net-takogo", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("missing product: %d", w.Code)
	}
}

// llms.txt is the shop's map for AI assistants: name, how ordering works, the
// catalogue sections. Answer engines already send buyers, and they decide from
// one read, not from twenty thousand pages.
func TestLlmsTxt(t *testing.T) {
	_, h := setup(t)
	body := get(t, h, "/llms.txt")
	for _, want := range []string{"# Лавка", "Оплата при получении",
		"## Каталог", "https://shop.example.com/sitemap.xml", "schema.org/Product"} {
		if !strings.Contains(body, want) {
			t.Errorf("llms.txt lacks %q:\n%s", want, body)
		}
	}
}

// client is a minimal cookie jar: it carries `cart` between requests, like a browser.
type client struct {
	h      http.Handler
	cookie *http.Cookie
}

func (c *client) do(t *testing.T, method, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if form == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	for _, ck := range w.Result().Cookies() {
		if ck.Name != "cart" {
			continue
		}
		if ck.MaxAge < 0 {
			c.cookie = nil
		} else {
			c.cookie = ck
		}
	}
	return w
}

func (c *client) add(t *testing.T, slug, qty string) {
	t.Helper()
	w := c.do(t, "POST", "/cart/add", url.Values{"slug": {slug}, "qty": {qty}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("add %s: %d %s", slug, w.Code, w.Body.String())
	}
}

func (c *client) cart(t *testing.T) string {
	t.Helper()
	w := c.do(t, "GET", "/cart", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /cart: %d", w.Code)
	}
	return w.Body.String()
}

func secondProduct(t *testing.T, d *database.Database) *database.Product {
	t.Helper()
	p := &database.Product{Title: "Синий стакан", Price: 30000, Stock: 10}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCartAddTwoProducts(t *testing.T) {
	d, h := setup(t)
	secondProduct(t, d)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "2")
	c.add(t, "sinij-stakan", "1")
	body := c.cart(t)
	for _, want := range []string{"Красный чайник", "Синий стакан", "5000.00", "300.00", "5300.00"} {
		if !strings.Contains(body, want) {
			t.Errorf("cart missing %q\n%s", want, body)
		}
	}
}

func TestCartIncrementSameProduct(t *testing.T) {
	_, h := setup(t)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	c.add(t, "krasnyj-chajnik", "1")
	body := c.cart(t)
	if n := strings.Count(body, "Красный чайник"); n != 1 {
		t.Errorf("expected one line, got %d\n%s", n, body)
	}
	if !strings.Contains(body, `value="2"`) || !strings.Contains(body, "5000.00") {
		t.Errorf("expected qty 2 / line 5000.00\n%s", body)
	}
}

func TestCartUpdateZeroRemovesLine(t *testing.T) {
	_, h := setup(t)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "2")
	w := c.do(t, "POST", "/cart/update",
		url.Values{"slug": {"krasnyj-chajnik"}, "qty": {"0"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("update: %d", w.Code)
	}
	body := c.cart(t)
	if strings.Contains(body, "Красный чайник") {
		t.Errorf("line must be gone\n%s", body)
	}
	if !strings.Contains(body, "Корзина пуста") {
		t.Errorf("expected empty-cart notice\n%s", body)
	}
}

func TestCartCheckoutCreatesSingleOrder(t *testing.T) {
	d, h := setup(t)
	secondProduct(t, d)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "2")
	c.add(t, "sinij-stakan", "3")
	w := c.do(t, "POST", "/cart/order",
		url.Values{"name": {"Иван"}, "phone": {"+79990001122"}, "comment": {"звонить утром"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("checkout: %d %s", w.Code, w.Body.String())
	}
	orders, _ := d.ListOrders()
	if len(orders) != 1 {
		t.Fatalf("expected exactly one order, got %d", len(orders))
	}
	var items []orderItemJSON
	if err := json.Unmarshal([]byte(orders[0].ItemsJSON), &items); err != nil {
		t.Fatalf("items_json: %v (%s)", err, orders[0].ItemsJSON)
	}
	if len(items) != 2 {
		t.Fatalf("items: %+v", items)
	}
	byTitle := map[string]orderItemJSON{}
	for _, it := range items {
		byTitle[it.Title] = it
	}
	if it := byTitle["Красный чайник"]; it.Price != 250000 || it.Qty != 2 {
		t.Errorf("teapot: %+v", it)
	}
	if it := byTitle["Синий стакан"]; it.Price != 30000 || it.Qty != 3 {
		t.Errorf("glass: %+v", it)
	}
	// The cookie is cleared — resubmitting does not create a second order.
	if c.cookie != nil {
		t.Errorf("cart cookie must be cleared, got %q", c.cookie.Value)
	}
	if !strings.Contains(c.cart(t), "Корзина пуста") {
		t.Error("cart must be empty after checkout")
	}
}

func TestCartTamperedCookieCannotSetPrice(t *testing.T) {
	d, h := setup(t)
	c := &client{h: h}
	c.cookie = &http.Cookie{Name: "cart", Value: url.QueryEscape(
		`[{"slug":"krasnyj-chajnik","qty":1,"price":1,"title":"Бесплатно"}]`)}
	if !strings.Contains(c.cart(t), "2500.00") {
		t.Error("cart must price from DB")
	}
	c.do(t, "POST", "/cart/order", url.Values{"name": {"И"}, "phone": {"+7"}})
	orders, _ := d.ListOrders()
	if len(orders) != 1 {
		t.Fatalf("orders: %+v", orders)
	}
	var items []orderItemJSON
	if err := json.Unmarshal([]byte(orders[0].ItemsJSON), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Price != 250000 || items[0].Title != "Красный чайник" {
		t.Errorf("tampered cookie leaked into order: %+v", items)
	}
}

func TestCartOutOfStockCannotBeAdded(t *testing.T) {
	d, h := setup(t)
	p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik")
	p.Stock = 0
	_ = d.UpdateProduct(p)
	c := &client{h: h}
	c.do(t, "POST", "/cart/add", url.Values{"slug": {"krasnyj-chajnik"}, "qty": {"1"}})
	if c.cookie != nil {
		t.Fatalf("out-of-stock product must not enter the cart: %q", c.cookie.Value)
	}
	if !strings.Contains(c.cart(t), "Корзина пуста") {
		t.Error("cart must stay empty")
	}
}

// The product ran out after it was added — the line is dropped at render time,
// what has vanished cannot be ordered.
func TestCartDropsVanishedProduct(t *testing.T) {
	d, h := setup(t)
	secondProduct(t, d)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	c.add(t, "sinij-stakan", "1")
	p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik")
	p.Stock = 0
	_ = d.UpdateProduct(p)
	body := c.cart(t)
	if strings.Contains(body, "Красный чайник") {
		t.Errorf("out-of-stock line must be dropped\n%s", body)
	}
	c.do(t, "POST", "/cart/order", url.Values{"name": {"И"}, "phone": {"+7"}})
	orders, _ := d.ListOrders()
	var items []orderItemJSON
	_ = json.Unmarshal([]byte(orders[0].ItemsJSON), &items)
	if len(items) != 1 || items[0].Title != "Синий стакан" {
		t.Errorf("vanished product ordered: %+v", items)
	}
}

func TestCartCheckoutRequiresPhoneAndItems(t *testing.T) {
	d, h := setup(t)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	w := c.do(t, "POST", "/cart/order", url.Values{"name": {"Иван"},
		"comment": {"привезите к обеду"}})
	if orders, _ := d.ListOrders(); len(orders) != 0 {
		t.Fatal("order without phone must be rejected")
	}
	// The refused form comes back filled: a phone user who loses their name
	// and comment to a validation error does not retype them — they leave.
	body := w.Body.String()
	if !strings.Contains(body, `value="Иван"`) ||
		!strings.Contains(body, "привезите к обеду") {
		t.Error("typed name and comment lost on the no-contact error")
	}
	empty := &client{h: h}
	empty.do(t, "POST", "/cart/order", url.Values{"name": {"И"}, "phone": {"+7"}})
	if orders, _ := d.ListOrders(); len(orders) != 0 {
		t.Fatal("empty cart must not create an order")
	}
}

func TestCartNoIndexNoScriptsNotInSitemap(t *testing.T) {
	_, h := setup(t)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	body := c.cart(t)
	if !strings.Contains(body, `<meta name="robots" content="noindex">`) {
		t.Errorf("cart must be noindex\n%s", body)
	}
	if strings.Contains(body, "<script") {
		t.Error("storefront must stay JS-free")
	}
	if strings.Contains(get(t, h, "/sitemap.xml"), "/cart") {
		t.Error("/cart must not be in sitemap")
	}
	if !strings.Contains(get(t, h, "/robots.txt"), "Disallow: /cart") {
		t.Error("robots must disallow /cart")
	}
}

func TestCartHeaderCountAndProductButton(t *testing.T) {
	_, h := setup(t)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "3")
	page := c.do(t, "GET", "/p/krasnyj-chajnik", nil).Body.String()
	if !strings.Contains(page, `href="/cart"`) || !strings.Contains(page, "Корзина (3)") {
		t.Errorf("header must link to cart with count\n%s", page)
	}
	if !strings.Contains(page, `action="/cart/add"`) || !strings.Contains(page, "В корзину") {
		t.Errorf("product page must post to /cart/add\n%s", page)
	}
}

// The only checkout — the old single-page /p/{slug}/order has been removed.
func TestLegacyProductOrderRouteGone(t *testing.T) {
	_, h := setup(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/p/krasnyj-chajnik/order", nil)
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Errorf("legacy order route must be gone: %d", w.Code)
	}
}

func TestCheckoutDecrementsStock(t *testing.T) {
	d, h := setup(t)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "2")
	w := c.do(t, "POST", "/cart/order", url.Values{"name": {"Иван"}, "phone": {"+7"}})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("checkout: %d", w.Code)
	}
	p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik")
	if p.Stock != 1 {
		t.Fatalf("stock %d, want 1", p.Stock)
	}
}

// Stock deduction failed at checkout: no order exists, the buyer sees the named
// reason, the line leaves the cart. Here it is triggered by a duplicated cookie
// line (two of 3 with a stock of 3) — the same path as two buyers racing for
// the last unit, but reproducible.
func TestCheckoutSoldOutRace(t *testing.T) {
	d, h := setup(t)
	second := secondProduct(t, d)
	c := &client{h: h}
	c.cookie = &http.Cookie{Name: "cart", Value: url.QueryEscape(
		`[{"slug":"krasnyj-chajnik","qty":3},{"slug":"krasnyj-chajnik","qty":3},{"slug":"sinij-stakan","qty":1}]`)}

	w := c.do(t, "POST", "/cart/order", url.Values{"name": {"Иван"}, "phone": {"+7"}})
	if w.Code != http.StatusOK {
		t.Fatalf("sold-out checkout must re-render the cart: %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "«Красный чайник» закончился") {
		t.Errorf("notice must name the product\n%s", body)
	}
	if strings.Contains(body, "<script") {
		t.Error("storefront must stay JS-free")
	}
	if orders, _ := d.ListOrders(); len(orders) != 0 {
		t.Fatalf("failed checkout must not create an order: %+v", orders)
	}
	if p, _ := d.GetProduct(second.ID); p.Stock != 10 {
		t.Errorf("second product stock moved: %d", p.Stock)
	}
	if p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik"); p.Stock != 3 {
		t.Errorf("stock must be untouched: %d", p.Stock)
	}
	// The line is removed from the cart, the rest stays in place.
	cart := c.cart(t)
	if strings.Contains(cart, "Красный чайник") || !strings.Contains(cart, "Синий стакан") {
		t.Errorf("cart must keep only what is available\n%s", cart)
	}
}

// A Belarusian shop's storefront must not show Russian rubles: the buyer sees
// the label, and priceCurrency ends up in the search engine results.
func TestStorefrontCurrencyBYN(t *testing.T) {
	d, h := setup(t)
	if err := d.UpdateSettings(&database.Settings{
		OwnerEmail: "a@b.c", PasswordHash: "h", ShopName: "Лавка",
		Currency: database.ShopCurrencyBYN}); err != nil {
		t.Fatal(err)
	}
	p := &database.Product{Title: "Блокнот", Price: 2500, Stock: 3}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}

	body := get(t, h, "/p/"+p.Slug)
	if !strings.Contains(body, "25.00 Br") {
		t.Error("product page does not show the price in BYN")
	}
	if !strings.Contains(body, `"priceCurrency": "BYN"`) {
		t.Error("JSON-LD carries the wrong currency")
	}
	if strings.Contains(body, "₽") {
		t.Error("ruble sign still on the storefront")
	}
}

// A hidden product must vanish from everywhere at once. Especially from the
// sitemap: a map leading to 404s hurts the search engine's trust in the whole
// site.
func TestHiddenProductLeavesStorefront(t *testing.T) {
	d, h := setup(t)
	p := &database.Product{Title: "Тайный чайник", Price: 1000, Stock: 5}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	if body := get(t, h, "/"); !strings.Contains(body, "Тайный чайник") {
		t.Fatal("product did not appear on the storefront before hiding")
	}

	p.Hidden = true
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}

	if body := get(t, h, "/"); strings.Contains(body, "Тайный чайник") {
		t.Error("hidden product still in the catalog")
	}
	if body := get(t, h, "/sitemap.xml"); strings.Contains(body, p.Slug) {
		t.Error("hidden product still in the sitemap")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/p/"+p.Slug, nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("hidden product page: %d, want 404", w.Code)
	}

	// And it cannot be ordered around the storefront by knowing the slug.
	form := strings.NewReader("slug=" + p.Slug + "&qty=1")
	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/cart/add", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(w, r)
	if len(w.Result().Cookies()) > 0 && strings.Contains(w.Result().Cookies()[0].Value, p.Slug) {
		t.Error("hidden product got into the cart via a direct link")
	}
}

// HEAD must behave like GET: monitoring, link checkers and some crawlers use it
// to probe availability, and a 405 on the storefront looks like a broken site.
func TestHeadIsAllowedEverywhere(t *testing.T) {
	d, h := setup(t)
	p := &database.Product{Title: "Чайник HEAD", Price: 1000, Stock: 1}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/sitemap.xml", "/yml.xml", "/gmc.xml", "/robots.txt", "/p/" + p.Slug, "/cart"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("HEAD", path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("HEAD %s: %d", path, w.Code)
		}
	}
}

// The tab icon comes from the shop name: the storefront belongs to the seller,
// and putting our mark there would brand somebody else's shop.
func TestFaviconUsesShopInitial(t *testing.T) {
	d, h := setup(t)
	if err := d.UpdateSettings(&database.Settings{
		OwnerEmail: "a@b.c", PasswordHash: "h", ShopName: "Ромашка",
		Currency: database.ShopCurrencyRUB}); err != nil {
		t.Fatal(err)
	}
	body := get(t, h, "/favicon.svg")
	if !strings.Contains(body, ">Р<") {
		t.Errorf("shop initial missing from the icon: %s", body)
	}
	if !strings.Contains(body, "<svg") {
		t.Errorf("not an svg: %s", body)
	}
	// And the page links to it, otherwise the browser keeps asking for favicon.ico.
	if page := get(t, h, "/"); !strings.Contains(page, `href="/favicon.svg"`) {
		t.Error("page does not link to the icon")
	}
}

// The seller's logo replaces the text name in the header and becomes the tab
// icon. The name does not disappear though: it moves into alt, otherwise the
// home page loses its textual signal of whose shop this is.
func TestShopLogoReplacesName(t *testing.T) {
	d, h := setup(t)
	base := &database.Settings{OwnerEmail: "a@b.c", PasswordHash: "h",
		ShopName: "Лавка Ивана", Currency: database.ShopCurrencyRUB}
	if err := d.UpdateSettings(base); err != nil {
		t.Fatal(err)
	}
	// Look for the tag itself, not the class: the class also appears in the inline CSS.
	if page := get(t, h, "/"); !strings.Contains(page, "Лавка Ивана") ||
		strings.Contains(page, `<img src="/uploads/`) {
		t.Fatal("without a logo the header must show the name as text")
	}

	base.Logo = "logo-abc.png"
	if err := d.UpdateSettings(base); err != nil {
		t.Fatal(err)
	}
	page := get(t, h, "/")
	if !strings.Contains(page, `src="/uploads/logo-abc.png"`) {
		t.Error("logo missing from the header")
	}
	if !strings.Contains(page, `alt="Лавка Ивана"`) {
		t.Error("name not preserved in alt")
	}
	if !strings.Contains(page, `<link rel="icon" href="/uploads/logo-abc.png">`) {
		t.Error("logo did not become the tab icon")
	}
}

// The buyer's search: a plain GET form, because the storefront carries no
// JavaScript to search with.
func TestStorefrontSearch(t *testing.T) {
	d, h := setup(t)
	_ = d.CreateProduct(&database.Product{Title: "Синяя кастрюля", Price: 100000,
		Stock: 1, SKU: "KS-7"})
	hidden := &database.Product{Title: "Чайник со склада", Price: 100, Hidden: true}
	_ = d.CreateProduct(hidden)

	body := get(t, h, "/?q="+url.QueryEscape("кастрюл"))
	if !strings.Contains(body, "Синяя кастрюля") || strings.Contains(body, "Красный чайник") {
		t.Fatalf("search did not filter: %s", body)
	}
	// Search results are thin and endless: out of the index, and no canonical
	// pointing at them.
	if !strings.Contains(body, `name="robots" content="noindex,follow"`) {
		t.Error("search results must be noindex")
	}
	if strings.Contains(body, "rel=\"canonical\"") {
		t.Error("search results must not claim a canonical URL")
	}

	// The article is searchable too — buyers paste those from a price list.
	if !strings.Contains(get(t, h, "/?q=KS-7"), "Синяя кастрюля") {
		t.Error("search by article found nothing")
	}
	// A hidden product stays hidden, search or no search.
	if strings.Contains(get(t, h, "/?q="+url.QueryEscape("склада")), "Чайник со склада") {
		t.Error("search exposed a hidden product")
	}
	// Nothing found is a page with a way out, not an empty catalogue.
	if !strings.Contains(get(t, h, "/?q="+url.QueryEscape("телевизор")), "ничего не нашлось") {
		t.Error("no empty-result message")
	}
	// The catalogue itself is still indexable.
	if strings.Contains(get(t, h, "/"), "noindex") {
		t.Error("the catalogue must stay in the index")
	}

	// Leafing through results must not drop the query and dump the buyer back
	// into the full catalogue.
	seedCatalog(t, d, 130)
	paged := get(t, h, "/?q="+url.QueryEscape("Товар"))
	if !strings.Contains(paged, "page=2&amp;q=%D0%A2%D0%BE%D0%B2%D0%B0%D1%80") {
		t.Errorf("next page lost the query: %s", paged)
	}
}

// A result page must say how many it found: "Поиск: кружка" alone leaves the
// buyer guessing whether that is everything or the first screen of hundreds.
func TestSearchResultCount(t *testing.T) {
	d, h := setup(t)
	for _, n := range []string{"Кружка синяя", "Кружка белая"} {
		_ = d.CreateProduct(&database.Product{Title: n, Price: 100, Stock: 1})
	}
	body := get(t, h, "/?q="+url.QueryEscape("Кружка"))
	for _, want := range []string{"нашлось 2 товара", `href="/"`} {
		if !strings.Contains(body, want) {
			t.Errorf("result page missing %q", want)
		}
	}
	if got := foundStr(1); got != "нашлось 1 товар" {
		t.Errorf("one: %q", got)
	}
	if got := foundStr(5); got != "нашлось 5 товаров" {
		t.Errorf("five: %q", got)
	}
	if got := foundStr(11); got != "нашлось 11 товаров" {
		t.Errorf("eleven: %q", got)
	}
	if got := foundStr(21); got != "нашлось 21 товар" {
		t.Errorf("twenty one: %q", got)
	}
}

// The product page has to say where the buyer is and let them step back into
// the category — and tell a search engine the same thing.
func TestProductBreadcrumbsAndGallery(t *testing.T) {
	d, h := setup(t)
	p, err := d.GetVisibleProductBySlug("krasnyj-chajnik")
	if err != nil {
		t.Fatal(err)
	}
	_ = d.AddImage(p.ID, "https://cdn.example/1.jpg")
	_ = d.AddImage(p.ID, "https://cdn.example/2.jpg")

	body := get(t, h, "/p/krasnyj-chajnik")
	for _, want := range []string{
		`class="crumbs"`, `href="/c/kitchen"`, `"@type": "BreadcrumbList"`,
		`id="photo-0"`, `href="#photo-1"`, `class="thumbs"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("product page missing %q", want)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Error("storefront must stay JS-free")
	}
}

// A single photo needs no thumbnail strip: one thumbnail under one picture is
// furniture, not navigation.
func TestSinglePhotoHasNoThumbs(t *testing.T) {
	d, h := setup(t)
	p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik")
	_ = d.AddImage(p.ID, "https://cdn.example/1.jpg")
	if strings.Contains(get(t, h, "/p/krasnyj-chajnik"), `class="thumbs"`) {
		t.Error("one photo must not get a thumbnail strip")
	}
}

// The catalogue grid must not pull full-size supplier photos: sixty of them on
// one page is what makes a real shop slow.
func TestCatalogUsesThumbnails(t *testing.T) {
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_ = d.CreateSettings(&database.Settings{OwnerEmail: "a@b.c", ShopName: "Лавка"})
	p := &database.Product{Title: "Чайник", Price: 1000, Stock: 1}
	_ = d.CreateProduct(p)

	uploads := t.TempDir()
	// A photo big enough to be worth shrinking, plus its small copy.
	img := image.NewRGBA(image.Rect(0, 0, 1000, 800))
	f, _ := os.Create(filepath.Join(uploads, "p1-abc.jpg"))
	_ = jpeg.Encode(f, img, nil)
	_ = f.Close()
	if err := media.MakeThumb(uploads, "p1-abc.jpg"); err != nil {
		t.Fatal(err)
	}
	_ = d.AddImage(p.ID, "p1-abc.jpg")

	h := New(d, "https://shop.example.com", uploads).Router()
	if body := get(t, h, "/"); !strings.Contains(body, "/uploads/p1-abc.t.jpg") {
		t.Error("catalogue is serving the full-size photo")
	}
	// The product page shows the real thing — that is where the buyer looks.
	body := get(t, h, "/p/chajnik")
	if !strings.Contains(body, `src="/uploads/p1-abc.jpg"`) {
		t.Error("product page must show the original")
	}
}

// A photo from before thumbnails existed still has to appear, just heavier.
func TestCatalogFallsBackToOriginal(t *testing.T) {
	d, _ := database.OpenInMemory()
	defer func() { _ = d.Close() }()
	_ = d.CreateSettings(&database.Settings{OwnerEmail: "a@b.c", ShopName: "Лавка"})
	p := &database.Product{Title: "Чайник", Price: 1000, Stock: 1}
	_ = d.CreateProduct(p)
	_ = d.AddImage(p.ID, "p1-old.jpg")

	h := New(d, "https://shop.example.com", t.TempDir()).Router()
	if body := get(t, h, "/"); !strings.Contains(body, "/uploads/p1-old.jpg") {
		t.Error("a photo without a thumbnail disappeared from the grid")
	}
}

// The shop language is the seller's choice, and <html lang> has to follow it:
// an English shop declaring Russian tells every crawler the wrong thing.
func TestHTMLLang(t *testing.T) {
	d, h := setup(t)
	for _, lang := range []string{"ru", "en"} {
		s, _ := d.GetSettings()
		s.Lang = lang
		if err := d.UpdateSettings(s); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{"/", "/p/krasnyj-chajnik", "/cart"} {
			if want := `<html lang="` + lang + `">`; !strings.Contains(get(t, h, path), want) {
				t.Errorf("%s (lang=%s) missing %s", path, lang, want)
			}
		}
	}
}

// Requisites are what a buyer checks before paying a stranger, and what the law
// requires a shop in Russia or Belarus to publish. Empty means the markup stays
// clean: an Organization carrying only a name is noise, not data.
func TestRequisites(t *testing.T) {
	d, h := setup(t)

	for _, path := range []string{"/", "/p/krasnyj-chajnik", "/cart"} {
		// Match the markup and the block, not the word: the .requisites class
		// lives in the inline CSS on every page and would always match.
		if body := get(t, h, path); strings.Contains(body, `"Organization"`) ||
			strings.Contains(body, `class="requisites"`) {
			t.Errorf("%s carries an Organization with no details filled in", path)
		}
	}

	const details = "ООО «Лавка»\nМинск, ул. Мира, 1\nУНП 123456789"
	s, _ := d.GetSettings()
	s.Requisites = details
	s.ShopPhone = "+375291234567"
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/", "/p/krasnyj-chajnik", "/cart"} {
		body := get(t, h, path)
		// Specifically the footer block, not the text anywhere: the same
		// requisites live in the JSON-LD, and a substring search would pass
		// with an empty footer.
		_, after, ok := strings.Cut(body, `<div class="requisites">`)
		if !ok {
			t.Fatalf("%s: no requisites block in the footer", path)
		}
		if block, _, _ := strings.Cut(after, "</div>"); !strings.Contains(block, "УНП 123456789") {
			t.Errorf("%s: footer block is missing the details: %q", path, block)
		}
		blocks := kLDJSON.FindAllString(body, -1)
		var org map[string]any
		for _, b := range blocks {
			raw := strings.TrimSuffix(strings.SplitN(b, ">", 2)[1], "</script>")
			var m map[string]any
			if err := json.Unmarshal([]byte(raw), &m); err != nil {
				t.Fatalf("%s: ld+json invalid: %v\n%s", path, err, raw)
			}
			if m["@type"] == "Organization" {
				org = m
			}
		}
		if org == nil {
			t.Fatalf("%s: no Organization block", path)
		}
		if org["description"] != details {
			t.Errorf("%s: description = %q", path, org["description"])
		}
		if org["telephone"] != "+375291234567" {
			t.Errorf("%s: telephone = %q", path, org["telephone"])
		}
	}
}

// Every picture sits in a frame that carries the shop's "no photo" mark behind
// it. A product without photos gets the mark instead of a hole half a page tall;
// a product whose supplier stopped serving a link gets it too, without anything
// being deleted — the link may work again tomorrow. The stub is served as a file,
// so a catalogue of sixty cards costs one request for all of them.
func TestNoPhotoPlaceholder(t *testing.T) {
	d, h := setup(t)

	for _, path := range []string{"/", "/p/krasnyj-chajnik"} {
		if !strings.Contains(get(t, h, path), `class="frame`) {
			t.Errorf("%s: no frame to carry the placeholder", path)
		}
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/nophoto.svg", nil))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "<svg") {
		t.Fatalf("/nophoto.svg: %d %.40s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("content-type = %q", ct)
	}

	p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik")
	if err := d.AddImage(p.ID, "https://cdn.example/x.jpg"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/p/krasnyj-chajnik"} {
		body := get(t, h, path)
		if !strings.Contains(body, "https://cdn.example/x.jpg") ||
			!strings.Contains(body, `class="frame`) {
			t.Errorf("%s: a real photo must render inside the frame", path)
		}
	}
}

// Yandex and Google both refuse a shop that does not publish how it ships and
// how it takes money, so the page has to exist — and has to stay out of the
// footer and the sitemap while the owner has not written the terms.
func TestInfoPage(t *testing.T) {
	d, h := setup(t)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/info", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("empty terms: GET /info = %d, want 404", w.Code)
	}
	if body := get(t, h, "/"); strings.Contains(body, `href="/info"`) {
		t.Error("footer links to /info with no terms filled in")
	}
	if strings.Contains(get(t, h, "/sitemap.xml"), "/info") {
		t.Error("sitemap lists /info with no terms filled in")
	}

	const terms = "Доставка курьером — 1–2 дня.\nОплата при получении."
	s, _ := d.GetSettings()
	s.Terms = terms
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}

	_, after, ok := strings.Cut(get(t, h, "/info"), `<div class="terms">`)
	if !ok {
		t.Fatal("/info has no terms block")
	}
	if block, _, _ := strings.Cut(after, "</div>"); !strings.Contains(block, "Оплата при получении") {
		t.Errorf("terms block is missing the text: %q", block)
	}
	if body := get(t, h, "/"); !strings.Contains(body, `href="/info"`) {
		t.Error("footer does not link to /info")
	}
	if !strings.Contains(get(t, h, "/sitemap.xml"), "https://shop.example.com/info") {
		t.Error("sitemap does not list /info")
	}
}

// A category is the landing page of the shop: a search for "купить КПБ евро"
// must arrive at a page with its own heading and only its own goods, not at the
// catalogue with a query parameter.
func TestCategoryPages(t *testing.T) {
	d, h := setup(t)
	seed := []struct{ title, category string }{
		{"КПБ Евро сатин", "Текстиль/Спальня/КПБ"},
		{"КПБ 1,5 спальный", "Текстиль/Спальня/КПБ"},
		{"Подушка перьевая", "Текстиль/Спальня/Подушки"},
		{"Сковорода чугунная", "Посуда"},
	}
	for _, s := range seed {
		if err := d.CreateProduct(&database.Product{Title: s.title, Price: 1000,
			Stock: 5, Category: s.category}); err != nil {
			t.Fatal(err)
		}
	}

	leaf := get(t, h, "/c/tekstil/spalnya/kpb")
	if !strings.Contains(leaf, "<h1>КПБ</h1>") {
		t.Error("leaf page has no heading of its own")
	}
	if strings.Contains(leaf, "Сковорода") || strings.Contains(leaf, "Подушка") {
		t.Error("leaf page shows goods from another category")
	}
	if !strings.Contains(leaf, `href="/c/tekstil/spalnya"`) {
		t.Error("breadcrumbs do not lead back up the tree")
	}

	// A parent must never be an empty page: its goods live in its children.
	parent := get(t, h, "/c/tekstil")
	for _, want := range []string{"КПБ Евро сатин", "Подушка перьевая",
		`href="/c/tekstil/spalnya"`} {
		if !strings.Contains(parent, want) {
			t.Errorf("parent page missing %q", want)
		}
	}

	// Two categories that render the same title or description are two pages
	// competing for one query — the bug this whole feature exists to fix.
	other := get(t, h, "/c/posuda")
	if title(leaf) == title(other) {
		t.Errorf("two categories share one title: %q", title(leaf))
	}
	if description(leaf) == description(other) {
		t.Errorf("two categories share one description: %q", description(leaf))
	}
	if !strings.Contains(leaf, `<link rel="canonical" href="https://shop.example.com/c/tekstil/spalnya/kpb">`) {
		t.Error("category page has no canonical of its own")
	}

	// The old parameter form is in the index and in bookmarks.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/?category=Посуда", nil))
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/c/posuda" {
		t.Errorf("?category= = %d %q, want 301 /c/posuda", w.Code, w.Header().Get("Location"))
	}

	for _, path := range []string{"/c/net-takoj", "/c/tekstil/spalnya/kpb?page=99"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, w.Code)
		}
	}

	sitemap := get(t, h, "/sitemap.xml")
	for _, want := range []string{"/c</loc>", "/c/tekstil</loc>", "/c/tekstil/spalnya/kpb</loc>"} {
		if !strings.Contains(sitemap, want) {
			t.Errorf("sitemap missing %q", want)
		}
	}
	if !strings.Contains(get(t, h, "/c"), `href="/c/posuda"`) {
		t.Error("the category index does not list the categories")
	}
}

func title(body string) string {
	_, after, _ := strings.Cut(body, "<title>")
	out, _, _ := strings.Cut(after, "</title>")
	return out
}

func description(body string) string {
	_, after, _ := strings.Cut(body, `<meta name="description" content="`)
	out, _, _ := strings.Cut(after, `">`)
	return out
}

// Sorting and "in stock" are links, not JavaScript, and every variant points at
// the plain page: the same goods in another order are one page for a crawler.
func TestCatalogFilters(t *testing.T) {
	d, h := setup(t)
	_ = d.CreateProduct(&database.Product{Title: "Дешёвый ковш", Price: 100, Stock: 3})
	_ = d.CreateProduct(&database.Product{Title: "Дорогая кастрюля", Price: 900000, Stock: 0})

	cheapFirst := get(t, h, "/?sort=price")
	if strings.Index(cheapFirst, "Дешёвый ковш") > strings.Index(cheapFirst, "Дорогая кастрюля") {
		t.Error("?sort=price did not put the cheap one first")
	}
	dearFirst := get(t, h, "/?sort=price&desc=1")
	if strings.Index(dearFirst, "Дорогая кастрюля") > strings.Index(dearFirst, "Дешёвый ковш") {
		t.Error("?sort=price&desc=1 did not put the dear one first")
	}
	inStock := get(t, h, "/?instock=1")
	if strings.Contains(inStock, "Дорогая кастрюля") {
		t.Error("?instock=1 shows a product that is out of stock")
	}
	if !strings.Contains(cheapFirst, `<link rel="canonical" href="https://shop.example.com/">`) {
		t.Error("a sorted page must point at the plain one")
	}
	if strings.Contains(cheapFirst, "<script>") {
		t.Error("storefront must stay JS-free")
	}
}

// HEAD must survive the outer router. A method-specific route registered there
// (/admin* is GET-only) makes chi answer 405 to a HEAD of any page before the
// mounted storefront runs its own middleware — monitoring and link checkers
// then see a shop that looks broken.
func TestHeadThroughOuterRouter(t *testing.T) {
	_, sf := setup(t)
	r := chi.NewRouter()
	r.Use(HeadAsGet)
	r.Get("/admin*", func(http.ResponseWriter, *http.Request) {})
	r.Mount("/", sf)

	for _, path := range []string{"/", "/p/krasnyj-chajnik", "/sitemap.xml"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("HEAD", path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("HEAD %s = %d, want 200", path, w.Code)
		}
	}
}

// The owner's text is what makes a category page a landing page instead of a
// listing: it shows above the goods and becomes the page description, because a
// generated one repeats on every page of every shop.
func TestCategoryText(t *testing.T) {
	d, h := setup(t)
	if err := d.CreateProduct(&database.Product{Title: "Кастрюля", Price: 100,
		Stock: 1, Category: "Посуда/Кастрюли"}); err != nil {
		t.Fatal(err)
	}

	plain := get(t, h, "/c/posuda/kastryuli")
	if strings.Contains(plain, `class="cat-text"`) {
		t.Error("an empty text must leave no block on the page")
	}

	const body = "Кастрюли из нержавейки и эмали, от полутора до десяти литров. " +
		"Оптом от десяти штук, доставка по Беларуси за три дня. " +
		"Берём под заказ то, чего нет на складе, и держим цену месяц."
	if err := d.SetCategoryText("Посуда/Кастрюли", body); err != nil {
		t.Fatal(err)
	}

	page := get(t, h, "/c/posuda/kastryuli")
	if !strings.Contains(page, "Оптом от десяти штук") {
		t.Error("the text is not on the page")
	}
	desc := description(page)
	if !strings.HasPrefix(desc, "Кастрюли из нержавейки") {
		t.Errorf("description does not come from the text: %q", desc)
	}
	if len([]rune(desc)) > 160 {
		t.Errorf("description is %d runes, a snippet shows about 160", len([]rune(desc)))
	}
	// Cut on a sentence, never mid-word: a description ending in "дост" reads as
	// a broken page in the search results.
	if !strings.HasSuffix(desc, ".") && !strings.HasSuffix(desc, "…") {
		t.Errorf("description ends mid-sentence: %q", desc)
	}

	// Clearing the field removes the block rather than leaving an empty one.
	if err := d.SetCategoryText("Посуда/Кастрюли", "  "); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(get(t, h, "/c/posuda/kastryuli"), `class="cat-text"`) {
		t.Error("a cleared text must leave no block")
	}
}

// Tidying the tree must not throw away what a page earned: a renamed category
// answers 301 at its old address, and a hidden one disappears from the shop
// altogether.
func TestCategoryRenameAndHide(t *testing.T) {
	d, h := setup(t)
	if err := d.CreateProduct(&database.Product{Title: "КПБ Евро", Price: 100,
		Stock: 1, Category: "Текстиль/КПБ"}); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateProduct(&database.Product{Title: "Кастрюля", Price: 100,
		Stock: 1, Category: "Посуда"}); err != nil {
		t.Fatal(err)
	}

	if err := d.RenameCategory("Текстиль", "Домашний текстиль"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/c/tekstil/kpb", nil))
	if w.Code != http.StatusMovedPermanently ||
		w.Header().Get("Location") != "/c/domashnij-tekstil/kpb" {
		t.Errorf("old address = %d %q, want 301 to the new one", w.Code, w.Header().Get("Location"))
	}
	if body := get(t, h, "/c/domashnij-tekstil/kpb"); !strings.Contains(body, "КПБ Евро") {
		t.Error("the renamed category lost its goods")
	}

	if err := d.SetCategoryHidden("Посуда", true); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/c/posuda", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("hidden category = %d, want 404", w.Code)
	}
	for _, path := range []string{"/", "/c", "/sitemap.xml"} {
		if strings.Contains(get(t, h, path), "/c/posuda") {
			t.Errorf("%s still links to a hidden category", path)
		}
	}
}

// A buyer leaves a phone or an email, whichever they prefer — but not neither:
// an order nobody can be reached about is a lost sale that looks like a sale.
func TestOrderContacts(t *testing.T) {
	d, h := setup(t)
	p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik")

	order := func(form string) *httptest.ResponseRecorder {
		add := httptest.NewRequest("POST", "/cart/add",
			strings.NewReader("slug="+p.Slug+"&qty=1"))
		add.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, add)
		req := httptest.NewRequest("POST", "/cart/order", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range w.Result().Cookies() {
			req.AddCookie(c)
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		return out
	}

	// Neither contact: the cart comes back with a message, no order is created.
	w := order("name=Иван")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Оставьте телефон или почту") {
		t.Errorf("no contacts = %d, want the cart with a notice", w.Code)
	}
	if orders, _ := d.ListOrders(); len(orders) != 0 {
		t.Fatalf("an order without contacts was created: %+v", orders)
	}

	// Email alone is enough.
	if w := order("name=Иван&email=ivan@example.com"); w.Code != http.StatusSeeOther {
		t.Fatalf("email-only order = %d, want 303", w.Code)
	}
	orders, _ := d.ListOrders()
	if len(orders) != 1 || orders[0].Email != "ivan@example.com" || orders[0].Phone != "" {
		t.Fatalf("order = %+v", orders)
	}

	// And so is a phone alone, as before.
	if w := order("name=Пётр&phone=%2B79990000000"); w.Code != http.StatusSeeOther {
		t.Fatalf("phone-only order = %d, want 303", w.Code)
	}
	if orders, _ := d.ListOrders(); len(orders) != 2 {
		t.Fatalf("orders = %+v", orders)
	}
}

// A cart is where a shop loses the sale it already had, so its controls must be
// obvious: minus, plus and a cross, not "type 0 to delete".
func TestCartRowControls(t *testing.T) {
	d, h := setup(t)
	p, _ := d.GetVisibleProductBySlug("krasnyj-chajnik")

	add := httptest.NewRequest("POST", "/cart/add",
		strings.NewReader("slug="+p.Slug+"&qty=2"))
	add.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, add)
	cookies := w.Result().Cookies()

	update := func(form string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/cart/update", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range cookies {
			req.AddCookie(c)
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		cookies = out.Result().Cookies()
		return out
	}
	cartBody := func() string {
		req := httptest.NewRequest("GET", "/cart", nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		out := httptest.NewRecorder()
		h.ServeHTTP(out, req)
		return out.Body.String()
	}

	if body := cartBody(); !strings.Contains(body, `value="remove"`) ||
		!strings.Contains(body, `value="inc"`) || !strings.Contains(body, `value="dec"`) {
		t.Error("the cart row has no minus, plus and remove controls")
	}

	update("slug=" + p.Slug + "&action=inc&qty=2")
	if !strings.Contains(cartBody(), "Красный чайник") {
		t.Fatal("the product left the cart on plus")
	}
	update("slug=" + p.Slug + "&action=remove&qty=3")
	if strings.Contains(cartBody(), "Красный чайник") {
		t.Error("the cross did not remove the row")
	}
}

// A price change must reach a shopper and a crawler without anyone submitting
// anything: the page is built per request, so it may not be cached. The feed is
// allowed an hour — the provider re-fetches it on its own schedule.
func TestPriceIsNeverCached(t *testing.T) {
	d, h := setup(t)
	p, err := d.GetVisibleProductBySlug("krasnyj-chajnik")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/", "/p/krasnyj-chajnik"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if cc := w.Header().Get("Cache-Control"); cc != "" {
			t.Errorf("%s must not be cached, got %q", path, cc)
		}
	}

	// The same page, one price change later: no purge, no resubmission.
	p.Price = 999900
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}
	body := get(t, h, "/p/krasnyj-chajnik")
	if !strings.Contains(body, "9999.00") {
		t.Fatal("the page still shows the old price")
	}
	// schema.org carries the same number, so the shopper and the search engine
	// never see two different prices.
	if strings.Count(body, "9999.00") < 2 {
		t.Fatal("the markup disagrees with the page")
	}
	// And the sitemap says the page moved, which is what brings the crawler back.
	if !strings.Contains(get(t, h, "/sitemap.xml"), "<lastmod>") {
		t.Fatal("no lastmod for a changed product")
	}
}

// Crawlers ask for /favicon.ico before they read the page, and Google puts that
// icon next to the snippet. A 404 there is why a shop shows a globe while its
// competitors show their own mark.
func TestFaviconICO(t *testing.T) {
	d, h := setup(t)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/favicon.ico", nil))
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/favicon.svg" {
		t.Fatalf("without a logo: %d → %q", w.Code, w.Header().Get("Location"))
	}

	// With a logo uploaded the same address must lead to it, not to the letter.
	if err := d.UpdateSettings(&database.Settings{ShopName: "Лавка", Logo: "logo-x.png",
		Currency: database.ShopCurrencyBYN}); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/favicon.ico", nil))
	if loc := w.Header().Get("Location"); loc != "/uploads/logo-x.png" {
		t.Fatalf("with a logo: %q", loc)
	}
}

// A price with no validity date is eventually dropped from the search snippet as
// stale, and an offer with no condition is incomplete. Both are things the shop
// actually knows — unlike a rating, which it must never invent.
// The measurements are the owner's, never a guess: a product nobody weighed
// says nothing at all — no table of dashes on the page and no empty node in the
// markup, which is what a shipping quote reads.
func TestSpecsShownOnlyWhenStated(t *testing.T) {
	d, h := setup(t)

	silent := get(t, h, "/p/krasnyj-chajnik")
	if strings.Contains(silent, "Характеристики") {
		t.Error("a product with no measurements grew a specs block")
	}
	for _, node := range []string{`"weight"`, `"width"`, `"height"`, `"depth"`} {
		if strings.Contains(silent, node) {
			t.Errorf("empty %s published in the markup", node)
		}
	}

	p, err := d.GetVisibleProductBySlug("krasnyj-chajnik")
	if err != nil {
		t.Fatal(err)
	}
	weight, l, w, ht := int64(1200), int64(300), int64(200), int64(150)
	p.WeightG, p.LengthMM, p.WidthMM, p.HeightMM = &weight, &l, &w, &ht
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}

	body := get(t, h, "/p/krasnyj-chajnik")
	if !strings.Contains(body, "Характеристики") {
		t.Fatal("stated measurements are not shown")
	}
	// Stored in grams and millimetres, read in kilograms and centimetres.
	if !strings.Contains(body, "1.2 кг") {
		t.Error("weight not shown in kilograms")
	}
	if !strings.Contains(body, "30 × 20 × 15 см") {
		t.Error("size not shown in centimetres")
	}
	// The markup keeps the stored units, which is what a machine wants. Spaces
	// are collapsed first: html/template pads numbers inside a script, and a
	// test that encodes that quirk breaks on the next Go release.
	flat := strings.Join(strings.Fields(body), " ")
	if !strings.Contains(flat, `"value": 1200 , "unitCode": "GRM"`) {
		t.Error("weight missing from the markup")
	}
	if !strings.Contains(flat, `"value": 150 , "unitCode": "MMT"`) {
		t.Error("height missing from the markup")
	}
}

func TestOfferCarriesWhatWeKnow(t *testing.T) {
	_, h := setup(t)
	body := get(t, h, "/p/krasnyj-chajnik")
	for _, want := range []string{
		`"priceValidUntil": "`,
		`"itemCondition": "https://schema.org/NewCondition"`,
		`"availability": "https://schema.org/InStock"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %s", want)
		}
	}
	// The date must be a real one, a month out, not a zero value.
	m := regexp.MustCompile(`"priceValidUntil": "(\d{4}-\d{2}-\d{2})"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("priceValidUntil is not a date")
	}
	when, err := time.Parse("2006-01-02", m[1])
	if err != nil || !when.After(time.Now().AddDate(0, 0, 20)) {
		t.Fatalf("priceValidUntil = %q", m[1])
	}
	// And nothing invents a rating: stars require reviews we do not have.
	if strings.Contains(body, "aggregateRating") {
		t.Fatal("a rating must never be marked up without real reviews")
	}
}

// A shop is expected to say whom the buyer is paying, and this one had nowhere
// to say it: only /info existed. The page writes nothing new — it renders the
// phone and the legal details the owner already filled — so it appears exactly
// when there is something on it, and 404s when there is not.
func TestContactsAppearsOnlyWhenThereIsSomethingToShow(t *testing.T) {
	d, h := setup(t)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/contacts", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("empty settings: /contacts answered %d, want 404", w.Code)
	}
	if strings.Contains(get(t, h, "/sitemap.xml"), "/contacts") {
		t.Error("a page that 404s must not be in the sitemap")
	}

	s, _ := d.GetSettings()
	s.ShopPhone = "+375 33 652 92 91"
	s.Requisites = "ИП Петровский, УНП 191234567"
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}

	body := get(t, h, "/contacts")
	for _, want := range []string{"+375 33 652 92 91", "191234567", "Контакты"} {
		if !strings.Contains(body, want) {
			t.Errorf("/contacts is missing %q", want)
		}
	}
	// The contact address is what a buyer looks for, so it is published on
	// purpose even though it doubles as the admin login: the password is the
	// secret, and guessing it is throttled.
	if !strings.Contains(body, s.OwnerEmail) {
		t.Error("no contact address on the contacts page")
	}
	if !strings.Contains(get(t, h, "/sitemap.xml"), "/contacts") {
		t.Error("/contacts is not in the sitemap")
	}
	if !strings.Contains(get(t, h, "/llms.txt"), "/contacts") {
		t.Error("/contacts is not in llms.txt")
	}
	if !strings.Contains(get(t, h, "/"), `href="/contacts"`) {
		t.Error("no link to /contacts in the footer")
	}
}

// Clean-param folds the sorted copies of a section into the section itself, so
// a crawler stops spending requests on permutations of the same products. The
// half that matters is what it must NOT list: page two holds different goods,
// and folding it into page one would hide most of a 24 000-product catalogue.
func TestRobotsCleanParam(t *testing.T) {
	_, h := setup(t)
	rb := get(t, h, "/robots.txt")
	line := ""
	for _, l := range strings.Split(rb, "\n") {
		if strings.HasPrefix(l, "Clean-param:") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no Clean-param: %s", rb)
	}
	for _, p := range []string{"sort", "desc", "instock"} {
		if !strings.Contains(line, p) {
			t.Errorf("%q missing from %q", p, line)
		}
	}
	if strings.Contains(line, "page") {
		t.Errorf("page must stay crawlable: %q", line)
	}
}

// TestProductShowsCharacteristics: an imported card carries a dozen properties
// and they are what a buyer compares and a search engine matches a query
// against. They belong in the visible table and in the structured data, both
// stated by the source and neither invented.
func TestProductShowsCharacteristics(t *testing.T) {
	d, h := setup(t)
	p := &database.Product{
		SKU: "A", Title: "Брелок", Price: 2849, Stock: 5,
		Params: []database.Param{
			{Name: "Материал изделия", Value: []any{"PETG пластик"}},
			{Name: "Цвет", Value: "прозрачный"},
			{Name: "Ширина предмета", Value: 5.0},
			{Name: "Пустое", Value: ""},
		},
	}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}

	body := get(t, h, "/p/"+p.Slug)
	for _, want := range []string{"Материал изделия", "PETG пластик", "прозрачный",
		"Ширина предмета", ">5<"} {
		if !strings.Contains(body, want) {
			t.Errorf("страница не показывает %q", want)
		}
	}
	// A list of one is how a marketplace states a single value; brackets and
	// quotes are Go syntax and have no business on a page.
	if strings.Contains(body, "[PETG") || strings.Contains(body, "Пустое") {
		t.Error("сырое значение или пустая характеристика попали на страницу")
	}
	if !strings.Contains(body, `"additionalProperty"`) ||
		!strings.Contains(body, `"@type": "PropertyValue"`) {
		t.Error("характеристик нет в разметке для поисковика")
	}

	// What a buyer sees is the owner's call, not ours: a marketplace card also
	// carries their paperwork, and which of it belongs on a page is a decision
	// they make once per property in the settings.
	if err := d.SetHiddenParams([]string{"Цвет"}); err != nil {
		t.Fatal(err)
	}
	body = get(t, h, "/p/"+p.Slug)
	if strings.Contains(body, "прозрачный") {
		t.Error("снятая характеристика осталась на странице")
	}
	if !strings.Contains(body, "PETG пластик") {
		t.Error("вместе с ней пропали остальные")
	}
}
