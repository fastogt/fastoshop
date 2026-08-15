package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

type categoryListResponse struct {
	Categories []database.Category `json:"categories"`
}

// CategoryList is the whole tree in one response, unpaged on purpose: a
// catalogue of 24 000 products has hundreds of categories, not thousands, and a
// tree drawn one page at a time is not a tree.
func (h *Handler) CategoryList(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.db.Tree()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, categoryListResponse{Categories: nodes})
}

type categoryCreateRequest struct {
	// Parent may be empty: then the category is a root one.
	Parent string `json:"parent"`
	Name   string `json:"name"`
}

func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "bad json")
		return
	}
	path := database.JoinCategory(req.Parent, req.Name)
	if strings.TrimSpace(req.Name) == "" || path == "" {
		writeBadRequest(w, h.msg(i18n.KeyCategoryNoName))
		return
	}
	if err := h.db.CreateCategory(path); err != nil {
		h.writeCategoryError(w, err)
		return
	}
	writeOK(w, categoryPathResponse{Path: path})
}

// categoryUpdateRequest carries only what changed: the fields are pointers so
// that renaming a category cannot silently unhide it.
type categoryUpdateRequest struct {
	Path string `json:"path"`
	// Name renames the node in place; Parent moves it (an empty string means the
	// root). Both rewrite the paths of every product and subcategory below.
	Name     *string `json:"name"`
	Parent   *string `json:"parent"`
	Position *int    `json:"position"`
	Hidden   *bool   `json:"hidden"`
	Body     *string `json:"body"`
}

func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "bad json")
		return
	}
	path := database.NormalizePath(req.Path)
	if path == "" {
		writeBadRequest(w, "empty path")
		return
	}
	// The move goes first: everything after it is written to the new address.
	if req.Name != nil || req.Parent != nil {
		parent := database.ParentPath(path)
		if req.Parent != nil {
			parent = database.NormalizePath(*req.Parent)
		}
		name := leafName(path)
		if req.Name != nil {
			name = *req.Name
		}
		to := database.JoinCategory(parent, name)
		if to == "" {
			writeBadRequest(w, h.msg(i18n.KeyCategoryNoName))
			return
		}
		if err := h.db.RenameCategory(path, to); err != nil {
			h.writeCategoryError(w, err)
			return
		}
		path = to
	}
	if req.Position != nil {
		if err := h.db.SetCategoryPosition(path, *req.Position); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	if req.Hidden != nil {
		if err := h.db.SetCategoryHidden(path, *req.Hidden); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	if req.Body != nil {
		if err := h.db.SetCategoryText(path, *req.Body); err != nil {
			writeInternalError(w, err)
			return
		}
	}
	writeOK(w, categoryPathResponse{Path: path})
}

func leafName(path string) string {
	if i := strings.LastIndex(path, database.CategorySep); i >= 0 {
		return path[i+1:]
	}
	return path
}

type categoryPathResponse struct {
	Path string `json:"path"`
}

// DeleteCategory removes the shelf, not what stands on it: products and
// subcategories move up to the parent, and the old address answers 301.
func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	path := database.NormalizePath(r.URL.Query().Get("path"))
	if path == "" {
		writeBadRequest(w, "empty path")
		return
	}
	if err := h.db.DeleteCategory(path); err != nil {
		h.writeCategoryError(w, err)
		return
	}
	writeOK(w, okStatusResponse{Status: "deleted"})
}

// writeCategoryError renders the two conflicts the owner can cause in their own
// language; anything else is ours to fix and goes out as a 500.
func (h *Handler) writeCategoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, database.ErrCategoryExists):
		writeBadRequest(w, h.msg(i18n.KeyCategoryExists))
	case errors.Is(err, database.ErrCategorySlugTaken):
		writeBadRequest(w, h.msg(i18n.KeyCategorySlugTaken))
	default:
		writeInternalError(w, err)
	}
}
