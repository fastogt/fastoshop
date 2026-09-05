package wb

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
)

// One day: reaching deeper would zero out stocks with old orders on the first poll.
const kFirstPollWindow = 24 * time.Hour

// An assembly task can appear later than its own timestamp; UNIQUE eats duplicates.
const kPollOverlap = 10 * time.Minute

// How many still-open tasks one pass asks the status of; older ones are finished.
const kOpenOrders = 1000

// Both vocabularies: the platform answers with two status fields spelled differently.
var kCancelledStatuses = map[string]bool{
	"cancel":              true,
	"cancelled":           true,
	"canceled":            true,
	"canceled_by_client":  true,
	"declined_by_client":  true,
	"cancelled_by_client": true,
}

// The cursor advances only after the whole batch was applied.
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
		// Stock levels changed - wake the push instead of waiting for the next tick.
		w.StockChanged()
	}
	return nil
}

// The order list carries no status, so a cancellation is only visible from this call.
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
