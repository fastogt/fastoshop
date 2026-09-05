package handler

import (
	"encoding/json"
	"net/http"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
)

type priceRulesResponse struct {
	Rules []database.PriceRule `json:"rules"`
	// The coefficient comes from the shop: a wrong one multiplies every price.
	Coefficient float64 `json:"coefficient"`
}

type priceRulesRequest struct {
	Rules []database.PriceRule `json:"rules"`
}

func (h *Handler) GetPriceRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.db.ShopPriceRules()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if rules == nil {
		rules = []database.PriceRule{}
	}
	c, err := h.db.PriceCoefficient()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, priceRulesResponse{Rules: rules, Coefficient: c})
}

// Storing the ladder moves no price: applying it is the "Recompute" button.
func (h *Handler) SetPriceRules(w http.ResponseWriter, r *http.Request) {
	var req priceRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	if err := database.ValidPriceRules(req.Rules); err != nil {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyBadPriceRules))
		return
	}
	if err := h.db.SetShopPriceRules(req.Rules); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	h.GetPriceRules(w, r)
}
