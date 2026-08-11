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
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/media"
)

func setup(t *testing.T) (*database.Database, http.Handler) {
	t.Helper()
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_ = d.CreateSettings(&database.Settings{OwnerEmail: "a@b.c", PasswordHash: "h", ShopName: "Лавка"})
	p := &database.Product{Title: "Красный чайник", Description: "Хорош",
		Price: 250000, Currency: "RUB", Stock: 3, Category: "kitchen"}
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
	body := get(t, h, "/")
	if strings.Contains(body, "<script") {
		t.Fatal("storefront serves a script without counters configured")
	}
	s, _ := d.GetSettings()
	s.GAMeasurementID = "G-ABC123"
	s.MetrikaCounterID = "12345678"
	s.GoogleVerification = "gtoken"
	s.YandexVerification = "ytoken"
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	body = get(t, h, "/")
	for _, want := range []string{
		`<meta name="google-site-verification" content="gtoken">`,
		`<meta name="yandex-verification" content="ytoken">`,
		"gtag/js?id=G-ABC123",
		"gtag('config','G-ABC123')",
		"ym('12345678','init'",
		"mc.yandex.ru/watch/12345678",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
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
		Price: 100, Currency: "RUB", Stock: 1}
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

// ldImage достаёт image из JSON-LD. Сравниваем разобранным значением, а не
// подстрокой: в JS-контексте html/template экранирует слэши (\/), это валидный
// JSON, но не совпадает с исходной строкой.
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

// Импорт больше не качает фото — в product_images.path лежит абсолютный URL
// источника. Он должен доехать до <img>, og:image и JSON-LD как есть.
func TestRemoteImageURLRenderedAsIs(t *testing.T) {
	d, h := setup(t)
	p, _ := d.GetProductBySlug("krasnyj-chajnik")
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
	if !strings.Contains(get(t, h, "/"), `<img src="https://cdn.example/x.jpg"`) {
		t.Error("catalog card must use the remote URL")
	}
}

// Локальные загрузки из админки продолжают жить под /uploads/.
func TestLocalImageStillUnderUploads(t *testing.T) {
	d, h := setup(t)
	p, _ := d.GetProductBySlug("krasnyj-chajnik")
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
	seedCatalog(t, d, 130) // +1 из setup = 131 товар, три страницы по 60

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
	// Третья страница — хвост каталога, «Дальше» уже нет.
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
	// За последней страницей — 404, а не пустая 200: мягкие 404 засоряют индекс.
	if c := code("/?page=99999"); c != http.StatusNotFound {
		t.Errorf("page beyond last: %d, want 404", c)
	}
	if c := code("/?page=abc"); c != http.StatusNotFound {
		t.Errorf("non-numeric page: %d, want 404", c)
	}
	if c := code("/?page=0"); c != http.StatusNotFound {
		t.Errorf("page=0: %d, want 404", c)
	}
	// У одного набора товаров — один URL.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/?page=1", nil))
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/" {
		t.Errorf("page=1 must 301 to /: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestOutOfStockAvailability(t *testing.T) {
	d, h := setup(t)
	p, _ := d.GetProductBySlug("krasnyj-chajnik")
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

// client — минимальная банка кук: тащит `cart` между запросами, как браузер.
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
	p := &database.Product{Title: "Синий стакан", Price: 30000, Currency: "RUB", Stock: 10}
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
		t.Errorf("чайник: %+v", it)
	}
	if it := byTitle["Синий стакан"]; it.Price != 30000 || it.Qty != 3 {
		t.Errorf("стакан: %+v", it)
	}
	// Кука очищена — повторная отправка не создаёт второй заказ.
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
	p, _ := d.GetProductBySlug("krasnyj-chajnik")
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

// Товар кончился уже после добавления — строка снимается на рендере, заказать
// исчезнувшее нельзя.
func TestCartDropsVanishedProduct(t *testing.T) {
	d, h := setup(t)
	secondProduct(t, d)
	c := &client{h: h}
	c.add(t, "krasnyj-chajnik", "1")
	c.add(t, "sinij-stakan", "1")
	p, _ := d.GetProductBySlug("krasnyj-chajnik")
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
	c.do(t, "POST", "/cart/order", url.Values{"name": {"Иван"}})
	if orders, _ := d.ListOrders(); len(orders) != 0 {
		t.Fatal("order without phone must be rejected")
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

// Единственный чекаут — старый одностраничный /p/{slug}/order удалён.
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
	p, _ := d.GetProductBySlug("krasnyj-chajnik")
	if p.Stock != 1 {
		t.Fatalf("stock %d, want 1", p.Stock)
	}
}

// Списание не сошлось на оформлении: заказа нет, покупатель видит названную
// причину, позиция уходит из корзины. Здесь это вызвано дублем строки в куке
// (две по 3 при остатке 3) — тот же путь, что и гонка двух покупателей за
// последней единицей, но воспроизводимо.
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
	if p, _ := d.GetProductBySlug("krasnyj-chajnik"); p.Stock != 3 {
		t.Errorf("stock must be untouched: %d", p.Stock)
	}
	// Позиция снята с корзины, остальное на месте.
	cart := c.cart(t)
	if strings.Contains(cart, "Красный чайник") || !strings.Contains(cart, "Синий стакан") {
		t.Errorf("cart must keep only what is available\n%s", cart)
	}
}

// Витрину белорусского магазина нельзя показывать в рублях: подпись видит
// покупатель, а priceCurrency уезжает в выдачу поисковика.
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
		t.Error("карточка не показывает цену в BYN")
	}
	if !strings.Contains(body, `"priceCurrency": "BYN"`) {
		t.Error("JSON-LD отдаёт не ту валюту")
	}
	if strings.Contains(body, "₽") {
		t.Error("на витрине остался знак рубля")
	}
}

