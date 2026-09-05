package ozon

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
	"github.com/fastogt/fastoshop/app/i18n"
)

// Polling faster only spends the method budget: sales within a tick collapse anyway.
const kPushInterval = 5 * time.Minute

// ErrPushBusy - the previous pass is still running.
var ErrPushBusy = errors.New("ozon push already running")

// ErrBadWarehouse travels to the owner, so the handler translates it.
var ErrBadWarehouse = errors.New("warehouse_id must be a number")

// Worker pushes by levels: the link row remembers the last value sent.
type Worker struct {
	*channel.Signals
	db *database.Database
	// BaseURL overrides the Seller API address - tests point it at a mock.
	BaseURL string
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
			log.Warnf("ozon sync: %v", err)
		case pushed > 0 || failed > 0:
			log.Infof("ozon sync: pushed %d, failed %d", pushed, failed)
		}
	}
}

// A disabled or half-configured integration is not an error: the pass is empty.
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
	// Prices need no warehouse, stocks do - a cabinet without one still gets prices.
	var warehouse int64
	if s.WarehouseID != "" {
		warehouse, err = strconv.ParseInt(s.WarehouseID, 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("%w: %q", ErrBadWarehouse, s.WarehouseID)
		}
	}

	c := &Client{ClientID: s.ClientID, APIKey: s.APIKey, BaseURL: w.BaseURL}
	// Orders first: a platform sale must lower our stock before we push levels.
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
	// No shop currency means no label for a price, so prices wait; stocks need none.
	shop, err := w.db.GetSettings()
	if err != nil {
		return pushed, failed, nil
	}
	p, f, err := w.pushPrices(c, shop.Currency)
	return pushed + p, failed + f, err
}

// halt means an entire call failed; the remaining batches wait for the next tick.
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
				w.markStockError(r, msg, channel.RetryDelay(r.Error))
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

// Only opted-in prices go out: a row with price = 0 never reaches the platform.
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
		// sent is what we remember as pushed - the wire value, not the typed one.
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
				w.markPriceError(r, msg, channel.RetryDelay(r.Error))
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

// On a 429 we obey Retry-After verbatim; the cabinet's limits outrank our backoff.
func callDelay(callErr error, fallback time.Duration) time.Duration {
	var apiErr *APIError
	if errors.As(callErr, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	return fallback
}

func (w *Worker) backoffStocks(batch []database.OzonStockRow, callErr error) {
	for _, r := range batch {
		w.markStockError(r, callErr.Error(), callDelay(callErr, channel.RetryDelay(r.Error)))
	}
}

func (w *Worker) backoffPrices(batch []database.OzonPriceRow, callErr error) {
	for _, r := range batch {
		w.markPriceError(r, callErr.Error(), callDelay(callErr, channel.RetryDelay(r.Error)))
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
