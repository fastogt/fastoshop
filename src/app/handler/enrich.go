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

	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
)

// kEnrichTimeout sits just under nginx's 60-second proxy read timeout. One card
// on the AdHunters GPU takes 15-25 seconds, so a synchronous request is honest
// here - ponytail: a background task with progress is the answer only if this
// ever grows into "rewrite the whole catalogue".
const kEnrichTimeout = 55 * time.Second

// A variable, not a constant, so a test can point at a fake AdHunters.
var adHuntersEnrichURL = "https://adhunters.fastolead.com/api/shop/enrich"

type enrichRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Lang        string   `json:"lang"`
	Categories  []string `json:"categories"`
	// Sent raw in stored units: the service converts, a model asked to may err.
	WeightG  *int64 `json:"weight_g,omitempty"`
	LengthMM *int64 `json:"length_mm,omitempty"`
	WidthMM  *int64 `json:"width_mm,omitempty"`
	HeightMM *int64 `json:"height_mm,omitempty"`
}

type enrichResponse struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

// kMaxCategoryBytes bounds the section list by size rather than by count: a
// deep path runs past a hundred characters, and two hundred of them made a
// 55 KB prompt that pushed the answer out of the model's context - it replied
// with nothing usable in three seconds. Measured on a live 24 000-product tree.
// ponytail: a flat budget, filled with the shallowest paths first. Narrowing
// the list by the product's own words is the upgrade if this proves too blunt.
const kMaxCategoryBytes = 4000

type adHuntersEnvelope struct {
	Data enrichResponse `json:"data"`
}

// Caps what we read back: the draft is a few kilobytes.
const kMaxUpstreamBody = 64 << 10

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

// Nothing is written to the database: the owner edits the draft and saves it.
func (h *Handler) EnrichProduct(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpjson.WriteBadRequest(w, "bad id")
		return
	}
	p, err := h.db.GetProduct(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s, err := h.db.GetSettings()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if s.AdHuntersAPIKey == "" {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyNoAIKey))
		return
	}

	// Sections go out only for an unfiled product, and only from the shop's tree.
	var offered []string
	if nodes, err := h.db.VisibleCategories(); err == nil && p.Category == "" {
		paths := make([]string, 0, len(nodes))
		for _, n := range nodes {
			paths = append(paths, n.Path)
		}
		// Shallow first: a top-level section is a usable answer for any product.
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
		WeightG: p.WeightG, LengthMM: p.LengthMM,
		WidthMM: p.WidthMM, HeightMM: p.HeightMM,
	})
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		adHuntersEnrichURL, bytes.NewReader(body))
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.AdHuntersAPIKey)

	client := &http.Client{Timeout: kEnrichTimeout}
	resp, err := client.Do(req)
	if err != nil {
		// Not logged: the transport error carries the request back, key included.
		log.Warnf("enrich: product %d: request to the AI service failed", id)
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyAIUnavailable))
		return
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, kMaxUpstreamBody))
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyAIKeyRejected))
		return
	case http.StatusPaymentRequired:
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyAINoCredits))
		return
	default:
		// The service's own words, untranslated like every message from a platform.
		log.Warnf("enrich: product %d: AI service answered %d: %s",
			id, resp.StatusCode, upstreamMessage(raw))
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyAIUnavailable)+": "+upstreamMessage(raw))
		return
	}

	var env adHuntersEnvelope
	if err := json.Unmarshal(raw, &env); err != nil ||
		env.Data.Title == "" || env.Data.Description == "" {
		log.Warnf("enrich: product %d: unusable answer from the AI service", id)
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyAIUnavailable))
		return
	}
	// Checked here too: a section the shop never offered must not reach the form.
	if !slices.Contains(offered, env.Data.Category) {
		env.Data.Category = ""
	}
	httpjson.WriteOK(w, env.Data)
}
