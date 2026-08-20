package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fastogt/fastoshop/app/i18n"
)

// kEnrichTimeout sits just under nginx's 60-second proxy read timeout. One card
// on the AdHunters GPU takes 15-25 seconds, so a synchronous request is honest
// here — ponytail: a background task with progress is the answer only if this
// ever grows into "rewrite the whole catalogue".
const kEnrichTimeout = 55 * time.Second

// adHuntersEnrichURL is a variable rather than a constant so a test can point
// the handler at a fake AdHunters instead of the live one.
var adHuntersEnrichURL = "https://adhunters.fastolead.com/api/shop/enrich"

type enrichRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Lang        string   `json:"lang"`
	Categories  []string `json:"categories"`
}

type enrichResponse struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// kMaxOfferedCategories bounds the prompt: a catalogue tree can run to
// hundreds of paths, and sending all of them makes every request slower for a
// suggestion the owner reviews anyway.
// ponytail: a plain cap. Narrowing the list by the product's own words is the
// upgrade if a big tree turns out to need it.
const kMaxOfferedCategories = 200

type adHuntersEnvelope struct {
	Data enrichResponse `json:"data"`
}

// EnrichProduct asks AdHunters to rewrite one card and hands the draft back to
// the admin. Nothing is written to the database: the owner reads what the model
// produced, edits it and saves it themselves — a generated text that saved
// itself would put invented properties on the storefront under their name.
func (h *Handler) EnrichProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeBadRequest(w, "bad id")
		return
	}
	p, err := h.db.GetProduct(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s, err := h.db.GetSettings()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if s.AdHuntersAPIKey == "" {
		writeBadRequest(w, h.msg(i18n.KeyNoAIKey))
		return
	}

	// The sections the shop already has: the model may pick one, never write
	// one of its own. Guessing a tree is the onboarding tool's job, not the
	// shop's — a made-up section is a landing page that does not exist.
	var offered []string
	if nodes, err := h.db.VisibleCategories(); err == nil {
		for _, n := range nodes {
			offered = append(offered, n.Path)
		}
		if len(offered) > kMaxOfferedCategories {
			offered = offered[:kMaxOfferedCategories]
		}
	}

	body, err := json.Marshal(enrichRequest{
		Title: p.Title, Description: p.Description,
		Category: p.Category, Lang: s.Lang, Categories: offered,
	})
	if err != nil {
		writeInternalError(w, err)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		adHuntersEnrichURL, bytes.NewReader(body))
	if err != nil {
		writeInternalError(w, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.AdHuntersAPIKey)

	client := &http.Client{Timeout: kEnrichTimeout}
	resp, err := client.Do(req)
	if err != nil {
		// The transport error is not passed on: it carries the request back,
		// headers included, and the key must not reach a log or a browser.
		writeBadRequest(w, h.msg(i18n.KeyAIUnavailable))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		writeBadRequest(w, h.msg(i18n.KeyAIKeyRejected))
		return
	case http.StatusPaymentRequired:
		writeBadRequest(w, h.msg(i18n.KeyAINoCredits))
		return
	default:
		writeBadRequest(w, h.msg(i18n.KeyAIUnavailable))
		return
	}

	var env adHuntersEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil ||
		env.Data.Title == "" || env.Data.Description == "" {
		writeBadRequest(w, h.msg(i18n.KeyAIUnavailable))
		return
	}
	// Checked here too, not only on the other side: this is the shop's own
	// tree, and a section it never offered must not reach the admin form.
	if !slices.Contains(offered, env.Data.Category) {
		env.Data.Category = ""
	}
	writeOK(w, env.Data)
}
