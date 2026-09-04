// Package httpjson is the one place the gofastogt response envelope is written:
// every handler package answers through these helpers, so the wire format
// cannot drift between tabs.
package httpjson

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"
	"gitlab.com/fastogt/gofastogt/gofastogt"
	"gitlab.com/fastogt/gofastogt/gofastogt/errorgt"
)

func WriteOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(gofastogt.NewOkResponse(data))
}

func writeError(w http.ResponseWriter, status int, err gofastogt.ErrorJson) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gofastogt.NewErrorResponse(err))
}

// WriteInternalError never leaks err outwards: the text of SQL errors and paths
// is a leak, and the repository is public. Details go to the log only.
func WriteInternalError(w http.ResponseWriter, err error) {
	log.Errorf("internal error: %v", err)
	details := "internal error"
	writeError(w, http.StatusInternalServerError, errorgt.MakeErrorJsonInternal(&details))
}

func WriteBadRequest(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, errorgt.MakeErrorJsonInvalidInput(&msg))
}

func WriteUnauthorized(w http.ResponseWriter) {
	msg := "unauthorized"
	writeError(w, http.StatusUnauthorized, errorgt.MakeErrorJsonInvalidInput(&msg))
}

func WriteNotFound(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusNotFound, errorgt.MakeErrorJsonInvalidInput(&msg))
}
