package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImportValidation(t *testing.T) {
	h := newTestHandler(t)
	w := httptest.NewRecorder()
	h.ImportCheck(w, httptest.NewRequest("POST", "/api/import/check",
		strings.NewReader(`{"source":"ozon"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no creds: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ImportRun(w, httptest.NewRequest("POST", "/api/import/run",
		strings.NewReader(`{"source":"etsy","token":"x"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown source: %d", w.Code)
	}
}
