package ozon

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
)

// One day: reaching deeper would zero out stocks with old orders on the first poll.
const kFirstPollWindow = 24 * time.Hour

// At Ozon a posting can appear in the listing later than its own date; UNIQUE eats dupes.
const kPollOverlap = 10 * time.Minute

// Statuses meaning the posting will not happen and the goods come back.
var kCancelledStatuses = map[string]bool{
	"cancelled":                    true,
	"not_accepted":                 true,
	"cancelled_from_split_pending": true,
}

// The cursor advances only after the whole batch was applied.
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
		// Stock levels changed - wake the push instead of waiting for the next tick.
		w.StockChanged()
	}
	return nil
}
