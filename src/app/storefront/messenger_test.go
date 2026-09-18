package storefront

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// Quotes and slashes in an unescaped href end the URL at the first one.
func TestMessengerURLSurvivesQuotesAndSlashes(t *testing.T) {
	shop := &database.Settings{Currency: "RUB", Telegram: "@lavka", WhatsApp: "+375 (29) 123-45-67"}
	p := &database.Product{
		Title: `Ерш унитазный с/подст "Шляпа" д13х36см`,
		SKU:   "022382", Price: 67600,
	}
	tg := messengerURL(shop, p, "https://shop.example.com/p/ersh", kTelegram)
	wa := messengerURL(shop, p, "https://shop.example.com/p/ersh", kWhatsApp)
	for _, link := range []string{tg, wa} {
		u, err := url.Parse(link)
		if err != nil {
			t.Fatalf("%s: unparsable: %v", link, err)
		}
		text := u.Query().Get("text")
		if !strings.Contains(text, `с/подст "Шляпа"`) {
			t.Errorf("%s: the title did not survive escaping: %q", link, text)
		}
		for _, want := range []string{"022382", "676.00", "https://shop.example.com/p/ersh"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: message is missing %q", link, want)
			}
		}
	}
	if !strings.HasPrefix(tg, "https://t.me/lavka?") {
		t.Errorf("telegram handle not normalised: %s", tg)
	}
	// wa.me refuses anything but digits, and an owner pastes spaces and a plus.
	if !strings.HasPrefix(wa, "https://wa.me/375291234567?") {
		t.Errorf("whatsapp number not normalised: %s", wa)
	}
}

// Telegram does not read "+" as a space in ?text=, so the buyer saw "Хочу+заказать".
func TestMessengerURLEncodesSpacesAsPercent(t *testing.T) {
	shop := &database.Settings{Currency: "BYN", Telegram: "@lavka", WhatsApp: "+375291234567"}
	p := &database.Product{Title: "Кастрюля 2,9л C++ edition", SKU: "083497", Price: 3952}
	for _, kind := range []string{kTelegram, kWhatsApp} {
		link := messengerURL(shop, p, "https://shop.example.com/p/k", kind)
		raw := link[strings.Index(link, "text=")+len("text="):]
		if strings.Contains(raw, "+") {
			t.Errorf("%s: a plus in the raw text reaches the buyer literally: %s", kind, raw)
		}
		text, err := url.PathUnescape(raw)
		if err != nil || !strings.Contains(text, "Кастрюля 2,9л C++ edition") {
			t.Errorf("%s: title did not survive: %q (%v)", kind, text, err)
		}
	}
}

// A blocker drops a ping but not a navigation, so the button's href is our own address.
func TestOrderLinksGoThroughTheShop(t *testing.T) {
	shop := &database.Settings{Telegram: "@lavka", WhatsApp: "+375291234567"}
	links := orderLinks(shop, &database.Product{Slug: "ersh"})
	if len(links) != 2 || links[0].URL != "/go/telegram/ersh" || links[1].URL != "/go/whatsapp/ersh" {
		t.Fatalf("links: %+v", links)
	}
}

// The hop is the count: every button click is a GET that nginx logs, then a redirect.
func TestGoRedirects(t *testing.T) {
	d, h := setup(t)
	s, _ := d.GetSettings()
	s.Telegram = "@lavka"
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	do := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	w := do("GET", "/go/telegram/krasnyj-chajnik")
	if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "https://t.me/lavka?text=") {
		t.Fatalf("click: %d -> %q", w.Code, w.Header().Get("Location"))
	}
	// A cached redirect would send the next click past us, uncounted.
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("redirect is cacheable: %q", w.Header().Get("Cache-Control"))
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "krasnyj-chajnik") {
		t.Errorf("the message lost the page address: %s", loc)
	}
	for _, path := range []string{"/go/whatsapp/krasnyj-chajnik", "/go/viber/krasnyj-chajnik",
		"/go/telegram/net-takogo", "/go/whatsapp"} {
		if code := do("GET", path).Code; code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, code)
		}
	}
	if w := do("GET", "/go/telegram"); w.Code != http.StatusFound || w.Header().Get("Location") != "https://t.me/lavka" {
		t.Errorf("footer contact: %d -> %q", w.Code, w.Header().Get("Location"))
	}
	body := get(t, h, "/p/krasnyj-chajnik")
	if !strings.Contains(body, `href="/go/telegram/krasnyj-chajnik"`) || strings.Contains(body, "ping=") {
		t.Error("the card button must lead through us, with no ping to double the count")
	}
	if !strings.Contains(get(t, h, "/robots.txt"), "Disallow: /go/") {
		t.Error("robots.txt no longer closes /go/")
	}
}

// An owner pastes whatever their client shows them; all of these are one account.
func TestTelegramHandleForms(t *testing.T) {
	for _, raw := range []string{"lavka", "@lavka", "t.me/lavka", "https://t.me/lavka", " @lavka "} {
		if got := telegramHandle(raw); got != "lavka" {
			t.Errorf("%q -> %q, want lavka", raw, got)
		}
	}
}

// No messenger set is the default, and a button that goes nowhere is worse than none.
func TestNoMessengerNoButtons(t *testing.T) {
	shop := &database.Settings{Currency: "RUB"}
	p := &database.Product{Title: "Чайник", Price: 100}
	if links := orderLinks(shop, p); len(links) != 0 {
		t.Errorf("buttons without a messenger: %+v", links)
	}
}

// The footer link carries no product, so it must carry no query either: a bare
// account, and nothing that a page without a product could interpolate empty.
func TestContactLinksAreBare(t *testing.T) {
	shop := &database.Settings{Telegram: "https://t.me/lavka/", WhatsApp: "+375 (29) 123-45-67"}
	links := contactLinks(shop)
	if len(links) != 2 {
		t.Fatalf("links: %d, want telegram and whatsapp", len(links))
	}
	if links[0].URL != "/go/telegram" || links[1].URL != "/go/whatsapp" {
		t.Errorf("footer links must go through us: %+v", links)
	}
	if contactURL(shop, kTelegram) != "https://t.me/lavka" || contactURL(shop, kWhatsApp) != "https://wa.me/375291234567" {
		t.Errorf("not bare accounts: %q %q", contactURL(shop, kTelegram), contactURL(shop, kWhatsApp))
	}
	if len(contactLinks(&database.Settings{})) != 0 {
		t.Error("a shop without messengers got footer buttons")
	}
}
