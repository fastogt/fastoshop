package handler

import (
	"encoding/json"
	"net/http"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

type priceRulesResponse struct {
	Rules []database.PriceRule `json:"rules"`
	// Coefficient is what the catalogue actually runs on. The form used to open
	// with a hard-coded 1 while the shop was on 0.0466: pressing "Recompute"
	// there would have multiplied twenty thousand prices by twenty-one.
	Coefficient float64 `json:"coefficient"`
}

type priceRulesRequest struct {
	Rules []database.PriceRule `json:"rules"`
}

// GetPriceRules returns the shop's own markup ladder. The channels have had one
// since they were written; the storefront made do with a single multiplier,
// which lies at both ends of a catalogue — it leaves nothing on a seven-rouble
// sieve and prices a steamer above the brand's own store.
func (h *Handler) GetPriceRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.db.ShopPriceRules()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if rules == nil {
		rules = []database.PriceRule{}
	}
	c, err := h.db.PriceCoefficient()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, priceRulesResponse{Rules: rules, Coefficient: c})
}

// SetPriceRules stores the ladder without touching a single price: applying it
// is the "Recompute" button, so the owner can lay the bands out and look at them
// before twenty thousand products move.
func (h *Handler) SetPriceRules(w http.ResponseWriter, r *http.Request) {
	var req priceRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	if err := database.ValidPriceRules(req.Rules); err != nil {
		writeBadRequest(w, h.msg(i18n.KeyBadPriceRules))
		return
	}
	if err := h.db.SetShopPriceRules(req.Rules); err != nil {
		writeInternalError(w, err)
		return
	}
	h.GetPriceRules(w, r)
}
