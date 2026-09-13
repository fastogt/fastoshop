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

// The button must reach our server, or the log has no click to count.
func TestOrderLinksGoThroughTheShop(t *testing.T) {
	shop := &database.Settings{Telegram: "@lavka", WhatsApp: "+375291234567"}
	links := orderLinks(shop, &database.Product{Slug: "ersh"})
	if len(links) != 2 || links[0].URL != "/go/telegram/ersh" || links[1].URL != "/go/whatsapp/ersh" {
		t.Errorf("links: %+v", links)
	}
}

func TestOrderRedirect(t *testing.T) {
	d, h := setup(t)
	s, _ := d.GetSettings()
	s.Telegram = "@lavka"
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	status := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}

	w := status("/go/telegram/krasnyj-chajnik")
	loc := w.Header().Get("Location")
	if w.Code != http.StatusFound || !strings.HasPrefix(loc, "https://t.me/lavka?text=") {
		t.Fatalf("telegram: %d %q", w.Code, loc)
	}
	u, _ := url.Parse(loc)
	if !strings.Contains(u.Query().Get("text"), "https://shop.example.com/p/krasnyj-chajnik") {
		t.Errorf("the message lost the card link: %q", u.Query().Get("text"))
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("a cached redirect hides the click: %q", w.Header().Get("Cache-Control"))
	}

	// The target comes from the settings only; a parameter must not steer it.
	if loc := status("/go/telegram/krasnyj-chajnik?to=https://evil.example").Header().Get("Location"); strings.Contains(loc, "evil") {
		t.Errorf("open redirect: %q", loc)
	}
	for _, path := range []string{"/go/whatsapp/krasnyj-chajnik", "/go/viber/krasnyj-chajnik", "/go/telegram/net-takogo"} {
		if code := status(path).Code; code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, code)
		}
	}
	if !strings.Contains(get(t, h, "/robots.txt"), "Disallow: /go/") {
		t.Error("crawlers would follow a redirect from every card")
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
	if links[0].URL != "https://t.me/lavka" || links[1].URL != "https://wa.me/375291234567" {
		t.Errorf("not bare accounts: %+v", links)
	}
	if len(contactLinks(&database.Settings{})) != 0 {
		t.Error("a shop without messengers got footer buttons")
	}
}
