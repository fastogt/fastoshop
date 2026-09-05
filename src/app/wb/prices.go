package wb

import (
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/channel"
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// How long an upload may stay unanswered before its rows are released again.
const kTaskTTL = time.Hour

// Runs before the price push: rows carrying a task are invisible until resolved.
func (w *Worker) settlePriceTasks(c *Client) error {
	tasks, err := w.db.WBPriceTasks()
	if err != nil {
		return err
	}
	for _, t := range tasks {
		state, byNm, err := c.PriceTaskStatus(t.UploadID)
		if err != nil {
			// An unreadable status is not a failed upload; the TTL is the ceiling.
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

// Only opted-in prices go out; one price per card, so disagreeing sizes are skipped.
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
		// The platform accepted the task, not the prices; the next pass credits them.
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

// Collapses rows to one item per card and reports the cards whose sizes disagree.
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
