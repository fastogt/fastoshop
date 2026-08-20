package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

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

// kMaxCategoryBytes bounds the section list by size rather than by count: a
// deep path runs past a hundred characters, and two hundred of them made a
// 55 KB prompt that pushed the answer out of the model's context — it replied
// with nothing usable in three seconds. Measured on a live 24 000-product tree.
// ponytail: a flat budget, filled with the shallowest paths first. Narrowing
// the list by the product's own words is the upgrade if this proves too blunt.
const kMaxCategoryBytes = 4000

type adHuntersEnvelope struct {
	Data enrichResponse `json:"data"`
}

// kMaxUpstreamBody caps what we read back: the draft is a few kilobytes, and a
// service answering with something enormous must not become our problem.
const kMaxUpstreamBody = 64 << 10

// upstreamMessage digs the service's own explanation out of its error envelope
// and falls back to nothing rather than to a wall of JSON.
func upstreamMessage(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return ""
	}
	return e.Error.Message
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
		paths := make([]string, 0, len(nodes))
		for _, n := range nodes {
			paths = append(paths, n.Path)
		}
		// Shallow first: a top-level section is a usable answer for any product,
		// while the deepest leaves are the ones a budget can afford to lose.
		slices.SortStableFunc(paths, func(a, b string) int {
			return strings.Count(a, "/") - strings.Count(b, "/")
		})
		budget := kMaxCategoryBytes
		for _, path := range paths {
			if len(path)+1 > budget {
				break
			}
			budget -= len(path) + 1
			offered = append(offered, path)
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
		// The transport error itself is not shown or logged: it carries the
		// request back, headers included, and the key must not leak either way.
		log.Warnf("enrich: product %d: request to the AI service failed", id)
		writeBadRequest(w, h.msg(i18n.KeyAIUnavailable))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, kMaxUpstreamBody))
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		writeBadRequest(w, h.msg(i18n.KeyAIKeyRejected))
		return
	case http.StatusPaymentRequired:
		writeBadRequest(w, h.msg(i18n.KeyAINoCredits))
		return
	default:
		// The service's own words rather than ours: it knows why it refused,
		// and a generic "try again" left the owner — and us — guessing. Not
		// translated, like every other message that came from a platform.
		log.Warnf("enrich: product %d: AI service answered %d: %s",
			id, resp.StatusCode, upstreamMessage(raw))
		writeBadRequest(w, h.msg(i18n.KeyAIUnavailable)+": "+upstreamMessage(raw))
		return
	}

	var env adHuntersEnvelope
	if err := json.Unmarshal(raw, &env); err != nil ||
		env.Data.Title == "" || env.Data.Description == "" {
		log.Warnf("enrich: product %d: unusable answer from the AI service", id)
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
