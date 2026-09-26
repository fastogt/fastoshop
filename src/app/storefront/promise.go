package storefront

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
)

// kPromiseCookie remembers that the buyer closed the line; a year is long enough
// that a returning buyer is not nagged and short enough to come back after a change.
const kPromiseCookie = "promise"

// promise is the one line every page carries: free delivery from a sum, how fast
// the shop ships, how long the buyer has to return. Built from the same numbers
// the card publishes as data, so the page and the markup cannot disagree.
func promise(shop *database.Settings) string {
	parts := make([]string, 0, 3)
	if shop.DeliveryFreeFrom > 0 {
		parts = append(parts, "Доставка бесплатно от "+priceStr(shop.DeliveryFreeFrom)+" "+shop.Sign())
	}
	if shop.DeliveryDays > 0 {
		parts = append(parts, "доставим за "+plural(shop.DeliveryDays, "день", "дня", "дней"))
	}
	if shop.ReturnDays > 0 {
		parts = append(parts, "возврат в течение "+plural(shop.ReturnDays, "дня", "дней", "дней"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// plural picks the Russian form: 1 день, 2 дня, 5 дней.
func plural(n int, one, few, many string) string {
	word := many
	switch {
	case n%10 == 1 && n%100 != 11:
		word = one
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		word = few
	}
	return strconv.Itoa(n) + " " + word
}

// PromiseOff hides the line for a year and returns the buyer to the page they read.
func (s *Storefront) PromiseOff(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: kPromiseCookie, Value: "1", Path: "/",
		MaxAge: 365 * 24 * 60 * 60, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	back := r.URL.Query().Get("back")
	// Only our own address: a redirect target taken from the query is an open redirect.
	if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") {
		back = "/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// promiseFor fills the line unless this buyer has already closed it.
func (v *pageVM) promiseFor(r *http.Request) {
	if _, err := r.Cookie(kPromiseCookie); err == nil {
		return
	}
	v.Promise = promise(v.Shop)
	v.PromiseBack = url.QueryEscape(r.URL.RequestURI())
}
