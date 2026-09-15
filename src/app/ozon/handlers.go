package ozon

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/channel"
	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/httpjson"
	"github.com/fastogt/fastoshop/app/i18n"
)

type Handlers struct {
	db     *database.Database
	worker *Worker
	// BaseURL overrides the Seller API address - tests point a mock here.
	BaseURL string
}

func NewHandlers(db *database.Database, w *Worker) *Handlers {
	return &Handlers{db: db, worker: w}
}

func (h *Handlers) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/settings", h.GetSettings)
	r.Put("/settings", h.SaveSettings)
	r.Post("/check", h.Check)
	r.Post("/warehouses", h.Warehouses)
	r.Post("/push", h.Push)
	r.Get("/orders", h.Orders)
	r.Get("/links", h.Links)
	r.Put("/price/{productID}", h.SetPrice)
	r.Post("/price/fill", h.FillPrices)
	r.Get("/price/rules", h.GetPriceRules)
	r.Put("/price/rules", h.SetPriceRules)
	r.Post("/price/fill-by-rules", h.FillPricesByRules)
	r.Get("/cabinet", h.Cabinet)
	r.Get("/candidates", h.Candidates)
	r.Post("/publish", h.Publish)
	r.Post("/unpublish", h.Unpublish)
	return r
}

// kOrdersPageSize is a page of the platform sales log.
const kOrdersPageSize = 50

// kLinksPageSize is a page of the linked-products table.
const kLinksPageSize = 100

type stockErrorRow struct {
	ProductID int64  `json:"product_id"`
	OfferID   string `json:"offer_id"`
	Stock     int64  `json:"stock"`
	Pushed    int64  `json:"pushed"`
	Error     string `json:"error"`
}

// Price and Pushed are in kopecks, like everywhere else on the wire.
type priceErrorRow struct {
	ProductID int64  `json:"product_id"`
	OfferID   string `json:"offer_id"`
	Price     int64  `json:"price"`
	Pushed    int64  `json:"pushed"`
	Error     string `json:"error"`
}

type settingsResponse struct {
	Enabled     bool   `json:"enabled"`
	ClientID    string `json:"client_id"`
	APIKeySet   bool   `json:"api_key_set"`
	WarehouseID string `json:"warehouse_id"`
	// The shop's currency, not a setting of this channel - see OzonSettings.
	Currency    string          `json:"currency"`
	Linked      int             `json:"linked"`
	Unlinked    int             `json:"unlinked"`
	Pending     int             `json:"pending"`
	Failed      int             `json:"failed"`
	StockErrors []stockErrorRow `json:"stock_errors"`
	// A price rejected by Ozon must not hide a stock that did not arrive.
	PricePending int             `json:"price_pending"`
	PriceFailed  int             `json:"price_failed"`
	PriceErrors  []priceErrorRow `json:"price_errors"`
	// Sales counters: total, of them oversold and with unmatched items.
	OrdersTotal      int    `json:"orders_total"`
	OrdersOversold   int    `json:"orders_oversold"`
	OrdersUnresolved int    `json:"orders_unresolved"`
	PollError        string `json:"poll_error"`
}

type settingsRequest struct {
	Enabled     bool    `json:"enabled"`
	ClientID    string  `json:"client_id"`
	APIKey      *string `json:"api_key"` // nil = leave unchanged
	WarehouseID string  `json:"warehouse_id"`
}

// Prices are in kopecks; an empty Title means only the platform card is left.
type ozonLinkRow struct {
	ProductID   int64  `json:"product_id"`
	OfferID     string `json:"offer_id"`
	Title       string `json:"title"`
	SKU         string `json:"sku"`
	Stock       int64  `json:"stock"`
	ShopPrice   int64  `json:"shop_price"`
	Price       int64  `json:"price"`
	StockPushed int64  `json:"stock_pushed"`
	PricePushed int64  `json:"price_pushed"`
	StockError  string `json:"stock_error"`
	PriceError  string `json:"price_error"`
}

