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
// FaviconICO answers the address every crawler asks for before it has read a
// line of the page. Google looks for the tab icon here as well as in the head,
// and a 404 leaves the shop with a globe next to its snippet while the
// competition shows its own mark. A redirect, not a copy: the icon itself is
// either the owner's logo or the letter drawn below, and one of them must not
// silently drift from the other.
func (s *Storefront) FaviconICO(w http.ResponseWriter, r *http.Request) {
	target := "/favicon.svg"
	if logo := s.shop().Logo; logo != "" {
		target = "/uploads/" + logo
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

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

// noPhoto stands in for a product without pictures. A supplier feed always has
// a few, and an empty column half a page tall next to the price reads as a
// broken page rather than a missing photo. Served as a file, not inlined: a
// catalogue page holds sixty cards, and sixty copies of the same data URI would
// weigh more than the request they save.
const kNoPhotoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 400">` +
	`<rect width="400" height="400" fill="#f4f1ee"/>` +
	`<g fill="none" stroke="#c9bfb6" stroke-width="10" stroke-linejoin="round">` +
	`<rect x="120" y="150" width="160" height="120" rx="12"/>` +
	`<path d="M170 150l14-22h32l14 22"/><circle cx="200" cy="212" r="34"/></g></svg>`

func (s *Storefront) NoPhoto(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	// The picture never changes, so it may sit in the cache as long as it likes.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write([]byte(kNoPhotoSVG))
}
