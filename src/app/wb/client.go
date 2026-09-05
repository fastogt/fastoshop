package wb

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fastogt/fastoshop/app/i18n"
)

// Wildberries splits its API across four hosts, so a single base URL cannot exist.
type Hosts struct {
	Content     string
	Prices      string
	Marketplace string
	Common      string
}

var kProd = Hosts{
	Content:     "https://content-api.wildberries.ru",
	Prices:      "https://discounts-prices-api.wildberries.ru",
	Marketplace: "https://marketplace-api.wildberries.ru",
	Common:      "https://common-api.wildberries.ru",
}

// Test contour; Common is empty because no common-api-sandbox host exists.
var kSandbox = Hosts{
	Content:     "https://content-api-sandbox.wildberries.ru",
	Prices:      "https://discounts-prices-api-sandbox.wildberries.ru",
	Marketplace: "https://marketplace-api-sandbox.wildberries.ru",
	Common:      "",
}

// kCardsLimit is what /content/v2/get/cards/list takes per page.
const kCardsLimit = 100

// ponytail: ceiling of 200 pages by 100 - 20 thousand cards in the cabinet. Same
// ceiling as the Ozon client, and the same upgrade path if a cabinet outgrows it.
const kMaxPages = 200

// kStockBatch is one PUT /api/v3/stocks/{id}: documented ceiling 1000 barcodes.
const kStockBatch = 1000

// kPriceBatch is what one price upload task carries.
const kPriceBatch = 1000

// kStatusBatch is what one POST /api/v3/orders/status asks about.
const kStatusBatch = 1000

var kHTTP = &http.Client{Timeout: 30 * time.Second}

// Authentication is a single token for every host, unlike Ozon's id plus key pair.
type Client struct {
	Token string
	// Zero value is production; tests point all four fields at one httptest mux.
	Hosts Hosts
}

func (c *Client) content() string     { return orDefault(c.Hosts.Content, kProd.Content) }
func (c *Client) prices() string      { return orDefault(c.Hosts.Prices, kProd.Prices) }
func (c *Client) marketplace() string { return orDefault(c.Hosts.Marketplace, kProd.Marketplace) }
func (c *Client) common() string      { return orDefault(c.Hosts.Common, kProd.Common) }

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// Several methods answer 200 and put the failure in the body.
type errorEnvelope struct {
	Error     bool   `json:"error"`
	ErrorText string `json:"errorText"`
}

