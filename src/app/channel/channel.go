// Package channel holds what the marketplace tabs share and no platform owns.
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

// OrphanSample caps the named orphans a cabinet check returns; the count is the answer.
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

// ID as a string: warehouse_id is stored as text in the settings.
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

// The ids come from the cabinet call the tab made on open; never re-ask per page.
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

// idList parses "1,2,3"; a malformed id is skipped rather than failing the request.
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

// Signals is the platform-free part of a worker: the wake channel and last poll error.
type Signals struct {
	wake    chan struct{}
	pollErr atomic.Pointer[string]
}

func NewSignals() *Signals {
	return &Signals{wake: make(chan struct{}, 1)}
}

// Wake is what the worker's loop selects on.
func (s *Signals) Wake() <-chan struct{} { return s.wake }

// StockChanged never blocks: a buffer of one makes this a signal, not an event queue.
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
