package storefront

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
)

// orderLinkVM is one "order in a message" button: a plain link, never a script.
// The storefront ships no JavaScript, and a messenger deep link does not need
// any — which is exactly why this is a link and not a widget.
type orderLinkVM struct {
	// Label is what the button says; URL is already escaped and safe to print.
	Label string
	URL   string
}

// telegramHandle normalises what the owner pasted. They type "@shop",
// "shop" or the whole "https://t.me/shop" — all three mean the same account.
func telegramHandle(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "t.me/")
	s = strings.TrimPrefix(s, "@")
	if i := strings.IndexAny(s, "/?"); i >= 0 {
		s = s[:i]
	}
	return s
}

// whatsappNumber keeps the digits and nothing else: wa.me refuses a number with
// spaces, brackets or a plus, and the owner will paste all three.
func whatsappNumber(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// orderMessage is the text the buyer sends. It carries what the seller needs to
// answer without asking anything back: what, which article, at what price and
// the page it came from.
func orderMessage(shop *database.Settings, p *database.Product, pageURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Здравствуйте! Хочу заказать: %s", p.Title)
	if p.SKU != "" {
		fmt.Fprintf(&b, ", артикул %s", p.SKU)
	}
	fmt.Fprintf(&b, ", цена %s", priceStr(p.Price)+" "+shop.Sign())
	if pageURL != "" {
		fmt.Fprintf(&b, "\n%s", pageURL)
	}
	return b.String()
}

// orderLinks builds the buttons for one product. An empty setting yields no
// button at all: a shop that did not fill a messenger in must not show a link
// that goes nowhere.
//
// The whole message goes through url.QueryEscape here rather than in the
// template: a title like `Ерш унитазный с/подст "Шляпа" д13х36см` carries
// quotes and slashes, and pasting that into an href unescaped breaks the link
// at the first one.
func orderLinks(shop *database.Settings, p *database.Product, pageURL string) []orderLinkVM {
	if shop == nil || p == nil {
		return nil
	}
	text := url.QueryEscape(orderMessage(shop, p, pageURL))
	var out []orderLinkVM
	if h := telegramHandle(shop.Telegram); h != "" {
		out = append(out, orderLinkVM{
			Label: "Заказать в Telegram",
			URL:   "https://t.me/" + h + "?text=" + text,
		})
	}
	if n := whatsappNumber(shop.WhatsApp); n != "" {
		out = append(out, orderLinkVM{
			Label: "Заказать в WhatsApp",
			URL:   "https://wa.me/" + n + "?text=" + text,
		})
	}
	return out
}
