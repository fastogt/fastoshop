package storefront

import (
	"fmt"
	"html/template"
	"net/http"
	"unicode"
)

// kFaviconAccent matches --accent in static/style.css.
const kFaviconAccent = "#b4532a"

// ponytail: a real logo upload is the obvious next step; until a seller asks,
// the initial is what a shop with no designer would have anyway.
//
// Crawlers fetch /favicon.ico whatever the head says; redirect so it cannot drift.
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
	// A day: a shop rename shows up the same day, without a fetch per page view.
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">`+
		`<rect width="64" height="64" rx="14" fill="%s"/>`+
		`<text x="32" y="33" fill="#fff" font-family="system-ui,sans-serif" `+
		`font-size="38" font-weight="700" text-anchor="middle" `+
		`dominant-baseline="central">%s</text></svg>`,
		kFaviconAccent, template.HTMLEscapeString(letter))
}

// A file, not a data URI: sixty cards a page would repeat the same blob sixty times.
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
