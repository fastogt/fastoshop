package ozon

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
)

// kFirstPollWindow is the window of the very first poll. One day: once the shop
// is linked to the cabinet, yesterday's sales are already written off on the
// platform and have to be caught up. Reaching deeper is not allowed, or the
// first poll alone would zero out stocks with old orders.
const kFirstPollWindow = 24 * time.Hour

// kPollOverlap is a deliberate overlap of the window: at Ozon a posting can
// show up in the listing later than its own date. A posting seen twice is
// rejected by UNIQUE in the ledger, so a duplicate is free, while a lost order
// is a permanent error in the stocks.
const kPollOverlap = 10 * time.Minute

// kCancelledStatuses are the statuses meaning the posting will not happen and
// the goods must go back to the warehouse.
var kCancelledStatuses = map[string]bool{
	"cancelled":                    true,
	"not_accepted":                 true,
	"cancelled_from_split_pending": true,
}

// pollOrders fetches the platform's postings and applies them to our stocks.
// The cursor advances only after the whole batch was applied: an interrupted
// pass repeats the window in full, and the ledger rejects everything already
// applied.
func (w *Worker) pollOrders(c *Client) error {
	since, err := w.db.OzonOrdersSince()
	if err != nil {
		return err
	}
	to := time.Now()
	if since.IsZero() {
		since = to.Add(-kFirstPollWindow)
	}

	postings, err := c.ListPostings(since.Add(-kPollOverlap), to)
	if err != nil {
		w.SetPollError(err.Error())
		return err
	}

	applied := 0
	for _, p := range postings {
		created := p.CreatedAt()
		if created.IsZero() {
			created = to
		}
		items := make([]database.OzonPostingItem, 0, len(p.Products))
		for _, it := range p.Products {
			items = append(items, database.OzonPostingItem{OfferID: it.OfferID, Qty: it.Quantity})
		}
		moved, err := w.db.ApplyOzonPosting(&database.OzonPosting{
			PostingNumber: p.PostingNumber,
			Status:        p.Status,
			Cancelled:     kCancelledStatuses[p.Status],
			CreatedAt:     created,
			Items:         items,
		})
		if err != nil {
			w.SetPollError(err.Error())
			return err
		}
		if moved {
			applied++
		}
	}

	if err := w.db.SetOzonOrdersSince(to); err != nil {
		return err
	}
	w.SetPollError("")
	if applied > 0 {
		log.Infof("ozon orders: applied %d postings", applied)
		// Stock levels changed - wake the push. Within the current pass it runs
		// next and already sees the new stocks; the signal matters when the pass
		// was triggered by the button and the next tick is far away.
		w.StockChanged()
	}
	return nil
}
