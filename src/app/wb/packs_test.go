package wb

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

func linkBody(barcode string, productID, qty int64) string {
	return fmt.Sprintf(`{"barcode":%q,"product_id":%d,"qty":%d}`, barcode, productID, qty)
}

func linkIDOf(t *testing.T, d *database.Database, barcode string) int64 {
	t.Helper()
	links, err := d.AllWBLinks()
	if err != nil {
		t.Fatal(err)
	}
	l, found := links[barcode]
	if !found {
		t.Fatalf("no link for %s", barcode)
	}
	return l.ID
}

// linkOfProduct is the id of a product's only link: prices are set per link.
func linkOfProduct(t *testing.T, d *database.Database, productID int64) int64 {
	t.Helper()
	links, err := d.AllWBLinks()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range links {
		if l.ProductID == productID {
			return l.ID
		}
	}
	t.Fatalf("product %d is not linked", productID)
	return 0
}

func stockSent(cab *cabinet, barcode string) (int64, bool) {
	var amount int64
	found := false
	for _, s := range cab.sentStocks() {
		if s.Sku == barcode {
			amount, found = s.Amount, true
		}
	}
	return amount, found
}

// Two packs of one product both draw on its stock, in whole packs.
func TestPackCardsPushWholePacks(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ШД8/4шт", "b4"), card(2, "ШД8/30шт", "b30"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ШД8", 50, 100)
	for _, body := range []string{linkBody("b4", id, 4), linkBody("b30", id, 30)} {
		if w := do(t, h, "PUT", "/links", body); w.Code != http.StatusOK {
			t.Fatalf("link: %d", w.Code)
		}
	}
	do(t, h, "POST", "/push", "")
	if got, _ := stockSent(cab, "b4"); got != 12 {
		t.Fatalf("pack of 4 from 50: %d", got)
	}
	if got, _ := stockSent(cab, "b30"); got != 1 {
		t.Fatalf("pack of 30 from 50: %d", got)
	}

	// A pack of 30 sells: the shop loses 30 units and both cards follow.
	cab.orders = []Order{order(900, "b30")}
	do(t, h, "POST", "/push", "")
	do(t, h, "POST", "/push", "")
	if got := stockOf(t, d, id); got != 20 {
		t.Fatalf("stock after one pack of 30: %d", got)
	}
	if got, _ := stockSent(cab, "b4"); got != 5 {
		t.Fatalf("pack of 4 from 20: %d", got)
	}
	if got, _ := stockSent(cab, "b30"); got != 0 {
		t.Fatalf("pack of 30 from 20: %d", got)
	}
}

// A cancel returns what the sale took, even after the pack size was changed.
func TestPackCancelReturnsTheUnitsTaken(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ШД8/4шт", "b4"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ШД8", 50, 100)
	do(t, h, "PUT", "/links", linkBody("b4", id, 4))
	cab.orders = []Order{order(500, "b4")}
	do(t, h, "POST", "/push", "")
	if got := stockOf(t, d, id); got != 46 {
		t.Fatalf("stock after one pack of 4: %d", got)
	}

	do(t, h, "PUT", "/links", linkBody("b4", id, 8))
	cab.mu.Lock()
	cab.statuses[500] = OrderStatus{ID: 500, SupplierStatus: "cancel", WBStatus: "canceled"}
	cab.mu.Unlock()
	do(t, h, "POST", "/push", "")
	if got := stockOf(t, d, id); got != 50 {
		t.Fatalf("cancel must return the 4 taken, not 8: %d", got)
	}
}

