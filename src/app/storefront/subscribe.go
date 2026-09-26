package storefront

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
)

// kSubscribeConsent is stored with the address, word for word. The page text is
// edited over time; what the buyer agreed to is what was in front of them.
const kSubscribeConsent = "Согласен получать письма магазина: новинки, скидки и полезные материалы. " +
	"Принимаю политику обработки персональных данных. Отписаться можно ссылкой из любого письма."

// kOrderConsent is what the buyer accepts to place an order; stored with it,
// because both documents are edited and the consent has to be shown as given.
const kOrderConsent = "Принимаю условия публичной оферты и даю согласие на обработку персональных данных."

// kOrderSubscribeConsent is the wording of the optional tick in the order form.
const kOrderSubscribeConsent = "Хочу получать письма магазина: новинки, скидки и полезные материалы. " +
	"Отписаться можно ссылкой из любого письма."

// Subscribe takes the address from the footer form and returns the buyer to the
// page they were reading: a subscription must not cost them their place.
func (s *Storefront) Subscribe(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	back := backTo(r.Referer(), s.baseURL)
	if err := s.db.Subscribe(email, database.SubscribeFooter, kSubscribeConsent); err != nil {
		log.Warnf("subscribe: %v", err)
		http.Redirect(w, r, back+"?subscribe=bad", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back+"?subscribed="+url.QueryEscape(email), http.StatusSeeOther)
}

// Unsubscribe is the one click promised in every letter: no login, no form.
func (s *Storefront) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	if token != "" {
		if err := s.db.Unsubscribe(token); err != nil {
			log.Warnf("unsubscribe: %v", err)
		}
	}
	data := pageVM{Shop: s.shop(), BaseURL: s.baseURL, CSS: template.CSS(styleCSS),
		CartCount: cartCount(r), NoIndex: true, Unsubscribed: true}
	data.fromRequest(r)
	if err := s.unsubscribeTpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Errorf("render unsubscribe: %v", err)
	}
}

// backTo keeps the redirect inside the shop: a Referer is the buyer's browser
// talking, and anything from there may point anywhere.
func backTo(referer, base string) string {
	if referer == "" {
		return "/"
	}
	if rest, ok := strings.CutPrefix(referer, base); ok {
		if rest == "" {
			return "/"
		}
		if strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, "//") {
			return strings.Split(rest, "?")[0]
		}
	}
	return "/"
}
