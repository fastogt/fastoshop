package handler

import (
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"
	"gitlab.com/fastogt/gofastogt/gofastogt"
	"gitlab.com/fastogt/gofastogt/gofastogt/errorgt"
)

func writeOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(gofastogt.NewOkResponse(data))
}

func writeError(w http.ResponseWriter, status int, err gofastogt.ErrorJson) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(gofastogt.NewErrorResponse(err))
}

// writeInternalError не отдаёт err наружу: текст SQL-ошибок и путей — утечка,
// репозиторий публичный. Подробности только в лог.
func writeInternalError(w http.ResponseWriter, err error) {
	log.Errorf("internal error: %v", err)
	details := "internal error"
	writeError(w, http.StatusInternalServerError, errorgt.MakeErrorJsonInternal(&details))
}

func writeBadRequest(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, errorgt.MakeErrorJsonInvalidInput(&msg))
}

func writeUnauthorized(w http.ResponseWriter) {
	msg := "unauthorized"
	writeError(w, http.StatusUnauthorized, errorgt.MakeErrorJsonInvalidInput(&msg))
}
