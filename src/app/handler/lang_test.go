package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// A fresh shop must have a language: an empty value would arrive in the admin
// and leave the switcher with no selected state.
func TestLangDefaultsAndRoundTrip(t *testing.T) {
	h := newTestHandler(t)
	if _, err := h.db.CreateOwner("a@b.c"); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	h.GetSettings(w, httptest.NewRequest("GET", "/api/settings", nil))
	if !strings.Contains(w.Body.String(), `"lang":"ru"`) {
		t.Fatalf("default lang: %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	h.SetLang(w, httptest.NewRequest("PUT", "/api/settings/lang",
		strings.NewReader(`{"lang":"en"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("set lang: %d %s", w.Code, w.Body.String())
	}
	if h.lang() != i18n.LangEN {
		t.Fatalf("lang not stored: %q", h.lang())
	}

	// Reject an unknown language, otherwise messages silently fall back.
	w = httptest.NewRecorder()
	h.SetLang(w, httptest.NewRequest("PUT", "/api/settings/lang",
		strings.NewReader(`{"lang":"de"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad lang accepted: %d", w.Code)
	}

	// Changing the language must not break the other settings.
	s, err := h.db.GetSettings()
	if err != nil || s.OwnerEmail != "a@b.c" || s.Currency != database.ShopCurrencyRUB {
		t.Fatalf("settings damaged: %+v %v", s, err)
	}
}
