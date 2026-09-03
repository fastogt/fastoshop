package ozon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// kPushInterval matches what platform connectors settle on in practice. Polling
// every minute costs four times the method budget and buys nothing: sales within
// a tick collapse into a single level push anyway.
const kPushInterval = 5 * time.Minute

// ponytail: a two-step backoff ladder instead of an attempt counter - it needs
// no new column in the schema (and any schema change here costs a MINOR release
// and a manual ALTER TABLE on live installs). We only tell "first failure" from
// "it was already bad". If a third class of errors shows up that this cannot
// serve - add attempts INTEGER and count for real.
const (
	kFirstRetry = time.Minute
	kNextRetry  = 15 * time.Minute
)

// ErrPushBusy - the previous pass is still running. Parallel pushes of the same
// level are harmless, but the counters in the button's answer would become a lie.
var ErrPushBusy = errors.New("ozon push already running")

// ErrBadWarehouse travels to the owner, so the handler turns it into their
// language; the wrapped value stays for the log.
var ErrBadWarehouse = errors.New("warehouse_id must be a number")

// Worker drives the shop's stocks and prices to the platform by levels: the link
// row remembers the last value pushed, and a push happens only when the wanted
// value differs from it. A lost push is repaired by the next tick, and N sales
// within one tick collapse into a single call.
type Worker struct {
	db *database.Database
	// BaseURL overrides the Seller API address - tests point it at a mock.
	BaseURL string
	wake    chan struct{}
	running atomic.Bool
	// pollErr is why the last order poll did not happen. In memory rather than
	// in the database: a column would cost an ALTER TABLE on live installs, and
	// after a restart the next tick fills it in again anyway.
	pollErr atomic.Pointer[string]
}

func NewWorker(db *database.Database) *Worker {
	return &Worker{db: db, wake: make(chan struct{}, 1)}
}

func (w *Worker) PollError() string {
	if p := w.pollErr.Load(); p != nil {
		return *p
	}
	return ""
}

func (w *Worker) setPollError(msg string) { w.pollErr.Store(&msg) }

// StockChanged wakes the worker without blocking the caller: a buffer of one and
// a non-blocking send make it a "something changed" signal, not an event queue.
func (w *Worker) StockChanged() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
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
		case <-w.wake:
		}
		pushed, failed, err := w.Pass()
		switch {
		case errors.Is(err, ErrPushBusy):
		case err != nil:
			log.Warnf("ozon sync: %v", err)
		case pushed > 0 || failed > 0:
			log.Infof("ozon sync: pushed %d, failed %d", pushed, failed)
		}
	}
}

// Pass is one sync pass. It returns how many items the platform accepted and
// rejected, stocks and prices together. A disabled or half-configured
// integration is not an error: the pass is simply empty.
func (w *Worker) Pass() (pushed, failed int, err error) {
	if !w.running.CompareAndSwap(false, true) {
		return 0, 0, ErrPushBusy
	}
	defer w.running.Store(false)

	s, err := w.db.GetOzonSettings()
	if err != nil {
		return 0, 0, err
	}
	if !s.Enabled || s.ClientID == "" || s.APIKey == "" {
		return 0, 0, nil
	}
	// Prices need no warehouse, stocks do: a cabinet without an FBS warehouse
	// yet must still get its prices instead of the whole pass going quiet.
	var warehouse int64
	if s.WarehouseID != "" {
		warehouse, err = strconv.ParseInt(s.WarehouseID, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: %q", ErrBadWarehouse, s.WarehouseID)
		}
	}

	c := &Client{ClientID: s.ClientID, APIKey: s.APIKey, BaseURL: w.BaseURL}
	// Polling orders goes first: a sale on the platform must lower our stock
	// before this very pass starts pushing levels. Otherwise we would push up
	// what Ozon has just sold - an oversell of our own making. If the poll
	// fails, nothing is pushed.
	if err := w.pollOrders(c); err != nil {
		return 0, 0, err
	}

	var halt bool
	if warehouse != 0 {
		pushed, failed, halt, err = w.pushStocks(c, warehouse)
	}
	if err != nil || halt {
		// halt: the whole stocks call died (network, 5xx, 429) - the platform is
		// having a bad moment, and hammering it with prices from the same pass
		// would only spend the retry budget. The next tick picks both up.
		return pushed, failed, err
	}
	// The shop's currency is the cabinet's: one shop, one legal entity, one
	// money. Reading it here keeps a single answer instead of two to reconcile.
	// A shop with no settings row has no currency to label a price with, so the
	// prices wait - the stocks above already went, and they need no money.
	shop, err := w.db.GetSettings()
	if err != nil {
		return pushed, failed, nil
	}
	p, f, err := w.pushPrices(c, shop.Currency)
	return pushed + p, failed + f, err
}

