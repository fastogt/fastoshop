package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
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

// A shop taking catalogues from several suppliers converts one of them from
// another currency. That conversion belongs to the run: stored as the shop's, it
// would reprice everybody else's goods on their next import.
func TestImportKeepsShopCoefficient(t *testing.T) {
	h := newTestHandler(t)
	// The coefficient lives in the settings row, and that row is the owner's.
	if err := h.db.CreateSettings(&database.Settings{OwnerEmail: "o@example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := h.db.SetPriceCoefficient(1.4); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	// Port 1 refuses at once: the job starts and dies, which is all this needs.
	h.ImportRun(w, httptest.NewRequest("POST", "/api/import/run", strings.NewReader(
		`{"source":"yml","url":"http://127.0.0.1:1/feed.xml","supplier":"marinesh","coefficient":0.03572}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("run: %d %s", w.Code, w.Body.String())
	}
	c, err := h.db.PriceCoefficient()
	if err != nil || c != 1.4 {
		t.Fatalf("coefficient: %v %v", c, err)
	}
}