func (c *Client) do(method, url string, body, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := kHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &APIError{
			Status:     resp.StatusCode,
			RetryAfter: retryAfter(resp.Header),
			Body:       raw,
			msg: fmt.Sprintf("wb %s: %d: %s", url, resp.StatusCode,
				c.safe(string(raw))),
		}
	}
	if len(raw) == 0 {
		return nil
	}
	var env errorEnvelope
	if json.Unmarshal(raw, &env) == nil && env.Error {
		text := env.ErrorText
		if text == "" {
			text = i18n.KeyWBUnknownReply
		}
		return &APIError{Status: resp.StatusCode, Body: raw,
			msg: fmt.Sprintf("wb %s: %s", url, c.safe(text))}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// A refusal can arrive as a page of HTML, and this text is logged and stored.
const kMaxErrorText = 500

// An API that echoes the request back would carry the token into our logs.
func (c *Client) safe(text string) string {
	if c.Token != "" {
		text = strings.ReplaceAll(text, c.Token, "***")
	}
	if len(text) > kMaxErrorText {
		return text[:kMaxErrorText] + "…"
	}
	return text
}

// errNoSellerInfo marks a contour that does not publish seller details at all.
var errNoSellerInfo = errors.New("wb: no seller-info endpoint in this contour")

// Body is kept: the per-barcode 409 of a stock push is read from it.
type APIError struct {
	Status     int
	RetryAfter time.Duration
	Body       []byte
	msg        string
}

func (e *APIError) Error() string { return e.msg }

// A 429 carries X-Ratelimit-Retry on most methods and Retry-After on some.
func retryAfter(h http.Header) time.Duration {
	for _, name := range []string{"X-Ratelimit-Retry", "Retry-After"} {
		sec, err := strconv.Atoi(h.Get(name))
		if err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return 0
}

// Cards ------------------------------------------------------------------

// Stock is set by the size's barcode, price by the card's nmID.
type Card struct {
	NmID       int64  `json:"nmID"`
	VendorCode string `json:"vendorCode"`
	Title      string `json:"title"`
	Sizes      []Size `json:"sizes"`
}

type Size struct {
	ChrtID   int64    `json:"chrtID"`
	TechSize string   `json:"techSize"`
	WBSize   string   `json:"wbSize"`
	Skus     []string `json:"skus"` // the size's barcodes
}

type cardsCursor struct {
	UpdatedAt string `json:"updatedAt,omitempty"`
	NmID      int64  `json:"nmID,omitempty"`
	Limit     int    `json:"limit"`
	Total     int    `json:"total,omitempty"`
}

type cardsSettings struct {
	Cursor cardsCursor `json:"cursor"`
	Filter cardsFilter `json:"filter"`
}

type cardsFilter struct {
	WithPhoto int `json:"withPhoto"`
}

type cardsRequest struct {
	Settings cardsSettings `json:"settings"`
}

type cardsResponse struct {
	Cards  []Card      `json:"cards"`
	Cursor cardsCursor `json:"cursor"`
}

// withPhoto -1 means cards with and without photos.
func (c *Client) ListCards() ([]Card, error) {
	var out []Card
	cursor := cardsCursor{Limit: kCardsLimit}
	for range kMaxPages {
		var resp cardsResponse
		req := cardsRequest{Settings: cardsSettings{
			Cursor: cursor,
			Filter: cardsFilter{WithPhoto: -1},
		}}
		if err := c.do("POST", c.content()+"/content/v2/get/cards/list", req, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Cards...)
		if len(resp.Cards) < kCardsLimit {
			return out, nil
		}
		cursor = cardsCursor{
			UpdatedAt: resp.Cursor.UpdatedAt,
			NmID:      resp.Cursor.NmID,
			Limit:     kCardsLimit,
		}
	}
	return out, nil
}

// Seller and warehouses --------------------------------------------------

type SellerInfo struct {
	Name      string `json:"name"`
	TradeMark string `json:"tradeMark"`
}

func (c *Client) SellerInfo() (*SellerInfo, error) {
	// An empty Common means the contour has no such host; no fallback to production.
	if c.Hosts != (Hosts{}) && c.Hosts.Common == "" {
		return nil, errNoSellerInfo
	}
	var out SellerInfo
	if err := c.do("GET", c.common()+"/api/v1/seller-info", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type Warehouse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (c *Client) ListWarehouses() ([]Warehouse, error) {
	var out []Warehouse
	if err := c.do("GET", c.marketplace()+"/api/v3/warehouses", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Stocks -----------------------------------------------------------------

// Sku here is the barcode - that is what the platform calls it in this method.
type StockItem struct {
	Sku    string `json:"sku"`
	Amount int64  `json:"amount"`
}

type stocksRequest struct {
	Stocks []StockItem `json:"stocks"`
}

// stocksError is the per-barcode refusal a 409 carries.
type stocksError struct {
	Data []struct {
		Sku  string `json:"sku"`
		Text string `json:"text"`
	} `json:"data"`
}

// Success is 204 with no body, so the whole batch is credited; only a 409 names rows.
func (c *Client) SetStocks(warehouseID int64, items []StockItem) (map[string]string, error) {
	url := fmt.Sprintf("%s/api/v3/stocks/%d", c.marketplace(), warehouseID)
	err := c.do("PUT", url, stocksRequest{Stocks: items}, nil)
	if err == nil {
		return nil, nil
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		return nil, err
	}
	var body stocksError
	if json.Unmarshal(apiErr.Body, &body) != nil || len(body.Data) == 0 {
		return nil, err
	}
	refused := make(map[string]string, len(body.Data))
	for _, d := range body.Data {
		text := d.Text
		if text == "" {
			text = i18n.KeyWBUnknownReply
		}
		refused[d.Sku] = text
	}
	return refused, nil
}

// Prices -----------------------------------------------------------------

// Whole roubles only; no discount field - a zero there would reprice the catalogue.
type PriceItem struct {
	NmID  int64 `json:"nmID"`
	Price int64 `json:"price"`
}

type priceRequest struct {
	Data []PriceItem `json:"data"`
}

type priceUploadResponse struct {
	Data struct {
		ID int64 `json:"id"`
	} `json:"data"`
}

// Rounds up to whole roubles and returns that back in kopecks: store what was sent.
func NewPriceItem(nmID, kopecks int64) (PriceItem, int64) {
	roubles := (kopecks + 99) / 100
	return PriceItem{NmID: nmID, Price: roubles}, roubles * 100
}

// The result is not known yet: PriceTaskStatus polls it a pass or more later.
func (c *Client) UploadPrices(items []PriceItem) (string, error) {
	var resp priceUploadResponse
	if err := c.do("POST", c.prices()+"/api/v2/upload/task",
		priceRequest{Data: items}, &resp); err != nil {
		return "", err
	}
	if resp.Data.ID == 0 {
		return "", fmt.Errorf("wb upload task: no id in reply")
	}
	return strconv.FormatInt(resp.Data.ID, 10), nil
}

// TaskState is how far an upload task got.
type TaskState int

const (
	TaskPending TaskState = iota
	TaskDone
	TaskFailed
)

type taskStatusResponse struct {
	Data struct {
		Status int `json:"status"`
	} `json:"data"`
}

type taskErrorsResponse struct {
	Data struct {
		Details []struct {
			NmID   int64  `json:"nmID"`
			Reason string `json:"reason"`
		} `json:"details"`
	} `json:"data"`
}

// Status 3 is processed, 4 and 5 are refusals; anything else is still moving.
const (
	kTaskStatusDone      = 3
	kTaskStatusCancelled = 4
	kTaskStatusRejected  = 5
)

// Reports how a task ended and, for a failure, why - per card where stated.
func (c *Client) PriceTaskStatus(uploadID string) (TaskState, map[int64]string, error) {
	var resp taskStatusResponse
	url := c.prices() + "/api/v2/history/tasks?uploadID=" + uploadID
	if err := c.do("GET", url, nil, &resp); err != nil {
		return TaskPending, nil, err
	}
	switch resp.Data.Status {
	case kTaskStatusDone:
		return TaskDone, nil, nil
	case kTaskStatusCancelled, kTaskStatusRejected:
		return TaskFailed, c.taskErrors(uploadID), nil
	default:
		return TaskPending, nil, nil
	}
}

// Best effort: the caller has a fallback message when the platform says nothing.
func (c *Client) taskErrors(uploadID string) map[int64]string {
	var resp taskErrorsResponse
	url := c.prices() + "/api/v2/buffer/tasks?uploadID=" + uploadID
	if err := c.do("GET", url, nil, &resp); err != nil {
		return nil
	}
	out := map[int64]string{}
	for _, d := range resp.Data.Details {
		if d.Reason != "" {
			out[d.NmID] = d.Reason
		}
	}
	return out
}

// Orders -----------------------------------------------------------------

// Order is one assembly task: a single item, unlike an Ozon posting.
type Order struct {
	ID        int64    `json:"id"`
	Rid       string   `json:"rid"`
	CreatedAt string   `json:"createdAt"`
	NmID      int64    `json:"nmId"`
	ChrtID    int64    `json:"chrtId"`
	Skus      []string `json:"skus"`
	Article   string   `json:"article"`
	Price     int64    `json:"convertedPrice"`
}

type ordersResponse struct {
	Orders []Order `json:"orders"`
	Next   int64   `json:"next"`
}

// ponytail: 40 pages by 1000 orders in one poll window. A sole trader on FBS
// never sells that in five minutes; if one does, the cursor picks up the rest on
// the next tick anyway.
const (
	kOrdersLimit    = 1000
	kMaxOrderPages  = 40
	kOrderTimeShort = "2006-01-02T15:04:05Z07:00"
)

// dateFrom is unix seconds and next is the platform's own cursor, not an offset.
func (c *Client) ListOrders(since time.Time) ([]Order, error) {
	var out []Order
	next := int64(0)
	for range kMaxOrderPages {
		url := fmt.Sprintf("%s/api/v3/orders?limit=%d&next=%d&dateFrom=%d",
			c.marketplace(), kOrdersLimit, next, since.UTC().Unix())
		var resp ordersResponse
		if err := c.do("GET", url, nil, &resp); err != nil {
			return nil, err
		}
		out = append(out, resp.Orders...)
		if len(resp.Orders) < kOrdersLimit || resp.Next == 0 || resp.Next == next {
			return out, nil
		}
		next = resp.Next
	}
	return out, nil
}

// An unparsable timestamp is not fatal: the caller stamps the sale itself.
func (o *Order) CreatedTime() time.Time {
	t, err := time.Parse(kOrderTimeShort, o.CreatedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// The size's barcode the sale came through - what links the task to a product.
func (o *Order) Barcode() string {
	if len(o.Skus) == 0 {
		return ""
	}
	return o.Skus[0]
}

// WBStatus is the platform's own state, SupplierStatus is ours.
type OrderStatus struct {
	ID             int64  `json:"id"`
	SupplierStatus string `json:"supplierStatus"`
	WBStatus       string `json:"wbStatus"`
}

type statusRequest struct {
	Orders []int64 `json:"orders"`
}

type statusResponse struct {
	Orders []OrderStatus `json:"orders"`
}

// The order list carries no status, so this is a second call by design.
func (c *Client) OrderStatuses(ids []int64) (map[int64]OrderStatus, error) {
	out := map[int64]OrderStatus{}
	for start := 0; start < len(ids); start += kStatusBatch {
		end := min(start+kStatusBatch, len(ids))
		var resp statusResponse
		if err := c.do("POST", c.marketplace()+"/api/v3/orders/status",
			statusRequest{Orders: ids[start:end]}, &resp); err != nil {
			return nil, err
		}
		for _, s := range resp.Orders {
			out[s.ID] = s
		}
	}
	return out, nil
}
