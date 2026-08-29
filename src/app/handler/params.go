package handler

import (
	"encoding/json"
	"net/http"
)

// One row per characteristic the catalogue states: the name as its source wrote
// it, and whether a buyer sees it. Which of a marketplace card's fields belong
// on a page is the owner's decision — the shop keeps all of them either way,
// because a channel needs the ones a buyer does not.
type paramRow struct {
	Name   string `json:"name"`
	Hidden bool   `json:"hidden"`
}

type paramVisibilityResponse struct {
	Params []paramRow `json:"params"`
}

// Hidden names only: the request states the exceptions, not the whole world.
type paramVisibilityRequest struct {
	Hidden []string `json:"hidden"`
}

func (h *Handler) GetParamVisibility(w http.ResponseWriter, r *http.Request) {
	names, err := h.db.CatalogParamNames()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	hidden, err := h.db.HiddenParams()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	res := paramVisibilityResponse{Params: make([]paramRow, 0, len(names))}
	for _, n := range names {
		res.Params = append(res.Params, paramRow{Name: n, Hidden: hidden[n]})
	}
	writeOK(w, res)
}

func (h *Handler) SetParamVisibility(w http.ResponseWriter, r *http.Request) {
	var req paramVisibilityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	if err := h.db.SetHiddenParams(req.Hidden); err != nil {
		writeInternalError(w, err)
		return
	}
	h.GetParamVisibility(w, r)
}
