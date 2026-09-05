package ozon

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fastogt/fastoshop/app/channel"
	"github.com/fastogt/fastoshop/app/database"
)

// publishTest shares the sync tests' mock cabinet: one view of the platform.
func publishTest(t *testing.T, offers ...string) (*Handlers, *database.Database, *ozonMock) {
	t.Helper()
	w, d, m := newSyncTest(t)
	h := NewHandlers(d, w)
	h.BaseURL = m.URL
	m.setOffers(offers...)
	return h, d, m
}

func seedProduct(t *testing.T, d *database.Database, sku string, stock int) int64 {
	t.Helper()
	p := &database.Product{SKU: sku, Title: "Товар " + sku, Stock: stock}
	if err := d.CreateProduct(p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

// Publishing is the owner's choice, not everything that matched by SKU.
func TestPublishOnlySelected(t *testing.T) {
	h, d, _ := publishTest(t, "A", "B")
	idA := seedProduct(t, d, "A", 5)
	seedProduct(t, d, "B", 7)

	body, _ := json.Marshal(channel.PublishRequest{ProductIDs: []int64{idA}})
	got := decode[publishResponse](t, do(t, h, "POST", "/publish", string(body)))
	if got.Published != 1 || len(got.NoCard) != 0 {
		t.Fatalf("publish: %+v", got)
	}
	links, _ := d.ListOzonLinksPage(1000, 0)
	if len(links) != 1 || links[0].ProductID != idA {
		t.Fatalf("extra rows went into the channel: %+v", links)
	}
}

// A product without a card in the cabinet is named by name, not dropped silently.
func TestPublishReportsMissingCard(t *testing.T) {
	h, d, _ := publishTest(t, "A")
	idA := seedProduct(t, d, "A", 5)
	idNo := seedProduct(t, d, "NOPE", 1)

	body, _ := json.Marshal(channel.PublishRequest{ProductIDs: []int64{idA, idNo}})
	got := decode[publishResponse](t, do(t, h, "POST", "/publish", string(body)))
	if got.Published != 1 || len(got.NoCard) != 1 || got.NoCard[0].SKU != "NOPE" {
		t.Fatalf("publish: %+v", got)
	}
}

// The link disappears only after a zero has gone out to the marketplace.
func TestUnpublishZeroesStockFirst(t *testing.T) {
	h, d, m := publishTest(t, "A")
	id := seedProduct(t, d, "A", 5)

	body, _ := json.Marshal(channel.PublishRequest{ProductIDs: []int64{id}})
	do(t, h, "POST", "/publish", string(body))
	if _, _, err := h.worker.Pass(); err != nil {
		t.Fatal(err)
	}
	if got := m.lastBatch(t); len(got) != 1 || got[0].Stock != 5 {
		t.Fatalf("stock not pushed: %+v", got)
	}

	got := decode[unpublishResponse](t, do(t, h, "POST", "/unpublish", string(body)))
	if got.Unpublished != 1 || len(got.Failed) != 0 {
		t.Fatalf("unpublish: %+v", got)
	}
	if last := m.lastBatch(t); len(last) != 1 || last[0].Stock != 0 {
		t.Fatalf("zero not sent before unlinking: %+v", last)
	}
	if links, _ := d.ListOzonLinksPage(1000, 0); len(links) != 0 {
		t.Fatalf("link remained: %+v", links)
	}
}

// If the marketplace did not accept the zero, the link must remain.
func TestUnpublishKeepsLinkWhenZeroRejected(t *testing.T) {
	h, d, m := publishTest(t, "A")
	id := seedProduct(t, d, "A", 5)
	body, _ := json.Marshal(channel.PublishRequest{ProductIDs: []int64{id}})
	do(t, h, "POST", "/publish", string(body))
	if _, _, err := h.worker.Pass(); err != nil {
		t.Fatal(err)
	}

	m.failOffer("A", "склад недоступен")
	got := decode[unpublishResponse](t, do(t, h, "POST", "/unpublish", string(body)))
	if got.Unpublished != 0 || len(got.Failed) != 1 {
		t.Fatalf("unpublish: %+v", got)
	}
	if links, _ := d.ListOzonLinksPage(1000, 0); len(links) != 1 {
		t.Fatalf("link lost on a failed zeroing: %+v", links)
	}
}

// A product that never went out to the marketplace is delisted without an API call.
func TestUnpublishNeverPushedNeedsNoCall(t *testing.T) {
	h, d, m := publishTest(t, "A")
	id := seedProduct(t, d, "A", 5)
	body, _ := json.Marshal(channel.PublishRequest{ProductIDs: []int64{id}})
	do(t, h, "POST", "/publish", string(body))

	before := len(m.sent())
	got := decode[unpublishResponse](t, do(t, h, "POST", "/unpublish", string(body)))
	if got.Unpublished != 1 {
		t.Fatalf("unpublish: %+v", got)
	}
	if len(m.sent()) != before {
		t.Error("extra marketplace call for a product that was never pushed there")
	}
}

// A product hidden from the storefront remains a publish candidate: those are separate decisions.
func TestCandidatesIncludeHidden(t *testing.T) {
	h, d, _ := publishTest(t, "A")
	id := seedProduct(t, d, "A", 5)
	p, _ := d.GetProduct(id)
	p.Hidden = true
	if err := d.UpdateProduct(p); err != nil {
		t.Fatal(err)
	}

	got := decode[channel.CandidatesResponse](t, do(t, h, "GET", "/candidates?page=1", ""))
	if len(got.Products) != 1 || !got.Products[0].Hidden || got.Products[0].Published {
		t.Fatalf("candidates: %+v", got.Products)
	}
	if w := do(t, h, "GET", "/candidates?q=нет", ""); w.Code != http.StatusOK {
		t.Fatalf("search: %d", w.Code)
	}
}

func TestCabinetCountsTheThreeStates(t *testing.T) {
	h, d, _ := publishTest(t, "A", "B", "ORPHAN")
	idA := seedProduct(t, d, "A", 5)
	seedProduct(t, d, "B", 7)
	seedProduct(t, d, "NOPE", 1)
	seedProduct(t, d, "ALSO-NOPE", 1)

	body, _ := json.Marshal(channel.PublishRequest{ProductIDs: []int64{idA}})
	do(t, h, "POST", "/publish", string(body))

	got := decode[cabinetResponse](t, do(t, h, "GET", "/cabinet", ""))
	if got.Cards != 3 || got.Products != 4 {
		t.Fatalf("cards %d products %d", got.Cards, got.Products)
	}
	if got.Linked != 1 || got.Ready != 1 || got.NoCard != 2 {
		t.Errorf("linked %d ready %d no_card %d; want 1/1/2", got.Linked, got.Ready, got.NoCard)
	}
	// A card matching nothing of ours is its own state, not a product row.
	if got.Orphans != 1 {
		t.Errorf("orphans %d, want 1", got.Orphans)
	}
	// The ids must name the product that can be linked, not the linked one.
	if len(got.ReadyIDs) != 1 {
		t.Fatalf("ready_ids %v", got.ReadyIDs)
	}
	if got.ReadyIDs[0] == idA {
		t.Error("a product that is already linked was offered for linking again")
	}
}

// A cabinet with no cards is an answer, not an error: everything lacks a card.
func TestCabinetWithAnEmptyCabinet(t *testing.T) {
	h, d, _ := publishTest(t)
	seedProduct(t, d, "A", 5)
	seedProduct(t, d, "B", 7)

	got := decode[cabinetResponse](t, do(t, h, "GET", "/cabinet", ""))
	if got.Cards != 0 || got.Ready != 0 || got.NoCard != 2 || len(got.ReadyIDs) != 0 {
		t.Fatalf("empty cabinet: %+v", got)
	}
}
