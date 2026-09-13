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

// Escaped here, not in the template: quotes and slashes in a title break the href.
// messengerURL is built from the settings and the product, never from the request:
// taking the target from a parameter would turn /go/ into an open redirect.
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

// A direct t.me link never reaches the server, so the button goes through our own address.
func orderLinks(shop *database.Settings, p *database.Product) []orderLinkVM {
	if shop == nil || p == nil {
		return nil
	}
	var out []orderLinkVM
	if telegramHandle(shop.Telegram) != "" {
		out = append(out, orderLinkVM{Label: "Заказать в Telegram", URL: "/go/" + kTelegram + "/" + url.PathEscape(p.Slug)})
	}
	if whatsappNumber(shop.WhatsApp) != "" {
		out = append(out, orderLinkVM{Label: "Заказать в WhatsApp", URL: "/go/" + kWhatsApp + "/" + url.PathEscape(p.Slug)})
	}
	return out
}

// OrderRedirect is a messenger button click: nginx logs it, and the answer is only a redirect.
func (s *Storefront) OrderRedirect(w http.ResponseWriter, r *http.Request) {
	p, err := s.db.GetVisibleProductBySlug(chi.URLParam(r, "slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	target := messengerURL(s.shop(), p, s.baseURL+"/p/"+p.Slug, chi.URLParam(r, "messenger"))
	if target == "" {
		http.NotFound(w, r)
		return
	}
	// A cached redirect is a click the log never sees.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	http.Redirect(w, r, target, http.StatusFound)
}
