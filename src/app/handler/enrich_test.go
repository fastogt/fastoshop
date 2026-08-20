package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fastogt/fastoshop/app/database"
)

// fakeAdHunters stands in for the paid service. It also records the key it was
// given, so the test can prove the shop forwards the owner's own key.
func fakeAdHunters(t *testing.T, status int, body string, gotKey *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if gotKey != nil {
				*gotKey = r.Header.Get("Authorization")
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	old := adHuntersEnrichURL
	adHuntersEnrichURL = srv.URL
	t.Cleanup(func() {
		adHuntersEnrichURL = old
		srv.Close()
	})
	return srv
}

// The handler reads {id} from chi, so the request is routed rather than
// hand-built: a router in the test is shorter than faking a route context.
func enrichProduct(t *testing.T, h *Handler, id int64) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/products/{id}/enrich", h.EnrichProduct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST",
		fmt.Sprintf("/products/%d/enrich", id), nil))
	return w
}

// The draft is a draft: whatever the model wrote, the product in the database
// must be untouched until the owner saves it themselves.
func TestEnrichReturnsDraftWithoutWriting(t *testing.T) {
	h := newTestHandler(t)
	if err := h.db.CreateSettings(&database.Settings{OwnerEmail: "o@example.com"}); err != nil {
		t.Fatal(err)
	}
	s, _ := h.db.GetSettings()
	s.AdHuntersAPIKey = "secret-key-a1b2"
	if err := h.db.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	p := &database.Product{Title: `Ерш унитазный с/подст "Шляпа"`, SKU: "E-1",
		Description: "старое описание", Price: 1000}
	if err := h.db.CreateProduct(p); err != nil {
		t.Fatal(err)
	}

	var sentKey string
	fakeAdHunters(t, http.StatusOK,
		`{"data":{"title":"Ёрш для унитаза с подставкой","description":"Длинный человеческий текст."}}`,
		&sentKey)

	w := enrichProduct(t, h, p.ID)
	if w.Code != http.StatusOK {
		t.Fatalf("enrich: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		Data enrichResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Data.Title != "Ёрш для унитаза с подставкой" {
		t.Errorf("draft title: %q", got.Data.Title)
	}
	if sentKey != "Bearer secret-key-a1b2" {
		t.Errorf("owner key not forwarded: %q", sentKey)
	}
	// The key must never come back to the browser.
	if strings.Contains(w.Body.String(), "secret-key") {
		t.Error("the AdHunters key leaked into the response")
	}
	stored, _ := h.db.GetProduct(p.ID)
	if stored.Title != `Ерш унитазный с/подст "Шляпа"` ||
		stored.Description != "старое описание" {
		t.Errorf("the draft was written to the database: %+v", stored)
	}
}

// Sections are offered only for a product that has none — a filed product keeps
// what it has, and the model is never given the chance to move it. For an
// unfiled one, a section the shop does not have cannot reach the form however
// confidently the model writes it.
func TestEnrichOffersSectionsOnlyForAnUnfiledProduct(t *testing.T) {
	h := newTestHandler(t)
	if err := h.db.CreateSettings(&database.Settings{OwnerEmail: "o@example.com"}); err != nil {
		t.Fatal(err)
	}
	s, _ := h.db.GetSettings()
	s.AdHuntersAPIKey = "k"
	if err := h.db.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	filed := &database.Product{Title: "Кружка", SKU: "K-1", Price: 100,
		Category: "Посуда/Кружки"}
	unfiled := &database.Product{Title: "Банка", SKU: "B-1", Price: 100}
	for _, p := range []*database.Product{filed, unfiled} {
		if err := h.db.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
	}

	draft := func(category string) string {
		b, _ := json.Marshal(map[string]any{"data": map[string]string{
			"title": "Кружка стеклянная", "description": "Текст.", "category": category}})
		return string(b)
	}
	var sent enrichRequest
	// What the fake service answers, set by each case just before its call.
	reply := ""
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&sent)
			_, _ = w.Write([]byte(draft(reply)))
		}))
	old := adHuntersEnrichURL
	adHuntersEnrichURL = srv.URL
	defer func() { adHuntersEnrichURL = old; srv.Close() }()

	var got struct {
		Data enrichResponse `json:"data"`
	}

	// Filed: nothing to choose from, so the list is not sent and no section
	// comes back to overwrite a precise path with a vague one.
	reply = "Посуда"
	w := enrichProduct(t, h, filed.ID)
	if len(sent.Categories) != 0 {
		t.Errorf("sections offered for a filed product: %v", sent.Categories)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Data.Category != "" {
		t.Errorf("a filed product was moved: %q", got.Data.Category)
	}

	// Unfiled: the shop's own tree is offered, an invented section is dropped.
	reply = "Кухня/Придуманный раздел"
	w = enrichProduct(t, h, unfiled.ID)
	if len(sent.Categories) == 0 {
		t.Fatal("no sections offered for an unfiled product")
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Data.Category != "" {
		t.Errorf("invented section reached the form: %q", got.Data.Category)
	}

	// ...and a real one passes through.
	reply = "Посуда/Кружки"
	w = enrichProduct(t, h, unfiled.ID)
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Data.Category != "Посуда/Кружки" {
		t.Errorf("a real section was dropped: %q", got.Data.Category)
	}
}

// Every way this can fail says something the owner can act on, and none of
// them touches the product.
func TestEnrichFailuresAreExplained(t *testing.T) {
	h := newTestHandler(t)
	if err := h.db.CreateSettings(&database.Settings{OwnerEmail: "o@example.com"}); err != nil {
		t.Fatal(err)
	}
	p := &database.Product{Title: "Товар", SKU: "T-1", Price: 100}
	if err := h.db.CreateProduct(p); err != nil {
		t.Fatal(err)
	}

	// No key saved: the shop must not even call out.
	w := enrichProduct(t, h, p.ID)
	if w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "ключ AdHunters") {
		t.Fatalf("no key: %d %s", w.Code, w.Body.String())
	}

	s, _ := h.db.GetSettings()
	s.AdHuntersAPIKey = "k"
	if err := h.db.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"rejected key", http.StatusUnauthorized, `{}`, "не подошёл"},
		{"out of credits", http.StatusPaymentRequired, `{}`, "закончились"},
		{"service down", http.StatusInternalServerError, `{}`, "попробуйте ещё раз"},
		{"empty draft", http.StatusOK, `{"data":{"title":"","description":""}}`, "попробуйте ещё раз"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeAdHunters(t, tc.status, tc.body, nil)
			w := enrichProduct(t, h, p.ID)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("code: %d %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Errorf("message %q lacks %q", w.Body.String(), tc.want)
			}
		})
	}
}
