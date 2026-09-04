// Package channel holds what every marketplace tab turned out to share once two
// of them were finished: request parsing, retry arithmetic, the wire shapes the
// tabs answer with, and the worker's wake-up plumbing. Nothing here knows how a
// platform names a card, a warehouse or a price - that stays in the platform's
// own package, and a third channel copies only that part.
package channel

import (
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fastogt/fastoshop/app/database"
)

// CandidatesPageSize is a page of the product picker in a tab.
const CandidatesPageSize = 100

// OrphanSample caps the named orphans a cabinet check returns. The count
// already answers whether they matter; a thousand names would answer nothing
// louder than twenty.
const OrphanSample = 20

// A failed push is retried soon once - the platform may have blinked - and then
// at a pace that does not hammer a cabinet that is genuinely refusing.
//
// ponytail: a two-step backoff ladder instead of an attempt counter - it needs
// no new column in the schema, and a schema change costs a MINOR release. We
// only tell "first failure" from "it was already bad"; if a third class of
// errors shows up that this cannot serve, add attempts INTEGER and count for real.
const (
	FirstRetry = time.Minute
	NextRetry  = 15 * time.Minute
)

func RetryDelay(prevError string) time.Duration {
	if prevError != "" {
		return NextRetry
	}
	return FirstRetry
}

type CandidateRow struct {
	ProductID int64  `json:"product_id"`
	SKU       string `json:"sku"`
	Title     string `json:"title"`
	Stock     int64  `json:"stock"`
	Price     int64  `json:"price"`
	Hidden    bool   `json:"hidden"`
	Published bool   `json:"published"`
}

type CandidatesResponse struct {
	Products []CandidateRow `json:"products"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type PublishRequest struct {
	ProductIDs []int64 `json:"product_ids"`
}

type SetPriceRequest struct {
	Price int64 `json:"price"`
}

type FillPricesRequest struct {
	MarkupBP int64 `json:"markup_bp"`
}

type FillPricesResponse struct {
	Filled int `json:"filled"`
}

type PriceRulesResponse struct {
	Rules []database.PriceRule `json:"rules"`
}

type PriceRulesRequest struct {
	Rules []database.PriceRule `json:"rules"`
}

type OKStatusResponse struct {
	Status string `json:"status"`
}

// ID as a string: warehouse_id is stored as text in the settings, and turning it
// into a number for a dropdown only to turn it back is pointless.
type WarehouseRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type WarehousesResponse struct {
	Warehouses []WarehouseRow `json:"warehouses"`
}

type PushResponse struct {
	Pushed int `json:"pushed"`
	Failed int `json:"failed"`
}

func PageParam(r *http.Request) int {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// CandidateFilter reads the state the tab is showing. The ids come from the
// cabinet call the tab already made when it opened: which products have a card
// on the platform is the platform's answer, not ours, and re-asking it per page
// of a hundred rows is what this endpoint must never do.
//
// Nothing given means the whole catalogue, which is what the table did before
// the filter existed and what it still falls back to.
func CandidateFilter(r *http.Request) database.CandidateFilter {
	f := database.CandidateFilter{Q: strings.TrimSpace(r.URL.Query().Get("q"))}
	f.IDs = idList(r.URL.Query().Get("ids"))
	f.ExcludeIDs = idList(r.URL.Query().Get("exclude"))
	switch r.URL.Query().Get("state") {
	case "linked":
		yes := true
		f.Linked = &yes
	case "unlinked":
		no := false
		f.Linked = &no
	}
	return f
}

// idList parses "1,2,3". A malformed id is skipped rather than failing the
// request: the list is a view filter, and answering with a shorter table beats
// answering with an error the owner cannot act on.
func idList(s string) []int64 {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// Signals is the part of a sync worker that has nothing to do with the
// platform: the wake-up channel and the reason the last order poll did not
// happen. The latter lives in memory rather than in the database: a column
// would cost an ALTER TABLE on live installs, and after a restart the next tick
// fills it in again anyway.
type Signals struct {
	wake    chan struct{}
	pollErr atomic.Pointer[string]
}

func NewSignals() *Signals {
	return &Signals{wake: make(chan struct{}, 1)}
}

// Wake is what the worker's loop selects on.
func (s *Signals) Wake() <-chan struct{} { return s.wake }

// StockChanged wakes the worker without blocking the caller: a buffer of one and
// a non-blocking send make it a "something changed" signal, not an event queue.
func (s *Signals) StockChanged() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Signals) PollError() string {
	if p := s.pollErr.Load(); p != nil {
		return *p
	}
	return ""
}

func (s *Signals) SetPollError(msg string) { s.pollErr.Store(&msg) }
