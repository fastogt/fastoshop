package ozon

import (
	"fmt"
	"testing"
	"time"

	"github.com/fastogt/fastoshop/app/database"
)

func TestPollAppliesPostingOnce(t *testing.T) {
	w, d, m := newSyncTest(t)
	a := seedLinked(t, d, "A", 10)
	b := seedLinked(t, d, "B", 4)
	m.setPostings(posting("0001-1", "awaiting_deliver", line("A", 3), line("B", 1)))

	pass(t, w)
	if got := stockOf(t, d, a); got != 7 {
		t.Fatalf("A: want 7, got %d", got)
	}
	if got := stockOf(t, d, b); got != 3 {
		t.Fatalf("B: want 3, got %d", got)
	}

	// The same posting a second time — the ledger rejects it via UNIQUE.
	pass(t, w)
	if stockOf(t, d, a) != 7 || stockOf(t, d, b) != 3 {
		t.Fatalf("repeated posting shifted stock: A=%d B=%d",
			stockOf(t, d, a), stockOf(t, d, b))
	}
	total, oversold, unresolved, err := d.CountOzonOrderState()
	if err != nil || total != 1 || oversold != 0 || unresolved != 0 {
		t.Fatalf("counters: %v %d/%d/%d", err, total, oversold, unresolved)
	}
}

func TestPollOversellClampsAtZero(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 2)
	m.setPostings(posting("0002-1", "awaiting_packaging", line("A", 5)))

	pass(t, w)
	if got := stockOf(t, d, id); got != 0 {
		t.Fatalf("stock went negative or was not deducted: %d", got)
	}
	orders, err := d.ListOzonOrdersPage(50, 0)
	if err != nil || len(orders) != 1 {
		t.Fatalf("order not recorded: %v %+v", err, orders)
	}
	if !orders[0].Oversold {
		t.Fatalf("oversell not flagged: %+v", orders[0])
	}
	t.Logf("oversell: %s qty=%d stock=%d",
		orders[0].PostingNumber, orders[0].Items[0].Qty, stockOf(t, d, id))
}

func TestPollUnresolvedOfferIsSurfaced(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	m.setPostings(posting("0003-1", "awaiting_deliver", line("A", 1), line("НЕТ-ТАКОГО", 2)))

	pass(t, w)
	if got := stockOf(t, d, id); got != 4 {
		t.Fatalf("linked item not deducted: %d", got)
	}
	orders, err := d.ListOzonOrdersPage(50, 0)
	if err != nil || len(orders) != 1 || len(orders[0].Items) != 2 {
		t.Fatalf("items: %v %+v", err, orders)
	}
	var unresolved *database.OzonOrderItem
	for i := range orders[0].Items {
		if orders[0].Items[i].ProductID == nil {
			unresolved = &orders[0].Items[i]
		}
	}
	if unresolved == nil || unresolved.OfferID != "НЕТ-ТАКОГО" || unresolved.Qty != 2 {
		t.Fatalf("unresolved item lost: %+v", orders[0].Items)
	}
	_, _, cnt, err := d.CountOzonOrderState()
	if err != nil || cnt != 1 {
		t.Fatalf("unresolved counter: %v %d", err, cnt)
	}
}

func TestPollCancellationRestocksOnce(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	m.setPostings(posting("0004-1", "awaiting_deliver", line("A", 2)))
	pass(t, w)
	if got := stockOf(t, d, id); got != 3 {
		t.Fatalf("sale not deducted: %d", got)
	}

	m.setPostings(posting("0004-1", "cancelled", line("A", 2)))
	pass(t, w)
	if got := stockOf(t, d, id); got != 5 {
		t.Fatalf("cancellation did not return stock: %d", got)
	}

	// Two more encounters of the same cancellation (including under a different
	// cancelling status) no longer move the stock.
	pass(t, w)
	m.setPostings(posting("0004-1", "not_accepted", line("A", 2)))
	pass(t, w)
	if got := stockOf(t, d, id); got != 5 {
		t.Fatalf("repeated cancellation returned stock twice: %d", got)
	}
}

func TestPollCancelledOnFirstSightDoesNotMoveStock(t *testing.T) {
	w, d, m := newSyncTest(t)
	id := seedLinked(t, d, "A", 5)
	m.setPostings(posting("0005-1", "cancelled", line("A", 2)))

	pass(t, w)
	if got := stockOf(t, d, id); got != 5 {
		t.Fatalf("posting cancelled right away shifted stock: %d", got)
	}
	orders, err := d.ListOzonOrdersPage(50, 0)
	if err != nil || len(orders) != 1 || orders[0].Status != database.OzonStatusCancelled {
		t.Fatalf("cancellation not recorded: %v %+v", err, orders)
	}

	// And the next encounter restocks nothing either.
	pass(t, w)
	if got := stockOf(t, d, id); got != 5 {
		t.Fatalf("cancelled posting returned stock that was never deducted: %d", got)
	}
}

