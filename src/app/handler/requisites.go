package handler

import (
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/storefront"
)

// OrderRequisites serves the file a company attached to its order. Behind the
// session on purpose: the directory holds bank accounts and is not published,
// unlike everything under /uploads/.
func (h *Handler) OrderRequisites(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpjson.WriteBadRequest(w, "bad id")
		return
	}
	list, err := h.db.ListOrders()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	for _, o := range list {
		if o.ID != id {
			continue
		}
		if o.RequisitesFile == "" {
			http.NotFound(w, r)
			return
		}
		// Base only: the stored name is generated, but a path from the database
		// must never be able to climb out of its directory.
		name := filepath.Base(o.RequisitesFile)
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		http.ServeFile(w, r, filepath.Join(storefront.RequisitesDir(h.uploadsDir), name))
		return
	}
	http.NotFound(w, r)
}
