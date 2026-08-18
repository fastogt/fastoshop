package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// The ladder is what makes a catalogue of seven-rouble sieves and three-hundred
// rouble steamers priceable at all: one multiplier leaves nothing on the first
// and prices the second above the brand's own store.
func TestShopPriceLadder(t *testing.T) {
	h := newTestHandler(t)
	if err := h.db.CreateSettings(&database.Settings{OwnerEmail: "o@example.com"}); err != nil {
		t.Fatal(err)
	}
	// Cost 10.00 and cost 300.00 in the shop's money, both from a supplier feed.
	cheap := &database.Product{Title: "Сито", SKU: "S-1", SourcePrice: 1000, Price: 1000}
	dear := &database.Product{Title: "Мантоварка", SKU: "M-1", SourcePrice: 30000, Price: 30000}
	for _, p := range []*database.Product{cheap, dear} {
		if err := h.db.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
	}

	w := httptest.NewRecorder()
	h.SetPriceRules(w, httptest.NewRequest("PUT", "/api/products/price-rules", strings.NewReader(
		`{"rules":[{"up_to":5000,"multiplier":1.5},{"up_to":0,"multiplier":1.15}]}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}

	// Saving the ladder must not move a single price on its own.
	got, _ := h.db.GetProduct(cheap.ID)
	if got.Price != 1000 {
		t.Fatalf("prices moved before the recompute: %d", got.Price)
	}

	w = httptest.NewRecorder()
	h.RecomputePrices(w, httptest.NewRequest("POST", "/api/products/recompute-prices",
		strings.NewReader(`{"coefficient":1}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("recompute: %d %s", w.Code, w.Body.String())
	}
	got, _ = h.db.GetProduct(cheap.ID)
	if got.Price != 1500 {
		t.Fatalf("cheap band: %d, want 1500", got.Price)
	}
	got, _ = h.db.GetProduct(dear.ID)
	if got.Price != 34500 {
		t.Fatalf("open band: %d, want 34500", got.Price)
	}

	// A ladder without an open-ended band would leave the dearest goods priceless.
	w = httptest.NewRecorder()
	h.SetPriceRules(w, httptest.NewRequest("PUT", "/api/products/price-rules", strings.NewReader(
		`{"rules":[{"up_to":5000,"multiplier":1.5}]}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a ladder with no open band must be refused: %d", w.Code)
	}
}
