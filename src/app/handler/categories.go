package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
)

type categoryListResponse struct {
	Categories []database.Category `json:"categories"`
}

// The tree comes in one response, unpaged: hundreds of categories, not thousands.
func (h *Handler) CategoryList(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.db.Tree()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, categoryListResponse{Categories: nodes})
}

type categoryCreateRequest struct {
	// Parent may be empty: then the category is a root one.
	Parent string `json:"parent"`
	Name   string `json:"name"`
}

func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "bad json")
		return
	}
	path := database.JoinCategory(req.Parent, req.Name)
	if strings.TrimSpace(req.Name) == "" || path == "" {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyCategoryNoName))
		return
	}
	if err := h.db.CreateCategory(path); err != nil {
		h.writeCategoryError(w, err)
		return
	}
	httpjson.WriteOK(w, categoryPathResponse{Path: path})
}

// Pointer fields: renaming a category must not silently unhide it.
type categoryUpdateRequest struct {
	Path string `json:"path"`
	// An empty Parent means the root; both rewrite the paths of everything below.
	Name     *string `json:"name"`
	Parent   *string `json:"parent"`
	Position *int    `json:"position"`
	Hidden   *bool   `json:"hidden"`
	Body     *string `json:"body"`
}

func (h *Handler) UpdateCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "bad json")
		return
	}
	path := database.NormalizePath(req.Path)
	if path == "" {
		httpjson.WriteBadRequest(w, "empty path")
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
			httpjson.WriteBadRequest(w, h.msg(i18n.KeyCategoryNoName))
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
			httpjson.WriteInternalError(w, err)
			return
		}
	}
	if req.Hidden != nil {
		if err := h.db.SetCategoryHidden(path, *req.Hidden); err != nil {
			httpjson.WriteInternalError(w, err)
			return
		}
	}
	if req.Body != nil {
		if err := h.db.SetCategoryText(path, *req.Body); err != nil {
			httpjson.WriteInternalError(w, err)
			return
		}
	}
	httpjson.WriteOK(w, categoryPathResponse{Path: path})
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

// Products and subcategories move up to the parent; the old address answers 301.
func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	path := database.NormalizePath(r.URL.Query().Get("path"))
	if path == "" {
		httpjson.WriteBadRequest(w, "empty path")
		return
	}
	if err := h.db.DeleteCategory(path); err != nil {
		h.writeCategoryError(w, err)
		return
	}
	httpjson.WriteOK(w, okStatusResponse{Status: "deleted"})
}

func (h *Handler) writeCategoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, database.ErrCategoryExists):
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyCategoryExists))
	case errors.Is(err, database.ErrCategorySlugTaken):
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyCategorySlugTaken))
	default:
		httpjson.WriteInternalError(w, err)
	}
}
