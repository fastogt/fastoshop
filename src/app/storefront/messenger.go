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
// Kind is what the buy box draws: "msg" is a messenger button, anything else a way out.
type orderLinkVM struct {
	Kind string
	// Label is what the button says; URL is our own /go/ address, which counts and redirects.
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
	var out []orderLinkVM
	for _, m := range []struct{ kind, label string }{{kTelegram, "Telegram"}, {kWhatsApp, "WhatsApp"}} {
		if contactURL(shop, m.kind) != "" {
			out = append(out, orderLinkVM{Label: m.label, URL: "/go/" + m.kind})
		}
	}
	return out
}

// contactURL is a bare account: the footer carries no product to fill a message with.
func contactURL(shop *database.Settings, kind string) string {
	if shop == nil {
		return ""
	}
	switch kind {
	case kTelegram:
		if h := telegramHandle(shop.Telegram); h != "" {
			return "https://t.me/" + h
		}
	case kWhatsApp:
		if n := whatsappNumber(shop.WhatsApp); n != "" {
			return "https://wa.me/" + n
		}
	}
	return ""
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

// The button goes through us: a blocker drops a ping, never the navigation itself.
func orderLinks(shop *database.Settings, p *database.Product) []orderLinkVM {
	if shop == nil || p == nil {
		return nil
	}
	var out []orderLinkVM
	for _, m := range []struct{ kind, label string }{
		{kTelegram, "Заказать в Telegram"},
		{kWhatsApp, "Заказать в WhatsApp"},
	} {
		if contactURL(shop, m.kind) != "" {
			out = append(out, orderLinkVM{Kind: database.BuyMsg, Label: m.label,
				URL: "/go/" + m.kind + "/" + url.PathEscape(p.Slug)})
		}
	}
	return out
}

// buyBox is the page's buttons in the order the owner arranged them. A button
// whose card or account is missing simply does not appear: a dead button is
// worse than none.
func buyBox(names []string, shop *database.Settings, p *database.Product,
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
			out = append(out, orderLinks(shop, p)...)
		case name == database.BuyWB && nmID > 0:
			out = append(out, orderLinkVM{Kind: database.BuyWB, Label: "Купить на Wildberries",
				URL: "/go/out/wb/" + url.PathEscape(p.Slug)})
		case name == database.BuyOzon && sku > 0:
			out = append(out, orderLinkVM{Kind: database.BuyOzon, Label: "Купить на Ozon",
				URL: "/go/out/ozon/" + url.PathEscape(p.Slug)})
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
// their Instagram. The address stays theirs and is looked up again on the click.
func outsideLink(l database.OutsideLink, slug string) orderLinkVM {
	return orderLinkVM{Kind: database.BuyLink, Label: l.Label,
		URL: fmt.Sprintf("/go/out/%d/%s", l.ID, url.PathEscape(slug))}
}

// platformURL is the card's public address; zero means no card and no button.
func platformURL(platform string, nmID, sku int64) string {
	switch {
	case platform == database.BuyWB && nmID > 0:
		return fmt.Sprintf("https://www.wildberries.ru/catalog/%d/detail.aspx", nmID)
	case platform == database.BuyOzon && sku > 0:
		return fmt.Sprintf("https://www.ozon.ru/product/%d/", sku)
	}
	return ""
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

// goTo is the count: nginx logs the GET, and nothing is stored. The target is
// always built here from our own data, never read from the request, so this is
// not an open redirect.
func goTo(w http.ResponseWriter, r *http.Request, target string) {
	if target == "" {
		http.NotFound(w, r)
		return
	}
	// Every click must reach us again, and no search engine may keep the hop.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	http.Redirect(w, r, target, http.StatusFound)
}

// GoMessenger opens the messenger with the order already written.
func (s *Storefront) GoMessenger(w http.ResponseWriter, r *http.Request) {
	p, err := s.db.GetVisibleProductBySlug(chi.URLParam(r, "slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	goTo(w, r, messengerURL(s.shop(), p, s.baseURL+"/p/"+p.Slug, chi.URLParam(r, "messenger")))
}

// GoContact opens the shop's account from the footer, with no product to name.
func (s *Storefront) GoContact(w http.ResponseWriter, r *http.Request) {
	goTo(w, r, contactURL(s.shop(), chi.URLParam(r, "messenger")))
}

// GoOutside leaves the shop for a card on a marketplace or the seller's own link.
// The id is a link of ours, or the name of a platform.
func (s *Storefront) GoOutside(w http.ResponseWriter, r *http.Request) {
	slug, id := chi.URLParam(r, "slug"), chi.URLParam(r, "id")
	target := ""
	if id == database.BuyWB || id == database.BuyOzon {
		if p, err := s.db.GetVisibleProductBySlug(slug); err == nil {
			nmID, sku := s.db.PlatformCards(p.ID)
			target = platformURL(id, nmID, sku)
		}
	} else if n, err := strconv.ParseInt(id, 10, 64); err == nil {
		target = s.db.OutsideLinkURL(n, slug)
	}
	if target != "" {
		target = withSource(target)
	}
	goTo(w, r, target)
}