// Скрытый товар обязан исчезнуть отовсюду разом. Особенно из sitemap: карта,
// ведущая на 404, портит доверие поисковика ко всему сайту.
func TestHiddenProductLeavesStorefront(t *testing.T) {
	d, h := setup(t)
	p := &database.Product{Title: "Тайный чайник", Price: 1000, Stock: 5}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	if body := get(t, h, "/"); !strings.Contains(body, "Тайный чайник") {
		t.Fatal("товар не появился на витрине до скрытия")
	}

	p.Hidden = true
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}

	if body := get(t, h, "/"); strings.Contains(body, "Тайный чайник") {
		t.Error("скрытый товар остался в каталоге")
	}
	if body := get(t, h, "/sitemap.xml"); strings.Contains(body, p.Slug) {
		t.Error("скрытый товар остался в sitemap")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/p/"+p.Slug, nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("страница скрытого товара: %d, ждали 404", w.Code)
	}

	// И его нельзя заказать в обход витрины, зная слаг.
	form := strings.NewReader("slug=" + p.Slug + "&qty=1")
	w = httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/cart/add", form)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(w, r)
	if len(w.Result().Cookies()) > 0 && strings.Contains(w.Result().Cookies()[0].Value, p.Slug) {
		t.Error("скрытый товар попал в корзину по прямой ссылке")
	}
}

// HEAD должен вести себя как GET: им проверяют доступность мониторинги, чекеры
// ссылок и часть краулеров, а 405 на витрине выглядит как сломанный сайт.
func TestHeadIsAllowedEverywhere(t *testing.T) {
	d, h := setup(t)
	p := &database.Product{Title: "Чайник HEAD", Price: 1000, Stock: 1}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/sitemap.xml", "/robots.txt", "/p/" + p.Slug, "/cart"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("HEAD", path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("HEAD %s: %d", path, w.Code)
		}
	}
}

// Иконка вкладки берётся из названия магазина: витрина принадлежит продавцу, и
// ставить туда наш знак значило бы брендировать чужой магазин.
func TestFaviconUsesShopInitial(t *testing.T) {
	d, h := setup(t)
	if err := d.UpdateSettings(&database.Settings{
		OwnerEmail: "a@b.c", PasswordHash: "h", ShopName: "Ромашка",
		Currency: database.ShopCurrencyRUB}); err != nil {
		t.Fatal(err)
	}
	body := get(t, h, "/favicon.svg")
	if !strings.Contains(body, ">Р<") {
		t.Errorf("буква магазина не попала в иконку: %s", body)
	}
	if !strings.Contains(body, "<svg") {
		t.Errorf("не svg: %s", body)
	}
	// И страница на неё ссылается, иначе браузер продолжит просить favicon.ico.
	if page := get(t, h, "/"); !strings.Contains(page, `href="/favicon.svg"`) {
		t.Error("страница не ссылается на иконку")
	}
}

// Логотип продавца заменяет текстовое название в шапке и становится иконкой
// вкладки. Название при этом не пропадает: оно уезжает в alt, иначе главная
// страница теряет текстовый сигнал о том, чей это магазин.
func TestShopLogoReplacesName(t *testing.T) {
	d, h := setup(t)
	base := &database.Settings{OwnerEmail: "a@b.c", PasswordHash: "h",
		ShopName: "Лавка Ивана", Currency: database.ShopCurrencyRUB}
	if err := d.UpdateSettings(base); err != nil {
		t.Fatal(err)
	}
	// Ищем именно тег, а не класс: класс есть и во встроенном CSS.
	if page := get(t, h, "/"); !strings.Contains(page, "Лавка Ивана") ||
		strings.Contains(page, `<img src="/uploads/`) {
		t.Fatal("без логотипа в шапке должно быть название текстом")
	}

	base.Logo = "logo-abc.png"
	if err := d.UpdateSettings(base); err != nil {
		t.Fatal(err)
	}
	page := get(t, h, "/")
	if !strings.Contains(page, `src="/uploads/logo-abc.png"`) {
		t.Error("логотип не попал в шапку")
	}
	if !strings.Contains(page, `alt="Лавка Ивана"`) {
		t.Error("название не сохранилось в alt")
	}
	if !strings.Contains(page, `<link rel="icon" href="/uploads/logo-abc.png">`) {
		t.Error("логотип не стал иконкой вкладки")
	}
}

// The buyer's search: a plain GET form, because the storefront carries no
// JavaScript to search with.
func TestStorefrontSearch(t *testing.T) {
	d, h := setup(t)
	_ = d.CreateProduct(&database.Product{Title: "Синяя кастрюля", Price: 100000,
		Currency: "RUB", Stock: 1, SKU: "KS-7"})
	hidden := &database.Product{Title: "Чайник со склада", Price: 100, Currency: "RUB", Hidden: true}
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
		_ = d.CreateProduct(&database.Product{Title: n, Price: 100, Currency: "RUB", Stock: 1})
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
		`class="crumbs"`, `href="/?category=kitchen"`, `"@type": "BreadcrumbList"`,
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
	p := &database.Product{Title: "Чайник", Price: 1000, Currency: "RUB", Stock: 1}
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
	p := &database.Product{Title: "Чайник", Price: 1000, Currency: "RUB", Stock: 1}
	_ = d.CreateProduct(p)
	_ = d.AddImage(p.ID, "p1-old.jpg")

	h := New(d, "https://shop.example.com", t.TempDir()).Router()
	if body := get(t, h, "/"); !strings.Contains(body, "/uploads/p1-old.jpg") {
		t.Error("a photo without a thumbnail disappeared from the grid")
	}
}
