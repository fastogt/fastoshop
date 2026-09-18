package storefront

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/fastogt/fastoshop/app/database"
)

const (
	kTelegram = "telegram"
	kWhatsApp = "whatsapp"
)

// A plain link, never a widget: the storefront ships no JavaScript.
//
// Kind is what the buy box draws: "cart" is the form, everything else a button.
type orderLinkVM struct {
	Kind string
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
	// Telegram keeps "+" literal in ?text=; QueryEscape already made real pluses %2B.
	text := strings.ReplaceAll(url.QueryEscape(orderMessage(shop, p, pageURL)), "+", "%20")
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
			out = append(out, orderLinkVM{Kind: database.BuyMsg, Label: m.label, URL: u,
				Ping: "/go/" + m.kind + "/" + url.PathEscape(p.Slug)})
		}
	}
	return out
}

// buyBox is the page's buttons in the order the owner arranged them. A button
// whose card or account is missing simply does not appear: a dead button is
// worse than none.
func buyBox(names []string, shop *database.Settings, p *database.Product, pageURL string,
	nmID, sku int64, links []database.OutsideLink) []orderLinkVM {
	out := make([]orderLinkVM, 0, len(names))
	// A list that names no link of the seller's keeps them all, in their own
	// order: the owner arranges what the shop offers, not what they pasted.
	listed := false
	for _, name := range names {
		if strings.HasPrefix(name, database.BuyLink) {
			listed = true
		}
	}
	for _, name := range names {
		switch {
		case name == database.BuyMsg:
			out = append(out, orderLinks(shop, p, pageURL)...)
		case name == database.BuyWB && nmID > 0:
			out = append(out, orderLinkVM{Kind: database.BuyWB, Label: "Купить на Wildberries",
				URL:  withSource(fmt.Sprintf("https://www.wildberries.ru/catalog/%d/detail.aspx", nmID)),
				Ping: "/go/out/wb/" + url.PathEscape(p.Slug)})
		case name == database.BuyOzon && sku > 0:
			out = append(out, orderLinkVM{Kind: database.BuyOzon, Label: "Купить на Ozon",
				URL:  withSource(fmt.Sprintf("https://www.ozon.ru/product/%d/", sku)),
				Ping: "/go/out/ozon/" + url.PathEscape(p.Slug)})
		case strings.HasPrefix(name, database.BuyLink):
			// "link:1" is the second of the product's own links, in their order.
			i, _ := strconv.Atoi(strings.TrimPrefix(name, database.BuyLink))
			if i >= 0 && i < len(links) {
				out = append(out, outsideLink(links[i], p.Slug))
			}
		}
	}
	if !listed {
		for _, l := range links {
			out = append(out, outsideLink(l, p.Slug))
		}
	}
	return out
}

// outsideLink is one of the seller's own buttons: their card on a marketplace,
// their Instagram. The address is theirs, the ping is ours, and utm_source tells
// them where the visitor came from if their own panel shows it.
func outsideLink(l database.OutsideLink, slug string) orderLinkVM {
	return orderLinkVM{Kind: database.BuyLink, Label: l.Label, URL: withSource(l.URL),
		Ping: fmt.Sprintf("/go/out/%d/%s", l.ID, url.PathEscape(slug))}
}

// withSource marks the click for the receiving side; an address that already
// carries utm_source is the owner's own choice and is left alone.
func withSource(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if q.Get("utm_source") != "" {
		return raw
	}
	q.Set("utm_source", "fastoshop")
	q.Set("utm_medium", "referral")
	u.RawQuery = q.Encode()
	return u.String()
}

// OutsidePing answers a button's ping: nginx logs the POST, nothing is stored.
// The id is a link of ours, or the name of a platform.
func (s *Storefront) OutsidePing(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "id")
	if raw == database.BuyWB || raw == database.BuyOzon {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || !s.db.OutsideLinkExists(id) {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
