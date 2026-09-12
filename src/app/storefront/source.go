package storefront

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const kSourceCookie = "src"

// Thirty days: the buying cycle here is minutes, and a longer window credits an
// order to a visit nobody remembers.
const kSourceTTL = 30 * 24 * time.Hour

// The landing path is stored whole; a query string is not - it carries the
// buyer's own search terms and has no place in an order row.
const kMaxSourceLen = 200

// orderSource is what the first touch left behind. Written once and never
// overwritten: an order belongs to the visit that found the shop, not to the
// last time the buyer came back to finish it.
type orderSource struct {
	Ref  string `json:"ref,omitempty"`
	UTM  string `json:"utm,omitempty"`
	Land string `json:"land,omitempty"`
	At   int64  `json:"t,omitempty"`
}

// TrackSource records where a visitor came from, on their very first request.
//
// Reading the referer when the order is placed would answer "/cart" every time:
// by then the buyer has walked the catalogue and the referer is our own host.
// Google is visible in the first request and nowhere else, which is why this
// has to be a cookie rather than a lookup at checkout.
func (s *Storefront) TrackSource(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || hasSource(r) {
			next.ServeHTTP(w, r)
			return
		}
		src := orderSource{Land: clip(r.URL.Path), At: time.Now().Unix()}
		src.Ref = externalHost(r.Referer(), r.Host)
		src.UTM = clip(utmOf(r.URL.Query()))
		if src.Ref == "" && src.UTM == "" {
			next.ServeHTTP(w, r)
			return
		}
		if b, err := json.Marshal(src); err == nil {
			http.SetCookie(w, &http.Cookie{Name: kSourceCookie, Path: "/",
				Value: url.QueryEscape(string(b)), HttpOnly: true,
				SameSite: http.SameSiteLaxMode, MaxAge: int(kSourceTTL.Seconds())})
		}
		next.ServeHTTP(w, r)
	})
}

func hasSource(r *http.Request) bool {
	_, err := r.Cookie(kSourceCookie)
	return err == nil
}

// externalHost returns the referring host, empty for our own pages and for the
// direct visits that have no referer at all.
func externalHost(referer, self string) string {
	if referer == "" {
		return ""
	}
	u, err := url.Parse(referer)
	if err != nil || u.Host == "" || strings.EqualFold(u.Host, self) {
		return ""
	}
	return u.Host
}

func utmOf(q url.Values) string {
	parts := make([]string, 0, 5)
	// term и content нужны не для красоты: в контексте именно они отвечают на
	// вопрос, какая фраза и какое объявление привели заказ.
	for _, k := range []string{"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content"} {
		if v := strings.TrimSpace(q.Get(k)); v != "" {
			parts = append(parts, k[4:]+"="+v)
		}
	}
	return strings.Join(parts, " ")
}

func clip(s string) string {
	r := []rune(s)
	if len(r) > kMaxSourceLen {
		return string(r[:kMaxSourceLen])
	}
	return s
}

// sourceOf renders the first touch as one line for the order row: "google.com
// /p/basket", or "direct" when the visitor arrived typing the address.
func sourceOf(r *http.Request) string {
	c, err := r.Cookie(kSourceCookie)
	if err != nil {
		return "direct"
	}
	raw, err := url.QueryUnescape(c.Value)
	if err != nil {
		return "direct"
	}
	var src orderSource
	if err := json.Unmarshal([]byte(raw), &src); err != nil {
		return "direct"
	}
	head := src.Ref
	if src.UTM != "" {
		if head != "" {
			head += " "
		}
		head += src.UTM
	}
	if head == "" {
		return "direct"
	}
	if src.Land != "" {
		return head + " " + src.Land
	}
	return head
}