func TestPollCursorAdvancesOnlyOnSuccess(t *testing.T) {
	w, d, m := newSyncTest(t)
	seedLinked(t, d, "A", 5)

	// A response of unknown shape — the cursor must not be touched, or the
	// window with the sales drifts away forever.
	m.mu.Lock()
	m.postingsRaw = `{"result":{"postings":[{"status":"awaiting_deliver"}]}}`
	m.mu.Unlock()
	if _, _, err := w.Pass(); err == nil {
		t.Fatal("unknown response format must be an error, not a silent zero")
	}
	t.Logf("poll error visible in the tab: %s", w.PollError())
	if since, err := d.OzonOrdersSince(); err != nil || !since.IsZero() {
		t.Fatalf("cursor advanced after a failure: %v %s", err, since)
	}

	m.mu.Lock()
	m.postingsRaw = ""
	m.mu.Unlock()
	before := time.Now().UTC().Add(-time.Second)
	m.setPostings(posting("0006-1", "awaiting_deliver", line("A", 1)))
	pass(t, w)
	since, err := d.OzonOrdersSince()
	if err != nil || since.Before(before) {
		t.Fatalf("cursor did not advance: %v %s", err, since)
	}
	if w.PollError() != "" {
		t.Fatalf("error not cleared: %s", w.PollError())
	}
	t.Logf("cursor: %s", since.Format(time.RFC3339))

	// The next poll goes out with an overlap backwards — and the re-fetched
	// posting passes through as a no-op.
	pass(t, w)
	fs := m.filters()
	last, err := time.Parse(time.RFC3339, fs[len(fs)-1].Since)
	if err != nil {
		t.Fatal(err)
	}
	overlap := since.Sub(last)
	t.Logf("window overlap: %s", overlap)
	if overlap < kPollOverlap-time.Second || overlap > kPollOverlap+time.Second {
		t.Fatalf("overlap %s is not %s", overlap, kPollOverlap)
	}
	if n, _ := d.CountOzonOrders(); n != 1 {
		t.Fatalf("overlap created a duplicate: %d", n)
	}
}

// TestFullCycle — the whole slice: linked, pushed a level, the marketplace sold,
// the poll applied the sale, the next pass pushed the reduced level.
func TestFullCycle(t *testing.T) {
	h, d := newTestHandlers(t)
	m := newOzonMock(t)
	h.BaseURL, h.worker.BaseURL = m.URL, m.URL
	if err := d.SaveOzonSettings(&database.OzonSettings{
		ClientID: "cid", APIKey: "key", WarehouseID: "77", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	id := seedLinked(t, d, "A", 10)

	step := func(format string, args ...any) { t.Logf("· "+format, args...) }

	got := decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	step("first push: pushed=%d, marketplace level %d",
		got.Pushed, m.lastBatch(t)[0].Stock)
	if got.Pushed != 1 || m.lastBatch(t)[0].Stock != 10 {
		t.Fatalf("first push: %+v %+v", got, m.lastBatch(t))
	}

	m.setPostings(posting("0007-1", "awaiting_deliver", line("A", 4)))
	got = decode[pushResponse](t, do(t, h, "POST", "/push", ""))
	step("after selling 4 pcs: our stock %d, pushed %d, marketplace level %d",
		stockOf(t, d, id), got.Pushed, m.lastBatch(t)[0].Stock)
	if stockOf(t, d, id) != 6 || got.Pushed != 1 || m.lastBatch(t)[0].Stock != 6 {
		t.Fatalf("cycle did not close: stock=%d %+v %+v",
			stockOf(t, d, id), got, m.lastBatch(t))
	}

	s := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	step("tab: sales %d, oversold %d, unresolved %d, poll error %q",
		s.OrdersTotal, s.OrdersOversold, s.OrdersUnresolved, s.PollError)
	if s.OrdersTotal != 1 || s.OrdersOversold != 0 || s.PollError != "" {
		t.Fatalf("tab status: %+v", s)
	}

	list := decode[ozonOrdersResponse](t, do(t, h, "GET", "/orders?page=1", ""))
	step("log: %s (%s), item %q x%d",
		list.Orders[0].PostingNumber, list.Orders[0].Status,
		list.Orders[0].Items[0].Title, list.Orders[0].Items[0].Qty)
	if list.Total != 1 || len(list.Orders) != 1 ||
		list.Orders[0].Items[0].Title != "Товар A" {
		t.Fatalf("sales log: %+v", list)
	}
}

func TestOrdersEndpointPaginates(t *testing.T) {
	h, d := newTestHandlers(t)
	for i := range kOrdersPageSize + 3 {
		if _, err := d.ApplyOzonPosting(&database.OzonPosting{
			PostingNumber: fmt.Sprintf("P-%03d", i),
			Status:        "awaiting_deliver",
			CreatedAt:     time.Now().Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	first := decode[ozonOrdersResponse](t, do(t, h, "GET", "/orders", ""))
	if first.Total != kOrdersPageSize+3 || len(first.Orders) != kOrdersPageSize {
		t.Fatalf("first page: total=%d len=%d", first.Total, len(first.Orders))
	}
	second := decode[ozonOrdersResponse](t, do(t, h, "GET", "/orders?page=2", ""))
	if len(second.Orders) != 3 || second.Page != 2 {
		t.Fatalf("second page: %+v", second)
	}
	// Newest first: the first on page two is older than the last on page one.
	if second.Orders[0].CreatedAt.After(first.Orders[len(first.Orders)-1].CreatedAt) {
		t.Fatalf("page order broken: %s vs %s",
			second.Orders[0].CreatedAt, first.Orders[len(first.Orders)-1].CreatedAt)
	}
}
