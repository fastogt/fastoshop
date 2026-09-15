package wb

import (
	"testing"
	"time"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

func linkAndPrice(t *testing.T, h *Handlers, d *database.Database, sku string, price int64) int64 {
	t.Helper()
	id := seedProduct(t, d, sku, 5, 1000)
	do(t, h, "POST", "/publish", selection(t, d))
	if _, err := d.SetWBPrice(id, price); err != nil {
		t.Fatal(err)
	}
	return id
}

// The price API is asynchronous: a second pass must not send the same price again.
func TestPriceNotResentWhileInFlight(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	cab.taskStatus = 0 // still moving
	linkAndPrice(t, h, d, "ART-1", 1500)

	do(t, h, "POST", "/push", "")
	if len(cab.sentPrices()) != 1 {
		t.Fatalf("expected one upload, got %d", len(cab.sentPrices()))
	}
	do(t, h, "POST", "/push", "")
	if len(cab.sentPrices()) != 1 {
		t.Fatalf("an in-flight price was uploaded again: %+v", cab.sentPrices())
	}

	got := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if got.PriceInFlight != 1 {
		t.Fatalf("the owner must see the wait: %+v", got)
	}
}

func TestPriceCreditedWhenTaskSucceeds(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	cab.taskStatus = 0
	linkAndPrice(t, h, d, "ART-1", 1500)
	do(t, h, "POST", "/push", "")

	cab.mu.Lock()
	cab.taskStatus = kTaskStatusDone
	cab.mu.Unlock()
	do(t, h, "POST", "/push", "")

	rows, err := d.ListWBLinksPage(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].PricePushed != 1500 || rows[0].InFlight {
		t.Fatalf("a confirmed task must become the new baseline: %+v", rows[0])
	}
	// And the baseline holds: nothing more goes on the wire.
	do(t, h, "POST", "/push", "")
	if len(cab.sentPrices()) != 1 {
		t.Fatalf("a credited price was uploaded again: %+v", cab.sentPrices())
	}
}

func TestPriceTaskFailureNamesTheCard(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	cab.taskStatus = 0
	linkAndPrice(t, h, d, "ART-1", 1500)
	do(t, h, "POST", "/push", "")

	cab.mu.Lock()
	cab.taskStatus = kTaskStatusRejected
	cab.taskErrors = map[int64]string{1: "цена ниже минимальной"}
	cab.mu.Unlock()
	do(t, h, "POST", "/push", "")

	bad, err := d.ListWBPriceErrors()
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 1 || bad[0].Error != "цена ниже минимальной" {
		t.Fatalf("the platform's own reason must survive: %+v", bad)
	}
}

// A task that never resolves would pin its rows out of the sync forever.
func TestStuckPriceTaskIsReleased(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	cab.taskStatus = 0
	linkAndPrice(t, h, d, "ART-1", 1500)
	do(t, h, "POST", "/push", "")

	// Age the task past its ceiling.
	if err := d.MarkWBPriceSent("42", time.Now().Add(-2*kTaskTTL), nil); err != nil {
		t.Fatal(err)
	}
	do(t, h, "POST", "/push", "")

	bad, err := d.ListWBPriceErrors()
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 1 || bad[0].Error != i18n.KeyWBPriceTaskStuck {
		t.Fatalf("a lost task must release its rows: %+v", bad)
	}
}

// One price per card: sizes that disagree are not resolved by picking one.
func TestSizesDisagreeingOnPriceSendNothing(t *testing.T) {
	h, d, cab := newTest(t,
		sizedCard(9, "ART-9", map[string]string{"M": "2000000000097", "L": "2000000000103"}),
	)
	enable(t, d, "7")
	// Two products of one card, linked by their imported sized articles.
	a := seedProduct(t, d, "ART-9-M", 5, 1000)
	b := seedProduct(t, d, "ART-9-L", 5, 1000)
	do(t, h, "POST", "/publish", selection(t, d))
	if _, err := d.SetWBPrice(a, 1500); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SetWBPrice(b, 1900); err != nil {
		t.Fatal(err)
	}

	do(t, h, "POST", "/push", "")
	if len(cab.sentPrices()) != 0 {
		t.Fatalf("a card with disagreeing sizes must send nothing: %+v", cab.sentPrices())
	}
	bad, err := d.ListWBPriceErrors()
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) != 2 {
		t.Fatalf("both sizes must be told why: %+v", bad)
	}
	for _, r := range bad {
		if r.Error != i18n.KeyWBPriceConflict {
			t.Fatalf("wrong reason: %+v", r)
		}
	}
}

// Sizes that agree collapse to one item on the wire: the price lives on the card.
func TestAgreeingSizesCollapseToOneItem(t *testing.T) {
	h, d, cab := newTest(t,
		sizedCard(9, "ART-9", map[string]string{"M": "2000000000097", "L": "2000000000103"}),
	)
	enable(t, d, "7")
	a := seedProduct(t, d, "ART-9-M", 5, 1000)
	b := seedProduct(t, d, "ART-9-L", 5, 1000)
	do(t, h, "POST", "/publish", selection(t, d))
	for _, id := range []int64{a, b} {
		if _, err := d.SetWBPrice(id, 1500); err != nil {
			t.Fatal(err)
		}
	}

	do(t, h, "POST", "/push", "")
	sent := cab.sentPrices()
	if len(sent) != 1 || len(sent[0]) != 1 || sent[0][0].NmID != 9 || sent[0][0].Price != 15 {
		t.Fatalf("one card is one item at whole roubles: %+v", sent)
	}
}

// A price the owner never set is never touched.
func TestUnsetPriceIsNeverPushed(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	seedProduct(t, d, "ART-1", 5, 1000)
	do(t, h, "POST", "/publish", selection(t, d))

	do(t, h, "POST", "/push", "")
	if len(cab.sentPrices()) != 0 {
		t.Fatalf("an opt-out product was priced: %+v", cab.sentPrices())
	}
}
