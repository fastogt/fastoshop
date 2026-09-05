package wb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/channel"
	"github.com/fastogt/fastoshop/app/database"
)

// Polling faster only spends the method budget: sales within a tick collapse anyway.
const kPushInterval = 5 * time.Minute

// Wildberries meters per method and answers a burst of calls with a 429.
const kBatchPause = 200 * time.Millisecond

// ErrPushBusy - the previous pass is still running.
var ErrPushBusy = errors.New("wb push already running")

// ErrBadWarehouse travels to the owner, so the handler translates it.
var ErrBadWarehouse = errors.New("warehouse_id must be a number")

// Worker pushes by levels: the link row remembers the last value sent.
type Worker struct {
	*channel.Signals
	db *database.Database
	// Hosts overrides the API addresses - tests point every field at one mock.
	Hosts   Hosts
	running atomic.Bool
}

func NewWorker(db *database.Database) *Worker {
	return &Worker{Signals: channel.NewSignals(), db: db}
}

// Settings are read on every pass: the owner changes them without a restart.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(kPushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-w.Wake():
		}
		pushed, failed, err := w.Pass()
		switch {
		case errors.Is(err, ErrPushBusy):
		case err != nil:
			log.Warnf("wb sync: %v", err)
		case pushed > 0 || failed > 0:
			log.Infof("wb sync: pushed %d, failed %d", pushed, failed)
		}
	}
}

// Price tasks are settled first: until then their rows are invisible to the push.
func (w *Worker) Pass() (pushed, failed int, err error) {
	if !w.running.CompareAndSwap(false, true) {
		return 0, 0, ErrPushBusy
	}
	defer w.running.Store(false)

	s, err := w.db.GetWBSettings()
	if err != nil {
		return 0, 0, err
	}
	if !s.Enabled || s.Token == "" {
		return 0, 0, nil
	}
	// Prices need no warehouse, stocks do - a cabinet without one still gets prices.
	var warehouse int64
	if s.WarehouseID != "" {
		warehouse, err = strconv.ParseInt(s.WarehouseID, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: %q", ErrBadWarehouse, s.WarehouseID)
		}
	}

	c := w.client(s)
	if err := w.settlePriceTasks(c); err != nil {
		return 0, 0, err
	}
	// Orders next: a platform sale must lower our stock before we push levels.
	if err := w.pollOrders(c); err != nil {
		return 0, 0, err
	}

	var halt bool
	if warehouse != 0 {
		pushed, failed, halt, err = w.pushStocks(c, warehouse)
	}
	if err != nil || halt {
		// halt: the whole stocks call died, so prices wait for the next tick too.
		return pushed, failed, err
	}
	p, f, err := w.pushPrices(c)
	return pushed + p, failed + f, err
}

// The worker's own Hosts win, so tests can point everything at a mock.
func (w *Worker) client(s *database.WBSettings) *Client {
	c := &Client{Token: s.Token, Hosts: w.Hosts}
	if w.Hosts == (Hosts{}) && s.Sandbox {
		c.Hosts = kSandbox
	}
	return c
}

// halt means an entire call failed; 204 credits the batch, only a 409 names rows.
func (w *Worker) pushStocks(c *Client, warehouse int64) (pushed, failed int, halt bool, err error) {
	rows, err := w.db.WBStockToPush()
	if err != nil {
		return 0, 0, false, err
	}
	for len(rows) > 0 {
		n := min(kStockBatch, len(rows))
		batch := rows[:n]
		rows = rows[n:]

		items := make([]StockItem, len(batch))
		for i, r := range batch {
			items[i] = StockItem{Sku: r.Barcode, Amount: r.Stock}
		}
		if pushed+failed > 0 {
			time.Sleep(kBatchPause)
		}
		refused, callErr := c.SetStocks(warehouse, items)
		if callErr != nil {
			for _, r := range batch {
				w.markStockError(r, callErr.Error(), callDelay(callErr, channel.RetryDelay(r.Error)))
			}
			return pushed, failed + len(batch), true, nil
		}
		for _, r := range batch {
			if msg, bad := refused[r.Barcode]; bad {
				w.markStockError(r, msg, channel.RetryDelay(r.Error))
				failed++
				continue
			}
			if err := w.db.MarkWBStockPushed(r.ProductID, r.Stock); err != nil {
				log.Warnf("wb stock sync: mark %s: %v", r.Barcode, err)
				continue
			}
			pushed++
		}
	}
	return pushed, failed, false, nil
}

// On a 429 we obey the platform's retry hint verbatim; its limits outrank ours.
func callDelay(callErr error, fallback time.Duration) time.Duration {
	var apiErr *APIError
	if errors.As(callErr, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	return fallback
}

func (w *Worker) markStockError(r database.WBStockRow, msg string, delay time.Duration) {
	if err := w.db.MarkWBStockError(r.ProductID, msg, time.Now().Add(delay)); err != nil {
		log.Warnf("wb stock sync: mark error %s: %v", r.Barcode, err)
	}
}
