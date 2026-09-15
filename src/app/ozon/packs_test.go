package ozon

import (
	"fmt"
	"net/http"
	"testing"
)

func linkBody(offerID string, productID, qty int64) string {
	return fmt.Sprintf(`{"offer_id":%q,"product_id":%d,"qty":%d}`, offerID, productID, qty)
}

// One posting sells a single unit and a pack of 12; each line takes its own units.
func TestPostingLinesTakeTheirPackUnits(t *testing.T) {
	h, d, m := publishTest(t, "A", "B/12шт")
	a := seedProduct(t, d, "A", 10)
	b := seedProduct(t, d, "B", 30)
	do(t, h, "PUT", "/links", linkBody("A", a, 1))
	do(t, h, "PUT", "/links", linkBody("B/12шт", b, 12))

	m.setPostings(posting("0100-1", "awaiting_deliver", line("A", 1), line("B/12шт", 2)))
	pass(t, h.worker)
	if got := stockOf(t, d, a); got != 9 {
		t.Fatalf("A: want 9, got %d", got)
	}
	if got := stockOf(t, d, b); got != 6 {
		t.Fatalf("B: two packs of 12 from 30 leave 6, got %d", got)
	}

	m.setPostings(posting("0100-1", "cancelled", line("A", 1), line("B/12шт", 2)))
	pass(t, h.worker)
	if stockOf(t, d, a) != 10 || stockOf(t, d, b) != 30 {
		t.Fatalf("cancel must return the units taken: A=%d B=%d", stockOf(t, d, a), stockOf(t, d, b))
	}
}

// A card sells whole packs: 30 units behind a pack of 12 show as 2.
func TestPackCardPushesWholePacks(t *testing.T) {
	h, d, m := publishTest(t, "B/12шт")
	b := seedProduct(t, d, "B", 30)
	if w := do(t, h, "PUT", "/links", linkBody("B/12шт", b, 12)); w.Code != http.StatusOK {
		t.Fatalf("link: %d", w.Code)
	}
	pass(t, h.worker)
	if got := m.lastBatch(t); len(got) != 1 || got[0].OfferID != "B/12шт" || got[0].Stock != 2 {
		t.Fatalf("pushed: %+v", got)
	}
}

func TestOzonLinkCardRefusesNonsense(t *testing.T) {
	h, d, _ := publishTest(t, "B/12шт")
	b := seedProduct(t, d, "B", 30)
	for name, body := range map[string]string{
		"zero units":      linkBody("B/12шт", b, 0),
		"unknown card":    linkBody("nope", b, 12),
		"missing product": linkBody("B/12шт", b+100, 12),
	} {
		if w := do(t, h, "PUT", "/links", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if links, _ := d.AllOzonLinks(); len(links) != 0 {
		t.Fatalf("a refused link was stored: %+v", links)
	}
}

func TestOzonCardsViewSuggestsPack(t *testing.T) {
	h, d, _ := publishTest(t, "B/12шт", "Посейдон17см")
	b := seedProduct(t, d, "B", 30)
	packs := decode[cabinetCardsResponse](t, do(t, h, "GET", "/cards?view=packs", ""))
	if packs.Total != 1 || packs.Cards[0].SuggestedQty != 12 {
		t.Fatalf("packs view: %+v", packs)
	}
	do(t, h, "PUT", "/links", linkBody("B/12шт", b, 12))
	all := decode[cabinetCardsResponse](t, do(t, h, "GET", "/cards", ""))
	for _, c := range all.Cards {
		if c.OfferID == "B/12шт" && (c.LinkID == 0 || c.CardStock != 2) {
			t.Fatalf("linked row: %+v", c)
		}
	}
}
