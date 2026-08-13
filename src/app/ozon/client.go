package ozon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/fastogt/fastoshop/app/i18n"
)

const kBaseURL = "https://api-seller.ozon.ru"

const kListLimit = 1000

// ponytail: ceiling of 20 pages by 1000 — 20 thousand cards in the cabinet. A
// single sole trader never has more; if one does — drop the limit and walk the
// list in the background, a synchronous HTTP handler will not carry that much.
const kMaxPages = 20

var kHTTP = &http.Client{Timeout: 30 * time.Second}

// Client is a minimal Ozon Seller API client: https://docs.ozon.ru/api/seller/
type Client struct {
	ClientID string
	APIKey   string
	BaseURL  string // defaults to api-seller.ozon.ru; tests point it at an httptest mock
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return kBaseURL
}

func (c *Client) Post(path string, body, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.base()+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Client-Id", c.ClientID)
	req.Header.Set("Api-Key", c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := kHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return &APIError{
			Status:     resp.StatusCode,
			RetryAfter: retryAfter(resp.Header.Get("Retry-After")),
			msg:        fmt.Sprintf("ozon %s: %d: %s", path, resp.StatusCode, raw),
		}
	}
	return json.Unmarshal(raw, out)
}

// APIError is a non-200 answer from the cabinet. RetryAfter carries the
// Retry-After header of a 429 as is: Ozon has its own per-method limits, and our
// backoff must yield to them instead of arguing.
type APIError struct {
	Status     int
	RetryAfter time.Duration
	msg        string
}

func (e *APIError) Error() string { return e.msg }

// retryAfter understands the "seconds" form only: Ozon never sends an HTTP date
// in this header, and guessing from an unparsed value is worse than falling back
// to our own backoff.
func retryAfter(v string) time.Duration {
	sec, err := strconv.Atoi(v)
	if err != nil || sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}

// Request bodies are named structs, never map[string]any (project rule).
type listFilter struct {
	Visibility string `json:"visibility"`
}

type listRequest struct {
	Filter listFilter `json:"filter"`
	LastID string     `json:"last_id"`
	Limit  int        `json:"limit"`
}

type listResponse struct {
	Result struct {
		Items  []Offer `json:"items"`
		LastID string  `json:"last_id"`
		Total  int     `json:"total"`
	} `json:"result"`
}

// Offer is a card in the seller cabinet. offer_id is the seller's article, and
// that is what we link against products.sku.
type Offer struct {
	ProductID int64  `json:"product_id"`
	OfferID   string `json:"offer_id"`
}

// ListProducts returns every card of the cabinet, hidden and archived included
// (visibility=ALL): what is not on sale right now still has to be linked.
func (c *Client) ListProducts() ([]Offer, error) {
	var out []Offer
	lastID := ""
	for range kMaxPages {
		var page listResponse
		err := c.Post("/v3/product/list", listRequest{
			Filter: listFilter{Visibility: "ALL"},
			LastID: lastID,
			Limit:  kListLimit,
		}, &page)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Result.Items...)
		// Ozon leaves last_id empty on the final page, but a non-empty last_id
		// with empty items happens too — a short page is the end as well.
		if page.Result.LastID == "" || len(page.Result.Items) < kListLimit {
			break
		}
		lastID = page.Result.LastID
	}
	return out, nil
}

// kBatchSize is the ceiling of /v2/products/stocks per call.
const kBatchSize = 100

type StockItem struct {
	OfferID     string `json:"offer_id"`
	Stock       int64  `json:"stock"`
	WarehouseID int64  `json:"warehouse_id"`
}

type stocksRequest struct {
	Stocks []StockItem `json:"stocks"`
}

type itemError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ItemResult is one row of a stocks or prices push answer — the two calls share
// the shape.
type ItemResult struct {
	OfferID string      `json:"offer_id"`
	Updated bool        `json:"updated"`
	Errors  []itemError `json:"errors"`
}