type ozonLinksResponse struct {
	Links    []ozonLinkRow `json:"links"`
	Total    int           `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

type checkResponse struct {
	Total     int    `json:"total"`
	LegalName string `json:"legal_name"`
	Currency  string `json:"currency"`
}

type unlinkedProduct struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	SKU   string `json:"sku"`
}

// ProductID nil means the item matched no shop product; the tab warns about it.
type ozonOrderItemRow struct {
	ProductID *int64 `json:"product_id"`
	OfferID   string `json:"offer_id"`
	Title     string `json:"title"`
	Qty       int    `json:"qty"`
}

type ozonOrderRow struct {
	PostingNumber string             `json:"posting_number"`
	Status        string             `json:"status"`
	Oversold      bool               `json:"oversold"`
	CreatedAt     time.Time          `json:"created_at"`
	Items         []ozonOrderItemRow `json:"items"`
}

type ozonOrdersResponse struct {
	Orders   []ozonOrderRow `json:"orders"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetOzonSettings()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	linked, unlinked, err := h.db.CountOzonLinks()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	pending, failed, err := h.db.CountOzonStockState()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	bad, err := h.db.ListOzonStockErrors()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	lang := h.db.Lang()
	errs := make([]stockErrorRow, 0, len(bad))
	for _, r := range bad {
		errs = append(errs, stockErrorRow{ProductID: r.ProductID, OfferID: r.OfferID,
			Stock: r.Stock, Pushed: r.StockPushed,
			Error: i18n.TIfKey(lang, r.Error)})
	}
	pricePending, priceFailed, err := h.db.CountOzonPriceState()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	badPrices, err := h.db.ListOzonPriceErrors()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	priceErrs := make([]priceErrorRow, 0, len(badPrices))
	for _, r := range badPrices {
		priceErrs = append(priceErrs, priceErrorRow{ProductID: r.ProductID, OfferID: r.OfferID,
			Price: r.Price, Pushed: r.PricePushed,
			Error: i18n.TIfKey(lang, r.Error)})
	}
	total, oversold, unresolved, err := h.db.CountOzonOrderState()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, settingsResponse{
		Enabled: s.Enabled, ClientID: s.ClientID, APIKeySet: s.APIKey != "",
		WarehouseID: s.WarehouseID, Currency: h.shopCurrency(), Linked: linked, Unlinked: unlinked,
		Pending: pending, Failed: failed, StockErrors: errs,
		PricePending: pricePending, PriceFailed: priceFailed, PriceErrors: priceErrs,
		OrdersTotal: total, OrdersOversold: oversold, OrdersUnresolved: unresolved,
		PollError: h.worker.PollError(),
	})
}

// Platform sales never land in the shop's orders: Ozon reports them itself.
func (h *Handlers) Orders(w http.ResponseWriter, r *http.Request) {
	page := channel.PageParam(r)
	total, err := h.db.CountOzonOrders()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	list, err := h.db.ListOzonOrdersPage(kOrdersPageSize, (page-1)*kOrdersPageSize)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	res := ozonOrdersResponse{
		Orders: make([]ozonOrderRow, 0, len(list)),
		Total:  total, Page: page, PageSize: kOrdersPageSize,
	}
	for _, o := range list {
		row := ozonOrderRow{
			PostingNumber: o.PostingNumber, Status: o.Status, Oversold: o.Oversold,
			CreatedAt: o.CreatedAt, Items: make([]ozonOrderItemRow, 0, len(o.Items)),
		}
		for _, it := range o.Items {
			row.Items = append(row.Items, ozonOrderItemRow{
				ProductID: it.ProductID, OfferID: it.OfferID,
				Title: it.Title, Qty: it.Qty,
			})
		}
		res.Orders = append(res.Orders, row)
	}
	httpjson.WriteOK(w, res)
}

func (h *Handlers) SaveSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetOzonSettings()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	s.Enabled, s.ClientID, s.WarehouseID = req.Enabled, req.ClientID, req.WarehouseID
	if req.APIKey != nil {
		s.APIKey = *req.APIKey
	}
	if err := h.db.SaveOzonSettings(s); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	h.GetSettings(w, r)
}

func (h *Handlers) msg(key string) string { return i18n.T(h.db.Lang(), key) }

// A false second value means there are no keys and the owner was already answered.
func (h *Handlers) client(w http.ResponseWriter) (*Client, bool) {
	s, err := h.db.GetOzonSettings()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return nil, false
	}
	if s.ClientID == "" || s.APIKey == "" {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyOzonNoKeys))
		return nil, false
	}
	return &Client{ClientID: s.ClientID, APIKey: s.APIKey, BaseURL: h.BaseURL}, true
}

