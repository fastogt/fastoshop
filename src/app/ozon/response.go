package ozon

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"
	"gitlab.com/fastogt/gofastogt/gofastogt"
	"gitlab.com/fastogt/gofastogt/gofastogt/errorgt"
)

// A deliberate copy of the helpers from app/handler: a platform tab is a
// vertical slice, and the package must not depend on app/handler for three
// lines. Exporting them from handler would couple the two both ways for nothing.

func writeOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(gofastogt.NewOkResponse(data))
}

func writeError(w http.ResponseWriter, status int, err gofastogt.ErrorJson) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gofastogt.NewErrorResponse(err))
}

// writeInternalError never leaks err outwards: the text of SQL errors and paths
// is a leak, and the repository is public. Details go to the log only.
func writeInternalError(w http.ResponseWriter, err error) {
	log.Errorf("ozon: internal error: %v", err)
	details := "internal error"
	writeError(w, http.StatusInternalServerError, errorgt.MakeErrorJsonInternal(&details))
}

func writeBadRequest(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, errorgt.MakeErrorJsonInvalidInput(&msg))
}

func writeNotFound(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusNotFound, errorgt.MakeErrorJsonInvalidInput(&msg))
}
