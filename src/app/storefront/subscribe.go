package storefront

import (
	"html/template"
	"net/http"
	"net/url"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// Subscribe takes the address from the footer form and returns the buyer to the
// page they were reading: a subscription must not cost them their place.
func (s *Storefront) Subscribe(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	back := backTo(r.Referer(), s.baseURL)
	// Stored word for word: the page text is edited over time, the consent was given once.
	consent := i18n.Page(s.shop().Lang, "consent_subscribe")
	if err := s.db.Subscribe(email, database.SubscribeFooter, consent); err != nil {
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
