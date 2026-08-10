package storefront

import (
	"fmt"
	"html/template"
	"net/http"
	"unicode"
)

// kFaviconAccent matches --accent in static/style.css: the tab icon should look
// like it belongs to the same shop as the page behind it.
const kFaviconAccent = "#b4532a"

// favicon draws the shop's initial on the accent colour. The storefront belongs
// to the seller, not to us, so putting the fastoshop mark on their tab would be
// branding their shop with someone else's logo. A generated letter needs no
// upload flow and no asset to ship, and it beats the blank square a browser
// shows today — Google puts this icon next to the snippet on mobile.
//
// ponytail: a real logo upload is the obvious next step; until a seller asks,
// the initial is what a shop with no designer would have anyway.
func (s *Storefront) Favicon(w http.ResponseWriter, r *http.Request) {
	letter := "?"
	for _, ch := range s.shop().ShopName {
		if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
			letter = string(unicode.ToUpper(ch))
			break
		}
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	// A day is long enough that a rename shows up the same day, and short
	// enough that the icon is not fetched on every page view.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">`+
		`<rect width="64" height="64" rx="14" fill="%s"/>`+
		`<text x="32" y="33" fill="#fff" font-family="system-ui,sans-serif" `+
		`font-size="38" font-weight="700" text-anchor="middle" `+
		`dominant-baseline="central">%s</text></svg>`,
		kFaviconAccent, template.HTMLEscapeString(letter))
}