func (h *Handlers) Check(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	offers, err := c.ListProducts()
	if err != nil {
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	res := checkResponse{Total: len(offers)}
	// The cabinet's currency is reported, not stored; a failure must not fail the check.
	if info, err := c.SellerInfo(); err != nil {
		log.Warnf("ozon: seller info: %v", err)
	} else {
		res.LegalName, res.Currency = info.Company.LegalName, info.Company.Currency
	}
	httpjson.WriteOK(w, res)
}

// The error goes to the owner as text: the tab degrades to typing warehouse_id.
func (h *Handlers) Warehouses(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	list, err := c.ListWarehouses()
	if err != nil {
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	res := channel.WarehousesResponse{Warehouses: make([]channel.WarehouseRow, 0, len(list))}
	for _, wh := range list {
		res.Warehouses = append(res.Warehouses,
			channel.WarehouseRow{ID: strconv.FormatInt(wh.ID, 10), Name: wh.Name})
	}
	httpjson.WriteOK(w, res)
}

// pushError turns our own sentinels into the owner's language, anything else verbatim.
func (h *Handlers) pushError(err error) string {
	switch {
	case errors.Is(err, ErrPushBusy):
		return h.msg(i18n.KeyOzonPushBusy)
	case errors.Is(err, ErrBadWarehouse):
		return h.msg(i18n.KeyOzonBadWarehouse)
	}
	return err.Error()
}

// Empty only on a shop with no settings row, which has no prices either.
func (h *Handlers) shopCurrency() string {
	s, err := h.db.GetSettings()
	if err != nil {
		return ""
	}
	return s.Currency
}

func (h *Handlers) Push(w http.ResponseWriter, r *http.Request) {
	if err := h.db.ClearOzonBackoff(); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	pushed, failed, err := h.worker.Pass()
	if err != nil {
		httpjson.WriteBadRequest(w, h.pushError(err))
		return
	}
	httpjson.WriteOK(w, channel.PushResponse{Pushed: pushed, Failed: failed})
}

// Paged: a large catalogue must not go into the browser whole.
func (h *Handlers) Links(w http.ResponseWriter, r *http.Request) {
	page := channel.PageParam(r)
	total, err := h.db.CountOzonLinkRows()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	list, err := h.db.ListOzonLinksPage(kLinksPageSize, (page-1)*kLinksPageSize)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	lang := h.db.Lang()
	res := ozonLinksResponse{
		Links: make([]ozonLinkRow, 0, len(list)),
		Total: total, Page: page, PageSize: kLinksPageSize,
	}
	for _, l := range list {
		res.Links = append(res.Links, ozonLinkRow{
			ProductID: l.ProductID, OfferID: l.OfferID, Title: l.Title, SKU: l.SKU,
			Stock: l.Stock, ShopPrice: l.ShopPrice, Price: l.Price,
			StockPushed: l.StockPushed, PricePushed: l.PricePushed,
			StockError: i18n.TIfKey(lang, l.StockError),
			PriceError: i18n.TIfKey(lang, l.PriceError),
		})
	}
	httpjson.WriteOK(w, res)
}

// Kopecks; zero switches management off and leaves whatever the cabinet holds.
func (h *Handlers) SetPrice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "productID"), 10, 64)
	if err != nil {
		httpjson.WriteBadRequest(w, "invalid product id")
		return
	}
	var req channel.SetPriceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	if req.Price < 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyNegativePrice))
		return
	}
	found, err := h.db.SetOzonPrice(id, req.Price)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if !found {
		httpjson.WriteNotFound(w, h.msg(i18n.KeyOzonNotLinked))
		return
	}
	httpjson.WriteOK(w, channel.OKStatusResponse{Status: "ok"})
}

// Fills only links whose price is still zero: the owner's own numbers survive.
func (h *Handlers) FillPrices(w http.ResponseWriter, r *http.Request) {
	var req channel.FillPricesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	if req.MarkupBP < 0 {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyNegativeMarkup))
		return
	}
	filled, err := h.db.FillOzonPrices(req.MarkupBP)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, channel.FillPricesResponse{Filled: filled})
}

// GetPriceRules returns the markup ladder of the channel.
func (h *Handlers) GetPriceRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.db.OzonPriceRules()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if rules == nil {
		rules = []database.PriceRule{}
	}
	httpjson.WriteOK(w, channel.PriceRulesResponse{Rules: rules})
}

func (h *Handlers) SetPriceRules(w http.ResponseWriter, r *http.Request) {
	var req channel.PriceRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	if err := database.ValidPriceRules(req.Rules); err != nil {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyBadPriceRules))
		return
	}
	if err := h.db.SetOzonPriceRules(req.Rules); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	h.GetPriceRules(w, r)
}

// FillPricesByRules is the ladder's counterpart of the flat "+N%" helper.
func (h *Handlers) FillPricesByRules(w http.ResponseWriter, r *http.Request) {
	n, err := h.db.FillOzonPricesByRules()
	if err != nil {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyBadPriceRules))
		return
	}
	h.worker.StockChanged()
	httpjson.WriteOK(w, channel.FillPricesResponse{Filled: n})
}
