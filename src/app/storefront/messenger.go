package storefront

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
)

// A plain link, never a widget: the storefront ships no JavaScript.
type orderLinkVM struct {
	// Label is what the button says; URL is already escaped and safe to print.
	Label string
	URL   string
}

// "@shop", "shop" and "https://t.me/shop" all mean the same account.
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

// wa.me refuses spaces, brackets and a plus, and the owner will paste all three.
func whatsappNumber(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

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

// Escaped here, not in the template: quotes and slashes in a title break the href.
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
