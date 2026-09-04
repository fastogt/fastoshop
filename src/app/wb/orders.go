package wb

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
)

// kFirstPollWindow is the window of the very first poll. One day: once the shop
// is linked to the cabinet, yesterday's sales are already written off on the
// platform and have to be caught up. Reaching deeper would let the first poll
// alone zero out stocks with old orders.
const kFirstPollWindow = 24 * time.Hour

// kPollOverlap is a deliberate overlap of the window: an assembly task can show
// up later than its own timestamp. A task seen twice is rejected by UNIQUE in
// the ledger, so a duplicate is free, while a lost sale is a permanent error in
// the stocks.
const kPollOverlap = 10 * time.Minute

// kOpenOrders is how many still-open tasks one pass asks the status of. Deep
// history is not interesting: a task that old is finished either way.
const kOpenOrders = 1000

// kCancelledStatuses are the statuses meaning the sale will not happen and the
// goods go back on the shelf. Both vocabularies are listed because the platform
// answers with two fields and spells cancellation differently in each.
var kCancelledStatuses = map[string]bool{
	"cancel":              true,
	"cancelled":           true,
	"canceled":            true,
	"canceled_by_client":  true,
	"declined_by_client":  true,
	"cancelled_by_client": true,
}

// pollOrders fetches assembly tasks, applies them to our stocks, and then asks
// what happened to the ones still open. The cursor advances only after the whole
// batch was applied: an interrupted pass repeats the window in full, and the
// ledger rejects everything already applied.
func (w *Worker) pollOrders(c *Client) error {
	since, err := w.db.WBOrdersSince()
	if err != nil {
		return err
	}
	to := time.Now()
	if since.IsZero() {
		since = to.Add(-kFirstPollWindow)
	}

	orders, err := c.ListOrders(since.Add(-kPollOverlap))
	if err != nil {
		w.SetPollError(err.Error())
		return err
	}

	applied := 0
	for _, o := range orders {
		created := o.CreatedTime()
		if created.IsZero() {
			created = to
		}
		moved, err := w.db.ApplyWBOrder(&database.WBOrder{
			OrderID: o.ID,
			// The list carries no status; refreshStatuses fills the real one in.
			Status:    "new",
			Barcode:   o.Barcode(),
			Article:   o.Article,
			NmID:      o.NmID,
			Qty:       1,
			CreatedAt: created,
		})
		if err != nil {
			w.SetPollError(err.Error())
			return err
		}
		if moved {
			applied++
		}
	}

	returned, err := w.refreshStatuses(c)
	if err != nil {
		w.SetPollError(err.Error())
		return err
	}

	if err := w.db.SetWBOrdersSince(to); err != nil {
		return err
	}
	w.SetPollError("")
	if applied+returned > 0 {
		log.Infof("wb orders: applied %d, returned %d", applied, returned)
		// Stock levels changed - wake the push. Within the current pass it runs
		// next and already sees the new stocks; the signal matters when the pass
		// was triggered by the button and the next tick is far away.
		w.StockChanged()
	}
	return nil
}

// refreshStatuses asks about the tasks that may still change. The order list
// carries no status of its own, so a cancellation is only visible through this
// second call - without it a cancelled sale would hold our stock forever.
func (w *Worker) refreshStatuses(c *Client) (int, error) {
	ids, err := w.db.OpenWBOrderIDs(kOpenOrders)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	statuses, err := c.OrderStatuses(ids)
	if err != nil {
		return 0, err
	}
	returned := 0
	for id, s := range statuses {
		status := s.SupplierStatus
		if status == "" {
			status = s.WBStatus
		}
		cancelled := kCancelledStatuses[s.SupplierStatus] || kCancelledStatuses[s.WBStatus]
		moved, err := w.db.SetWBOrderStatus(id, status, cancelled)
		if err != nil {
			return returned, err
		}
		if moved {
			returned++
		}
	}
	return returned, nil
}
