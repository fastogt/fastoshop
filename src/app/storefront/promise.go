package storefront

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// kPromiseCookie remembers that the buyer closed the line; a year is long enough
// that a returning buyer is not nagged and short enough to come back after a change.
const kPromiseCookie = "promise"

// promise is the one line every page carries: free delivery from a sum, how fast
// the shop ships, how long the buyer has to return. Built from the same numbers
// the card publishes as data, so the page and the markup cannot disagree.
func promise(shop *database.Settings) string {
	// Delivery is what makes the line worth pinning and worth closing once. A
	// shop that states only a return window has nothing to promise here: that
	// fact belongs in the footer and on the card, not in a bar over the page.
	if shop.DeliveryFreeFrom == 0 && shop.DeliveryDays == 0 {
		return ""
	}
	parts := make([]string, 0, 3)
	if shop.DeliveryFreeFrom > 0 {
		parts = append(parts, i18n.Page(shop.Lang, "promise_free_from", priceStr(shop.DeliveryFreeFrom)+" "+shop.Sign()))
	}
	if shop.DeliveryDays > 0 {
		parts = append(parts, i18n.PageN(shop.Lang, "promise_days", shop.DeliveryDays))
	}
	if shop.ReturnDays > 0 {
		parts = append(parts, i18n.PageN(shop.Lang, "promise_return", shop.ReturnDays))
	}
	return strings.Join(parts, " · ")
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

// fromRequest fills what the page takes from the request itself: the promise
// line unless this buyer closed it, and the answer to the footer subscription.
func (v *pageVM) fromRequest(r *http.Request) {
	q := r.URL.Query()
	v.Subscribed, v.BadEmail = q.Get("subscribed"), q.Get("subscribe") == "bad"
	// The footer states the terms whatever the buyer did with the pinned line.
	v.Terms = terms(v.Shop)
	if _, err := r.Cookie(kPromiseCookie); err == nil {
		return
	}
	v.Promise = promise(v.Shop)
	v.PromiseBack = url.QueryEscape(r.URL.RequestURI())
}

// terms is the footer's own line: the same facts as the promise, including a
// shop that only states a return window.
func terms(shop *database.Settings) string {
	parts := make([]string, 0, 3)
	if shop.DeliveryFreeFrom > 0 {
		parts = append(parts, i18n.Page(shop.Lang, "terms_free_from", priceStr(shop.DeliveryFreeFrom)+" "+shop.Sign()))
	}
	if shop.DeliveryDays > 0 {
		parts = append(parts, i18n.PageN(shop.Lang, "terms_days", shop.DeliveryDays))
	}
	if shop.ReturnDays > 0 {
		parts = append(parts, i18n.PageN(shop.Lang, "terms_return", shop.ReturnDays))
	}
	return strings.Join(parts, " · ")
}
