package storefront

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

var kCyrillic = regexp.MustCompile(`\p{Cyrillic}+`)

// An English shop with every block switched on and a catalogue in Latin letters:
// whatever Cyrillic reaches the buyer then came from us, not from the owner.
func setupEnglish(t *testing.T) (*database.Database, http.Handler) {
	t.Helper()
	d, err := database.OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	_ = d.CreateSettings(&database.Settings{OwnerEmail: "a@b.c", PasswordHash: "h", ShopName: "Corner"})
	s, _ := d.GetSettings()
	s.Lang = i18n.LangEN
	s.ShopPhone, s.Telegram, s.SMTPHost = "+375291112233", "@corner", "smtp.example.com"
	s.Requisites, s.Terms = "Corner LLC, UNP 190304936", "We ship across the country."
	s.CartEnabled, s.CustomerKind = true, database.CustomerBoth
	s.DeliveryCost, s.DeliveryFreeFrom, s.DeliveryDays, s.ReturnDays = 50000, 1000000, 3, 14
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	w, l := int64(1500), int64(120)
	_ = d.CreateProduct(&database.Product{Title: "Red kettle", Description: "Boils fast.",
		SKU: "RK-1", Price: 250000, Stock: 3, Category: "Kitchen/Kettles",
		WeightG: &w, LengthMM: &l, WidthMM: &l, HeightMM: &l,
		Params: []database.Param{{Name: "Colour", Value: "red"}, {Name: "Cordless", Value: true}}})
	_ = d.CreateProduct(&database.Product{Title: "Gone mug", Price: 1000, Stock: 0})
	for i := range kCatalogPageSize {
		_ = d.CreateProduct(&database.Product{Title: fmt.Sprintf("Plate %d", i), Price: 500, Stock: 1})
	}
	return d, New(d, "https://shop.example.com", t.TempDir()).Router()
}

func noCyrillic(t *testing.T, what, body string) {
	t.Helper()
	if m := kCyrillic.FindAllString(body, 5); len(m) > 0 {
		t.Errorf("%s speaks Russian in an English shop: %q", what, m)
	}
}

func TestEnglishShopSpeaksEnglish(t *testing.T) {
	d, h := setupEnglish(t)
	for _, path := range []string{"/", "/?page=2", "/?q=kettle", "/?q=nothing", "/c",
		"/c/kitchen", "/c/kitchen/kettles", "/p/red-kettle", "/p/gone-mug", "/cart",
		"/info", "/contacts", "/offer", "/privacy", "/unsubscribe", "/?subscribed=a@b.c",
		"/?subscribe=bad", "/llms.txt", "/yml.xml", "/gmc.xml"} {
		noCyrillic(t, path, get(t, h, path))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/no-such-page", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("404 page: %d", w.Code)
	}
	noCyrillic(t, "404", w.Body.String())

	c := &client{h: h}
	c.add(t, "red-kettle", "1")
	noCyrillic(t, "cart with a line", c.cart(t))
	for name, form := range map[string]url.Values{
		"no contact": {"name": {"John"}, "consent": {"on"}},
		"no consent": {"name": {"John"}, "phone": {"+375291112233"}},
		"bad org":    {"name": {"John"}, "phone": {"+375291112233"}, "consent": {"on"}, "customer": {"company"}},
	} {
		if w := c.do(t, "POST", "/cart/order", form); w.Code != http.StatusOK {
			t.Fatalf("%s: %d", name, w.Code)
		} else {
			noCyrillic(t, name, w.Body.String())
		}
	}
	if w := c.do(t, "POST", "/cart/order", url.Values{"name": {"John"}, "email": {"j@example.com"},
		"consent": {"on"}, "subscribe": {"on"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("checkout: %d", w.Code)
	}
	noCyrillic(t, "order confirmation", c.cart(t))

	// The consent is stored as the buyer read it, in the shop's language.
	orders, _ := d.ListOrders()
	if len(orders) != 1 || orders[0].ConsentText != i18n.Page(i18n.LangEN, "consent_order") {
		t.Fatalf("order consent: %+v", orders)
	}
	subs, _ := d.Subscribers()
	if len(subs) != 1 || subs[0].ConsentText != i18n.Page(i18n.LangEN, "consent_order_subscribe") {
		t.Fatalf("subscriber consent: %+v", subs)
	}
}

// The storefront follows the setting, not the moment it was started.
func TestStorefrontFollowsTheShopLanguage(t *testing.T) {
	d, h := setup(t)
	if body := get(t, h, "/"); !strings.Contains(body, "Каталог") || !strings.Contains(body, `lang="ru"`) {
		t.Fatal("a Russian shop lost its catalogue heading")
	}
	s, _ := d.GetSettings()
	s.Lang = i18n.LangEN
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	body := get(t, h, "/")
	if !strings.Contains(body, "<h1>Catalogue</h1>") || !strings.Contains(body, `lang="en"`) {
		t.Errorf("switching to English left the storefront as it was:\n%s", body)
	}
}
