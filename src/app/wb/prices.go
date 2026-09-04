package wb

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/channel"
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// kTaskTTL is how long a price upload may stay unanswered before its rows are
// released. Without a ceiling a task the platform forgot about would pin its
// products out of the sync forever, and the tab would show them as "in flight"
// with nothing ever moving.
const kTaskTTL = time.Hour

// settlePriceTasks credits or releases the uploads started by earlier passes.
// It runs before the price push: rows carrying a task are invisible to the guard
// until this has resolved them.
func (w *Worker) settlePriceTasks(c *Client) error {
	tasks, err := w.db.WBPriceTasks()
	if err != nil {
		return err
	}
	for _, t := range tasks {
		state, byNm, err := c.PriceTaskStatus(t.UploadID)
		if err != nil {
			// A status we could not read is not a failed upload: leave the task
			// alone and try again next pass, the TTL below is the real ceiling.
			log.Warnf("wb price task %s: %v", t.UploadID, err)
			state = TaskPending
		}
		switch state {
		case TaskDone:
			if err := w.db.MarkWBPriceTaskDone(t.UploadID); err != nil {
				return err
			}
		case TaskFailed:
			if err := w.db.MarkWBPriceTaskFailed(t.UploadID, byNm,
				i18n.KeyWBUnknownReply, time.Now().Add(channel.NextRetry)); err != nil {
				return err
			}
		case TaskPending:
			if time.Since(t.CreatedAt) < kTaskTTL {
				continue
			}
			if err := w.db.MarkWBPriceTaskFailed(t.UploadID, nil,
				i18n.KeyWBPriceTaskStuck, time.Now().Add(channel.NextRetry)); err != nil {
				return err
			}
		}
	}
	return nil
}

// pushPrices sends only the prices the owner opted in explicitly: rows with
// price = 0 never reach the platform, so a shop that never touched the price
// column cannot have its Wildberries prices moved by us.
//
// Wildberries takes one price per card, and our catalogue can hold several sizes
// of one card as separate products. Sizes that disagree are not resolved by
// picking one - the whole card is skipped and every row of it is told why.
func (w *Worker) pushPrices(c *Client) (pushed, failed int, err error) {
	rows, err := w.db.WBPriceToPush()
	if err != nil {
		return 0, 0, err
	}
	agreed, conflicted := groupByCard(rows)
	if len(conflicted) > 0 {
		if err := w.db.MarkWBCardError(cardIDs(conflicted), i18n.KeyWBPriceConflict,
			time.Now().Add(channel.NextRetry)); err != nil {
			return 0, 0, err
		}
		for _, g := range conflicted {
			failed += len(g.rows)
		}
	}

	for len(agreed) > 0 {
		n := min(kPriceBatch, len(agreed))
		batch := agreed[:n]
		agreed = agreed[n:]

		items := make([]PriceItem, len(batch))
		var sent []database.WBPriceSent
		for i, g := range batch {
			var value int64
			items[i], value = NewPriceItem(g.nmID, g.price)
			for _, r := range g.rows {
				sent = append(sent, database.WBPriceSent{ProductID: r.ProductID, Sent: value})
			}
		}
		if pushed > 0 {
			time.Sleep(kBatchPause)
		}
		uploadID, callErr := c.UploadPrices(items)
		if callErr != nil {
			if err := w.db.MarkWBCardError(cardIDs(batch), callErr.Error(),
				time.Now().Add(callDelay(callErr, channel.NextRetry))); err != nil {
				return pushed, failed, err
			}
			return pushed, failed + len(sent), nil
		}
		if err := w.db.MarkWBPriceSent(uploadID, time.Now(), sent); err != nil {
			return pushed, failed, err
		}
		// Not counted as pushed yet: the platform has accepted the task, not the
		// prices. The next pass credits them once the task reports back.
		pushed += len(sent)
	}
	return pushed, failed, nil
}

// cardGroup is one card's rows and the single price they all agree on.
type cardGroup struct {
	nmID  int64
	price int64
	rows  []database.WBPriceRow
}

func cardIDs(groups []cardGroup) []int64 {
	out := make([]int64, len(groups))
	for i, g := range groups {
		out[i] = g.nmID
	}
	return out
}

// groupByCard collapses rows to one item per card and reports the cards whose
// sizes want different prices.
func groupByCard(rows []database.WBPriceRow) (agreed, conflicted []cardGroup) {
	byNm := map[int64]*cardGroup{}
	var order []int64
	for _, r := range rows {
		g, ok := byNm[r.NmID]
		if !ok {
			byNm[r.NmID] = &cardGroup{nmID: r.NmID, price: r.Price, rows: []database.WBPriceRow{r}}
			order = append(order, r.NmID)
			continue
		}
		g.rows = append(g.rows, r)
		if r.Price != g.price {
			g.price = -1
		}
	}
	for _, nm := range order {
		g := byNm[nm]
		if g.price < 0 {
			conflicted = append(conflicted, *g)
			continue
		}
		agreed = append(agreed, *g)
	}
	return agreed, conflicted
}