func TestLinkCardRefusesNonsense(t *testing.T) {
	h, d, _ := newTest(t, card(1, "ШД8/4шт", "b4"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ШД8", 50, 100)
	for name, body := range map[string]string{
		"zero units":      linkBody("b4", id, 0),
		"unknown card":    linkBody("nope", id, 4),
		"missing product": linkBody("b4", id+100, 4),
	} {
		if w := do(t, h, "PUT", "/links", body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	if links, _ := d.AllWBLinks(); len(links) != 0 {
		t.Fatalf("a refused link was stored: %+v", links)
	}
}

func TestCardsViewSuggestsPackAndShowsCardStock(t *testing.T) {
	h, d, _ := newTest(t, card(1, "ШД8/4шт", "b4"), card(2, "Посейдон", "bp"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ШД8", 50, 100)

	packs := decode[cabinetCardsResponse](t, do(t, h, "GET", "/cards?view=packs", ""))
	if packs.Total != 1 || packs.Cards[0].SuggestedQty != 4 || packs.Cards[0].LinkID != 0 {
		t.Fatalf("packs view: %+v", packs)
	}
	do(t, h, "PUT", "/links", linkBody("b4", id, 4))
	all := decode[cabinetCardsResponse](t, do(t, h, "GET", "/cards?q=шд8", ""))
	if all.Total != 1 || all.Cards[0].CardStock != 12 || all.Cards[0].ProductStock != 50 {
		t.Fatalf("linked row: %+v", all)
	}
	unlinked := decode[cabinetCardsResponse](t, do(t, h, "GET", "/cards?view=unlinked", ""))
	if unlinked.Total != 1 || unlinked.Cards[0].Barcode != "bp" {
		t.Fatalf("unlinked view: %+v", unlinked)
	}
}

// Every card of the product hears zero before its link is dropped.
func TestUnpublishZeroesEveryCardOfTheProduct(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ШД8", "b1"), card(2, "ШД8/4шт", "b4"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ШД8", 50, 100)
	do(t, h, "POST", "/publish", selection(t, d))
	do(t, h, "PUT", "/links", linkBody("b4", id, 4))
	do(t, h, "POST", "/push", "")

	do(t, h, "POST", "/unpublish", selection(t, d))
	for _, b := range []string{"b1", "b4"} {
		if got, found := stockSent(cab, b); !found || got != 0 {
			t.Fatalf("%s did not get a zero: %d %v", b, got, found)
		}
	}
	if links, _ := d.AllWBLinks(); len(links) != 0 {
		t.Fatalf("links remained: %+v", links)
	}
}

// Publishing by article must not undo a pack size set by hand.
func TestPublishKeepsAHandMadeLink(t *testing.T) {
	h, d, _ := newTest(t, card(1, "ШД8", "b1"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ШД8", 50, 100)
	do(t, h, "PUT", "/links", linkBody("b1", id, 3))
	do(t, h, "POST", "/publish", selection(t, d))
	links, _ := d.AllWBLinks()
	if links["b1"].Qty != 3 {
		t.Fatalf("publish reset the pack size: %+v", links["b1"])
	}
}

func TestFillPricesPricesThePack(t *testing.T) {
	h, d, _ := newTest(t, card(1, "ШД8/4шт", "b4"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ШД8", 50, 250)
	do(t, h, "PUT", "/links", linkBody("b4", id, 4))
	if _, err := d.FillWBPrices(0); err != nil {
		t.Fatal(err)
	}
	links, _ := d.AllWBLinks()
	if links["b4"].Price != 1000 {
		t.Fatalf("pack of 4 at 250: %d", links["b4"].Price)
	}
}

// Unlinking a card that was pushed zeroes it on the platform first.
func TestUnlinkCardZeroesFirst(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ШД8/4шт", "b4"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ШД8", 50, 100)
	do(t, h, "PUT", "/links", linkBody("b4", id, 4))
	do(t, h, "POST", "/push", "")
	if w := do(t, h, "DELETE", fmt.Sprintf("/links/%d", linkIDOf(t, d, "b4")), ""); w.Code != http.StatusOK {
		t.Fatalf("unlink: %d", w.Code)
	}
	if got, _ := stockSent(cab, "b4"); got != 0 {
		t.Fatalf("no zero before unlink: %d", got)
	}
	if links, _ := d.AllWBLinks(); len(links) != 0 {
		t.Fatalf("link remained: %+v", links)
	}
}
