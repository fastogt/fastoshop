package storefront

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// ETag answers a crawler's second visit with 304 instead of the page. A shop of
// twenty thousand cards is re-fetched far more often than it changes, and the
// hash of the body is its own version: nothing has to be invalidated, and a page
// that did change gets a new tag by construction.
//
// Mounted on the storefront router only. The admin API carries the progress
// stream, and buffering a stream is how you stop it from streaming.
func ETag(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		rec := &etagRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		// Only a plain 200 is worth a tag: a redirect or a 404 carries no body
		// a client would want to keep.
		if rec.status == http.StatusOK {
			sum := sha256.Sum256(rec.body.Bytes())
			tag := `"` + hex.EncodeToString(sum[:16]) + `"`
			w.Header().Set("ETag", tag)
			if noneMatch(r.Header.Get("If-None-Match"), tag) {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		w.WriteHeader(rec.status)
		_, _ = w.Write(rec.body.Bytes())
	})
}

// noneMatch compares the way RFC 9110 asks for a conditional GET, which is not
// string equality. nginx rewrites our tag to the weak form when it compresses,
// so what a real client sends back carries a W/ prefix that never came from us;
// a browser may also hold several tags for one address and offer them together.
func noneMatch(header, tag string) bool {
	if header == "" {
		return false
	}
	if strings.TrimSpace(header) == "*" {
		return true
	}
	want := strings.TrimPrefix(tag, "W/")
	for _, candidate := range strings.Split(header, ",") {
		if strings.TrimPrefix(strings.TrimSpace(candidate), "W/") == want {
			return true
		}
	}
	return false
}

// etagRecorder holds the body back until its hash is known; headers the handler
// sets go straight through to the real writer.
type etagRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (e *etagRecorder) WriteHeader(code int)        { e.status = code }
func (e *etagRecorder) Write(p []byte) (int, error) { return e.body.Write(p) }
