package wb

import (
	"testing"
	"time"

	"github.com/fastogt/fastoshop/app/database"
)

func order(id int64, barcode string) Order {
	return Order{
		ID: id, Rid: "rid", CreatedAt: time.Now().UTC().Format(kOrderTimeShort),
		NmID: 1, Skus: []string{barcode}, Article: "ART-1",
	}
}

func stockOf(t *testing.T, d *database.Database, id int64) int {
	t.Helper()
	p, err := d.GetProduct(id)
	if err != nil {
		t.Fatal(err)
	}
	return p.Stock
}

func TestSaleDeductsStockOnce(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ART-1", 10, 1000)
	do(t, h, "POST", "/publish", selection(t, d))
	cab.orders = []Order{order(500, "2000000000011")}

	do(t, h, "POST", "/push", "")
	if got := stockOf(t, d, id); got != 9 {
		t.Fatalf("stock after one sale: %d", got)
	}
	// The overlap window shows the same task again; the ledger rejects it.
	do(t, h, "POST", "/push", "")
	if got := stockOf(t, d, id); got != 9 {
		t.Fatalf("the same sale was applied twice: %d", got)
	}
}

func TestCancellationReturnsStockOnce(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ART-1", 10, 1000)
	do(t, h, "POST", "/publish", selection(t, d))
	cab.orders = []Order{order(500, "2000000000011")}
	do(t, h, "POST", "/push", "")

	cab.mu.Lock()
	cab.statuses[500] = OrderStatus{ID: 500, SupplierStatus: "cancel", WBStatus: "canceled"}
	cab.mu.Unlock()
	do(t, h, "POST", "/push", "")
	if got := stockOf(t, d, id); got != 10 {
		t.Fatalf("a cancelled sale must come back: %d", got)
	}
	do(t, h, "POST", "/push", "")
	if got := stockOf(t, d, id); got != 10 {
		t.Fatalf("the same cancellation was applied twice: %d", got)
	}
}

// The marketplace has already sold: negative stock is worse than zero.
func TestOversellFloorsAtZero(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	id := seedProduct(t, d, "ART-1", 0, 1000)
	do(t, h, "POST", "/publish", selection(t, d))
	cab.orders = []Order{order(501, "2000000000011")}

	do(t, h, "POST", "/push", "")
	if got := stockOf(t, d, id); got != 0 {
		t.Fatalf("stock went negative: %d", got)
	}
	got := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if got.OrdersOversold != 1 {
		t.Fatalf("an oversell must reach the owner's eyes: %+v", got)
	}
}

// A sale for a barcode we never linked is recorded anyway, as a warning.
func TestUnknownBarcodeIsRecorded(t *testing.T) {
	h, d, cab := newTest(t, card(1, "ART-1", "2000000000011"))
	enable(t, d, "7")
	seedProduct(t, d, "ART-1", 10, 1000)
	do(t, h, "POST", "/publish", selection(t, d))
	cab.orders = []Order{order(502, "9999999999999")}

	do(t, h, "POST", "/push", "")
	got := decode[settingsResponse](t, do(t, h, "GET", "/settings", ""))
	if got.OrdersTotal != 1 || got.OrdersUnresolved != 1 {
		t.Fatalf("an unmatched sale must be visible: %+v", got)
	}
	page := decode[wbOrdersResponse](t, do(t, h, "GET", "/orders", ""))
	if len(page.Orders) != 1 || page.Orders[0].ProductID != nil {
		t.Fatalf("the row must say it matched nothing: %+v", page.Orders)
	}
}