// pushStocks reports halt when an entire call failed: the remaining batches are
// left to the next tick instead of being hammered through.
func (w *Worker) pushStocks(c *Client, warehouse int64) (pushed, failed int, halt bool, err error) {
	rows, err := w.db.OzonStockToPush()
	if err != nil {
		return 0, 0, false, err
	}
	for len(rows) > 0 {
		n := min(kBatchSize, len(rows))
		batch := rows[:n]
		rows = rows[n:]

		items := make([]StockItem, len(batch))
		for i, r := range batch {
			items[i] = StockItem{OfferID: r.OfferID, Stock: r.Stock, WarehouseID: warehouse}
		}
		results, callErr := c.SetStocks(items)
		if callErr != nil {
			w.backoffStocks(batch, callErr)
			return pushed, failed + len(batch), true, nil
		}
		byOffer := make(map[string]ItemResult, len(results))
		for _, res := range results {
			byOffer[res.OfferID] = res
		}
		for _, r := range batch {
			res, found := byOffer[r.OfferID]
			msg := res.Err()
			if !found {
				msg = i18n.KeyOzonNoAnswer
			}
			if msg != "" {
				w.markStockError(r, msg, retryDelay(r.Error))
				failed++
				continue
			}
			if err := w.db.MarkOzonStockPushed(r.ProductID, r.Stock); err != nil {
				log.Warnf("ozon stock sync: mark %s: %v", r.OfferID, err)
				continue
			}
			pushed++
		}
	}
	return pushed, failed, false, nil
}

// pushPrices sends only the prices the owner opted in explicitly: rows with
// price = 0 never reach the platform, so a shop that never touched the price
// column cannot have its Ozon prices moved by us.
func (w *Worker) pushPrices(c *Client, currency string) (pushed, failed int, err error) {
	rows, err := w.db.OzonPriceToPush()
	if err != nil {
		return 0, 0, err
	}
	for len(rows) > 0 {
		n := min(kPriceBatchSize, len(rows))
		batch := rows[:n]
		rows = rows[n:]

		items := make([]PriceItem, len(batch))
		// sent holds what we will remember as pushed - the rounded value, not
		// the one the owner typed.
		sent := make([]int64, len(batch))
		for i, r := range batch {
			items[i], sent[i] = NewPriceItem(r.OfferID, r.Price, currency)
		}
		results, callErr := c.SetPrices(items)
		if callErr != nil {
			w.backoffPrices(batch, callErr)
			return pushed, failed + len(batch), nil
		}
		byOffer := make(map[string]ItemResult, len(results))
		for _, res := range results {
			byOffer[res.OfferID] = res
		}
		for i, r := range batch {
			res, found := byOffer[r.OfferID]
			msg := res.Err()
			if !found {
				msg = i18n.KeyOzonNoAnswer
			}
			if msg != "" {
				w.markPriceError(r, msg, retryDelay(r.Error))
				failed++
				continue
			}
			if err := w.db.MarkOzonPricePushed(r.ProductID, sent[i]); err != nil {
				log.Warnf("ozon price sync: mark %s: %v", r.OfferID, err)
				continue
			}
			pushed++
		}
	}
	return pushed, failed, nil
}

// callDelay: on a 429 we obey Retry-After verbatim - our own backoff has no
// business arguing with the cabinet's limits.
func callDelay(callErr error, fallback time.Duration) time.Duration {
	var apiErr *APIError
	if errors.As(callErr, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	return fallback
}

func (w *Worker) backoffStocks(batch []database.OzonStockRow, callErr error) {
	for _, r := range batch {
		w.markStockError(r, callErr.Error(), callDelay(callErr, retryDelay(r.Error)))
	}
}

func (w *Worker) backoffPrices(batch []database.OzonPriceRow, callErr error) {
	for _, r := range batch {
		w.markPriceError(r, callErr.Error(), callDelay(callErr, retryDelay(r.Error)))
	}
}

func (w *Worker) markStockError(r database.OzonStockRow, msg string, delay time.Duration) {
	if err := w.db.MarkOzonStockError(r.ProductID, msg, time.Now().Add(delay)); err != nil {
		log.Warnf("ozon stock sync: mark error %s: %v", r.OfferID, err)
	}
}

func (w *Worker) markPriceError(r database.OzonPriceRow, msg string, delay time.Duration) {
	if err := w.db.MarkOzonPriceError(r.ProductID, msg, time.Now().Add(delay)); err != nil {
		log.Warnf("ozon price sync: mark error %s: %v", r.OfferID, err)
	}
}

func retryDelay(prevError string) time.Duration {
	if prevError != "" {
		return kNextRetry
	}
	return kFirstRetry
}
