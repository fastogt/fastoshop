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

// kPushInterval matches what platform connectors settle on in practice. Polling
// every minute costs four times the method budget and buys nothing: sales within
// a tick collapse into a single level push anyway.
const kPushInterval = 5 * time.Minute

// kBatchPause spaces consecutive calls out. Wildberries meters per method and
// answers a burst with a 429 that costs far more than this wait: a catalogue of
// 24 000 products is 24 stock calls, so the whole pass pays under five seconds
// for not looking like a flood.
const kBatchPause = 200 * time.Millisecond

// ErrPushBusy - the previous pass is still running. Parallel pushes of the same
// level are harmless, but the counters in the button's answer would become a lie.
var ErrPushBusy = errors.New("wb push already running")

// ErrBadWarehouse travels to the owner, so the handler turns it into their
// language; the wrapped value stays for the log.
var ErrBadWarehouse = errors.New("warehouse_id must be a number")

// Worker drives the shop's stocks and prices to Wildberries by levels: the link
// row remembers the last value pushed, and a push happens only when the wanted
// value differs from it.
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

// Run reads the settings on every pass rather than at start: the owner enables
// the push and changes the warehouse from the admin panel, and demanding a
// service restart for that is not acceptable.
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

// Pass is one sync pass. Settling price tasks comes first: they were started by
// an earlier pass, and until they are credited or released their rows are
// invisible to the price push.
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
	// Prices need no warehouse, stocks do: a cabinet without an FBS warehouse yet
	// must still get its prices instead of the whole pass going quiet.
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
	// Polling orders goes next: a sale on the platform must lower our stock before
	// this very pass starts pushing levels, otherwise we push up what Wildberries
	// has just sold - an oversell of our own making.
	if err := w.pollOrders(c); err != nil {
		return 0, 0, err
	}

	var halt bool
	if warehouse != 0 {
		pushed, failed, halt, err = w.pushStocks(c, warehouse)
	}
	if err != nil || halt {
		// halt: the whole stocks call died - the platform is having a bad moment,
		// and spending the price budget in the same pass buys nothing.
		return pushed, failed, err
	}
	p, f, err := w.pushPrices(c)
	return pushed + p, failed + f, err
}

// client builds the API client for these settings; the worker's own Hosts win so
// tests can point everything at a mock.
func (w *Worker) client(s *database.WBSettings) *Client {
	c := &Client{Token: s.Token, Hosts: w.Hosts}
	if w.Hosts == (Hosts{}) && s.Sandbox {
		c.Hosts = kSandbox
	}
	return c
}

// pushStocks reports halt when an entire call failed: the remaining batches are
// left to the next tick instead of being hammered through.
//
// Unlike Ozon there is no per-item verdict: a success is 204 with no body, so
// the whole batch is credited, and only a 409 names the barcodes to blame.
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

// callDelay: on a 429 we obey the platform's own retry hint verbatim - our
// backoff has no business arguing with the cabinet's limits.
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
