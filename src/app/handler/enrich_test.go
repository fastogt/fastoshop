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

// A section the shop does not have cannot reach the form, however confidently
// the model writes it: an invented category is a landing page that does not
// exist. One the shop does have passes through.
func TestEnrichOnlyOffersSectionsTheShopHas(t *testing.T) {
	h := newTestHandler(t)
	if err := h.db.CreateSettings(&database.Settings{OwnerEmail: "o@example.com"}); err != nil {
		t.Fatal(err)
	}
	s, _ := h.db.GetSettings()
	s.AdHuntersAPIKey = "k"
	if err := h.db.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	real := &database.Product{Title: "Кружка", SKU: "K-1", Price: 100,
		Category: "Посуда/Кружки"}
	if err := h.db.CreateProduct(real); err != nil {
		t.Fatal(err)
	}

	draft := func(category string) string {
		b, _ := json.Marshal(map[string]any{"data": map[string]string{
			"title": "Кружка стеклянная", "description": "Текст.", "category": category}})
		return string(b)
	}

	fakeAdHunters(t, http.StatusOK, draft("Кухня/Придуманный раздел"), nil)
	w := enrichProduct(t, h, real.ID)
	var got struct {
		Data enrichResponse `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Data.Category != "" {
		t.Errorf("invented section reached the form: %q", got.Data.Category)
	}

	fakeAdHunters(t, http.StatusOK, draft("Посуда/Кружки"), nil)
	w = enrichProduct(t, h, real.ID)
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Data.Category != "Посуда/Кружки" {
		t.Errorf("a real section was dropped: %q", got.Data.Category)
	}

	// The product's own section must always be on the list, however deep it
	// sits: otherwise the model cannot answer "keep it" and the shallow
	// section it picks instead replaces a precise path with a vague one.
	deep := &database.Product{Title: "Банка", SKU: "B-1", Price: 100,
		Category: "Посуда/Ёмкости для хранения/Банки и крышки"}
	if err := h.db.CreateProduct(deep); err != nil {
		t.Fatal(err)
	}
	var sent enrichRequest
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&sent)
			_, _ = w.Write([]byte(draft(sent.Category)))
		}))
	old := adHuntersEnrichURL
	adHuntersEnrichURL = srv.URL
	defer func() { adHuntersEnrichURL = old; srv.Close() }()

	w = enrichProduct(t, h, deep.ID)
	if len(sent.Categories) == 0 || sent.Categories[0] != deep.Category {
		t.Errorf("the product's own section was not offered first: %v", sent.Categories)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Data.Category != deep.Category {
		t.Errorf("keeping the existing section was refused: %q", got.Data.Category)
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
