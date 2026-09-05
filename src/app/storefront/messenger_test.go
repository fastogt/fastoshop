package storefront

import (
	"net/url"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// Quotes and slashes in an unescaped href end the URL at the first one.
func TestOrderLinkSurvivesQuotesAndSlashes(t *testing.T) {
	shop := &database.Settings{Currency: "RUB", Telegram: "@lavka", WhatsApp: "+375 (29) 123-45-67"}
	p := &database.Product{
		Title: `Ерш унитазный с/подст "Шляпа" д13х36см`,
		SKU:   "022382", Price: 67600,
	}
	links := orderLinks(shop, p, "https://shop.example.com/p/ersh")
	if len(links) != 2 {
		t.Fatalf("links: %d, want telegram and whatsapp", len(links))
	}
	for _, l := range links {
		u, err := url.Parse(l.URL)
		if err != nil {
			t.Fatalf("%s: unparsable: %v", l.Label, err)
		}
		text := u.Query().Get("text")
		if !strings.Contains(text, `с/подст "Шляпа"`) {
			t.Errorf("%s: the title did not survive escaping: %q", l.Label, text)
		}
		for _, want := range []string{"022382", "676.00", "https://shop.example.com/p/ersh"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: message is missing %q", l.Label, want)
			}
		}
	}
	if !strings.HasPrefix(links[0].URL, "https://t.me/lavka?") {
		t.Errorf("telegram handle not normalised: %s", links[0].URL)
	}
	// wa.me refuses anything but digits, and an owner pastes spaces and a plus.
	if !strings.HasPrefix(links[1].URL, "https://wa.me/375291234567?") {
		t.Errorf("whatsapp number not normalised: %s", links[1].URL)
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
	if links := orderLinks(shop, p, "https://shop.example.com/p/chajnik"); len(links) != 0 {
		t.Errorf("buttons without a messenger: %+v", links)
	}
}
