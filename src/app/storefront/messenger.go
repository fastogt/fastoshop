package storefront

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/fastogt/fastoshop/app/database"
)

const (
	kTelegram = "telegram"
	kWhatsApp = "whatsapp"
)

// A plain link, never a widget: the storefront ships no JavaScript.
type orderLinkVM struct {
	// Label is what the button says; URL is already escaped and safe to print.
	Label string
	URL   string
	Ping  string
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

// The same accounts without a product: a buyer who came to ask, not to order.
func contactLinks(shop *database.Settings) []orderLinkVM {
	if shop == nil {
		return nil
	}
	var out []orderLinkVM
	if h := telegramHandle(shop.Telegram); h != "" {
		out = append(out, orderLinkVM{Label: "Telegram", URL: "https://t.me/" + h})
	}
	if n := whatsappNumber(shop.WhatsApp); n != "" {
		out = append(out, orderLinkVM{Label: "WhatsApp", URL: "https://wa.me/" + n})
	}
	return out
}

// From settings and product only; a title's quotes are escaped here, not in the template.
func messengerURL(shop *database.Settings, p *database.Product, pageURL, kind string) string {
	text := url.QueryEscape(orderMessage(shop, p, pageURL))
	switch kind {
	case kTelegram:
		if h := telegramHandle(shop.Telegram); h != "" {
			return "https://t.me/" + h + "?text=" + text
		}
	case kWhatsApp:
		if n := whatsappNumber(shop.WhatsApp); n != "" {
			return "https://wa.me/" + n + "?text=" + text
		}
	}
	return ""
}

// href stays the messenger for Metrika's messenger auto-goal; ping counts the click.
func orderLinks(shop *database.Settings, p *database.Product, pageURL string) []orderLinkVM {
	if shop == nil || p == nil {
		return nil
	}
	var out []orderLinkVM
	for _, m := range []struct{ kind, label string }{
		{kTelegram, "Заказать в Telegram"},
		{kWhatsApp, "Заказать в WhatsApp"},
	} {
		if u := messengerURL(shop, p, pageURL, m.kind); u != "" {
			out = append(out, orderLinkVM{Label: m.label, URL: u,
				Ping: "/go/" + m.kind + "/" + url.PathEscape(p.Slug)})
		}
	}
	return out
}

// OrderPing answers a button's ping: nginx logs the POST, and nothing is stored.
func (s *Storefront) OrderPing(w http.ResponseWriter, r *http.Request) {
	p, err := s.db.GetVisibleProductBySlug(chi.URLParam(r, "slug"))
	if err != nil || messengerURL(s.shop(), p, "", chi.URLParam(r, "messenger")) == "" {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
