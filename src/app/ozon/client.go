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

// ponytail: ceiling of 20 pages by 1000 - 20 thousand cards in the cabinet. A
// single sole trader never has more; if one does - drop the limit and walk the
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

// RetryAfter carries a 429's Retry-After verbatim: Ozon's limits outrank our backoff.
type APIError struct {
	Status     int
	RetryAfter time.Duration
	msg        string
}

func (e *APIError) Error() string { return e.msg }

// Ozon sends Retry-After in seconds only, never as an HTTP date.
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

// offer_id is the seller's article - that is what we link against products.sku.
type Offer struct {
	ProductID int64  `json:"product_id"`
	OfferID   string `json:"offer_id"`
}

// visibility=ALL: cards that are hidden or archived still have to be linkable.
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
		// A short page is the end too: Ozon may still send a last_id on it.
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

// ItemResult is one row of a stocks or prices push answer - both share the shape.
type ItemResult struct {
	OfferID string      `json:"offer_id"`
	Updated bool        `json:"updated"`
	Errors  []itemError `json:"errors"`
}

// An item with neither updated nor errors is not ok: never credit it as pushed.
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

// One call: the caller splits by kBatchSize, the client never truncates a batch.
func (c *Client) SetStocks(items []StockItem) ([]ItemResult, error) {
	var resp stocksResponse
	if err := c.Post("/v2/products/stocks", stocksRequest{Stocks: items}, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// kPriceBatchSize is the ceiling of /v1/product/import/prices per call.
const kPriceBatchSize = 1000

// Prices are wire strings; old_price "0" clears; *_enabled keeps cabinet settings.
type PriceItem struct {
	AutoActionEnabled    string `json:"auto_action_enabled"`
	CurrencyCode         string `json:"currency_code"`
	OfferID              string `json:"offer_id"`
	Price                string `json:"price"`
	OldPrice             string `json:"old_price"`
	PriceStrategyEnabled string `json:"price_strategy_enabled"`
}

// Ozon takes the minor unit: nothing is rounded, and we remember what we sent.
func NewPriceItem(offerID string, kopecks int64, currency string) (PriceItem, int64) {
	return PriceItem{
		AutoActionEnabled:    "UNKNOWN",
		CurrencyCode:         currency,
		OfferID:              offerID,
		Price:                formatMinor(kopecks),
		OldPrice:             "0",
		PriceStrategyEnabled: "UNKNOWN",
	}, kopecks
}

// formatMinor writes kopecks as the platform's decimal string.
func formatMinor(kopecks int64) string {
	return strconv.FormatInt(kopecks/100, 10) + "." +
		fmt.Sprintf("%02d", kopecks%100)
}

type pricesRequest struct {
	Prices []PriceItem `json:"prices"`
}

type pricesResponse struct {
	Result []ItemResult `json:"result"`
}

// Splitting by kPriceBatchSize is the caller's job, same contract as SetStocks.
func (c *Client) SetPrices(items []PriceItem) ([]ItemResult, error) {
	var resp pricesResponse
	if err := c.Post("/v1/product/import/prices", pricesRequest{Prices: items}, &resp); err != nil {
		return nil, err
	}
	return resp.Result, nil
}

// kPostingLimit is the documented per-page ceiling of /v3/posting/fbs/list.
const kPostingLimit = 50

// ponytail: 40 pages by 50 - 2000 postings per pass. A five-minute tick and a
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

// InProcessAt stays a string: a bad date must not break parsing of the answer.
type Posting struct {
	PostingNumber string           `json:"posting_number"`
	Status        string           `json:"status"`
	InProcessAt   string           `json:"in_process_at"`
	Products      []PostingProduct `json:"products"`
}

// CreatedAt returns the zero time on an unparsable date; the caller substitutes.
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
// A posting without posting_number means the response format drifted: that is an error.
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

// Currency and country follow from the cabinet's legal entity - read, not asked.
func (c *Client) SellerInfo() (*SellerInfo, error) {
	var resp SellerInfo
	if err := c.Post("/v1/seller/info", struct{}{}, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// v2 has no "result" envelope and pages by cursor; v1 is retired.
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
		// The cabinet repeats the cursor on the last page, so has_next alone loops.
		if !resp.HasNext || len(resp.Warehouses) == 0 {
			return out, nil
		}
		cursor = resp.Cursor
	}
}
