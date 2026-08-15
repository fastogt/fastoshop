package handler

import (
	"encoding/json"
	"net/http"
	"strings"
)

// categoryRow is a node of the tree as the admin list shows it: the path, how
// many goods hang below it and the owner's own text for the page.
type categoryRow struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
	Body  string `json:"body"`
}

type categoryListResponse struct {
	Categories []categoryRow `json:"categories"`
	Total      int           `json:"total"`
}

// CategoryList is the whole tree in one response, unpaged on purpose: a
// catalogue of 24 000 products has hundreds of categories, not thousands, and
// the screen is a place to look for the ones still without a text.
func (h *Handler) CategoryList(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.db.VisibleCategories()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	texts, err := h.db.CategoryTexts()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	rows := make([]categoryRow, 0, len(nodes))
	for _, n := range nodes {
		if q != "" && !strings.Contains(strings.ToLower(n.Path), q) {
			continue
		}
		rows = append(rows, categoryRow{Path: n.Path, Count: n.Count, Body: texts[n.Path]})
	}
	writeOK(w, categoryListResponse{Categories: rows, Total: len(rows)})
}

type categoryTextRequest struct {
	Path string `json:"path"`
	Body string `json:"body"`
}

func (h *Handler) SetCategoryText(w http.ResponseWriter, r *http.Request) {
	var req categoryTextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "bad json")
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeBadRequest(w, "empty path")
		return
	}
	if err := h.db.SetCategoryText(req.Path, req.Body); err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, okStatusResponse{Status: "saved"})
}