// itemErr returns the reason an item counts as rejected, or an empty string on
// success. An item with neither updated nor errors is not "ok by default":
// silently crediting it would mean remembering a level the platform never got.
func itemErr(errs []itemError, updated bool) string {
	for _, e := range errs {
		if e.Message != "" {
			return e.Message
		}
		if e.Code != "" {
			return e.Code
		}
	}
	if updated {
		return ""
	}
	return i18n.KeyOzonUnknownReply
}

func (r ItemResult) Err() string { return itemErr(r.Errors, r.Updated) }

type stocksResponse struct {
	Result []ItemResult `json:"result"`
}

// SetStocks sends stocks in a single call; the caller must split the list by
// kBatchSize itself — the client will not silently truncate someone's batch.
func (c *Client) SetStocks(items []StockItem) ([]ItemResult, error) {
	var resp stocksResponse
	if err := c.Post("/v2/products/stocks", stocksRequest{Stocks: items}, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// kPriceBatchSize is the ceiling of /v1/product/import/prices per call.
const kPriceBatchSize = 1000

// PriceItem mirrors the wire format exactly: price and old_price are STRINGS in
// whole rubles, not kopecks. old_price "0" clears the crossed-out price — we do
// not invent fake discounts, so it is always "0". The two *_enabled fields say
// "leave whatever the cabinet is set to": promotions and price strategies belong
// to the seller, and a stock sync has no business flipping them.
type PriceItem struct {
	AutoActionEnabled    string `json:"auto_action_enabled"`
	CurrencyCode         string `json:"currency_code"`
	OfferID              string `json:"offer_id"`
	Price                string `json:"price"`
	OldPrice             string `json:"old_price"`
	PriceStrategyEnabled string `json:"price_strategy_enabled"`
}

// NewPriceItem builds the wire item for a price stored in kopecks and returns
// the value to remember as pushed, in kopecks. Rounding is up: an extra kopeck
// on the shelf costs the seller nothing, a missing one costs margin. The stored
// value is the rounded one, so the next pass compares like with like and the
// row does not flap between "needs push" and "pushed" forever. currency is the
// cabinet's trading currency — RUB for ozon.ru, BYN for ozon.by.
func NewPriceItem(offerID string, kopecks int64, currency string) (PriceItem, int64) {
	rubles := (kopecks + 99) / 100
	return PriceItem{
		AutoActionEnabled:    "UNKNOWN",
		CurrencyCode:         currency,
		OfferID:              offerID,
		Price:                strconv.FormatInt(rubles, 10),
		OldPrice:             "0",
		PriceStrategyEnabled: "UNKNOWN",
	}, rubles * 100
}

type pricesRequest struct {
	Prices []PriceItem `json:"prices"`
}

type pricesResponse struct {
	Result []ItemResult `json:"result"`
}

// SetPrices sends prices in a single call; splitting by kPriceBatchSize is the
// caller's job, same contract as SetStocks.
func (c *Client) SetPrices(items []PriceItem) ([]ItemResult, error) {
	var resp pricesResponse
	if err := c.Post("/v1/product/import/prices", pricesRequest{Prices: items}, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// kPostingLimit is the documented per-page ceiling of /v3/posting/fbs/list.
const kPostingLimit = 50

// ponytail: 40 pages by 50 — 2000 postings per pass. A five-minute tick and a
// poll window starting at the last cursor will not reach that even for a large
// sole trader; if they do, the cursor simply catches up on the next pass.
const kMaxPostingPages = 40

type postingFilter struct {
	Since string `json:"since"`
	To    string `json:"to"`
}

type postingListRequest struct {
	Dir    string        `json:"dir"`
	Filter postingFilter `json:"filter"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

type PostingProduct struct {
	OfferID  string `json:"offer_id"`
	Quantity int    `json:"quantity"`
}

// Posting is an FBS posting. InProcessAt is a string, not a time.Time: an empty
// or non-standard value in that field must not break parsing of the whole
// answer — the date is here only to be shown to the owner.
type Posting struct {
	PostingNumber string           `json:"posting_number"`
	Status        string           `json:"status"`
	InProcessAt   string           `json:"in_process_at"`
	Products      []PostingProduct `json:"products"`
}

// CreatedAt returns the platform's order time; on an unparsable date it returns
// the zero time and the caller substitutes its own.
func (p Posting) CreatedAt() time.Time {
	t, err := time.Parse(time.RFC3339, p.InProcessAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

type postingListResponse struct {
	Result struct {
		Postings []Posting `json:"postings"`
		HasNext  bool      `json:"has_next"`
	} `json:"result"`
}

// ponytail: v3 is the version we can verify; /v4/posting/fbs/list also exists
// and drops the "result" envelope for a cursor, the direction Ozon is moving
// (v1/warehouse/list is already retired). Migrate on the first real posting,
// when both shapes can be compared against actual data instead of guessed.
//
// ListPostings returns postings for a period. A posting without a
// posting_number is not an "empty record" but a sign the response format has
// drifted from ours: applying that silently would quietly lose a sale, hence an
// error.
func (c *Client) ListPostings(since, to time.Time) ([]Posting, error) {
	var out []Posting
	for page := range kMaxPostingPages {
		var resp postingListResponse
		err := c.Post("/v3/posting/fbs/list", postingListRequest{
			Dir: "asc",
			Filter: postingFilter{
				Since: since.UTC().Format(time.RFC3339),
				To:    to.UTC().Format(time.RFC3339),
			},
			Limit:  kPostingLimit,
			Offset: page * kPostingLimit,
		}, &resp)
		if err != nil {
			return nil, err
		}
		for _, p := range resp.Result.Postings {
			if p.PostingNumber == "" {
				return nil, fmt.Errorf(
					"ozon returned a posting without posting_number: response format changed")
			}
		}
		out = append(out, resp.Result.Postings...)
		if !resp.Result.HasNext || len(resp.Result.Postings) == 0 {
			break
		}
	}
	return out, nil
}

type Warehouse struct {
	ID   int64  `json:"warehouse_id"`
	Name string `json:"name"`
}

type SellerInfo struct {
	Company struct {
		LegalName string `json:"legal_name"`
		Currency  string `json:"currency"`
		Country   string `json:"country"`
	} `json:"company"`
}

// SellerInfo reports what the cabinet itself is: currency and country follow
// from the legal entity the seller registered with, so they are read, not asked.
func (c *Client) SellerInfo() (*SellerInfo, error) {
	var resp SellerInfo
	if err := c.Post("/v1/seller/info", struct{}{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// v2 answers without the "result" envelope the other methods use, and paginates
// by cursor: v1 was retired ("obsolete method cannot be used", verified against
// a live cabinet).
type warehouseListResponse struct {
	Warehouses []Warehouse `json:"warehouses"`
	HasNext    bool        `json:"has_next"`
	Cursor     string      `json:"cursor"`
}

type warehouseListRequest struct {
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
}

// kWarehousePageSize is the platform's ceiling: limit must be within (0, 200].
const kWarehousePageSize = 200

func (c *Client) ListWarehouses() ([]Warehouse, error) {
	var out []Warehouse
	cursor := ""
	for {
		var resp warehouseListResponse
		err := c.Post("/v2/warehouse/list",
			warehouseListRequest{Limit: kWarehousePageSize, Cursor: cursor}, &resp)
		if err != nil {
			return nil, err
		}
		out = append(out, resp.Warehouses...)
		// The cabinet repeats the cursor on the last page, so has_next alone
		// would loop forever on an empty account.
		if !resp.HasNext || len(resp.Warehouses) == 0 {
			return out, nil
		}
		cursor = resp.Cursor
	}
}
