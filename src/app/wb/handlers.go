package wb

import (
	"database/sql"
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
	// Hosts overrides the API addresses - tests point a mock here.
	Hosts Hosts
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
	Barcode   string `json:"barcode"`
	Stock     int64  `json:"stock"`
	Pushed    int64  `json:"pushed"`
	Error     string `json:"error"`
	// RetryAt nil means the row is due on the next pass.
	RetryAt *time.Time `json:"retry_at"`
}

// Price and Pushed are in kopecks, like everywhere else on the wire.
type priceErrorRow struct {
	ProductID int64      `json:"product_id"`
	NmID      int64      `json:"nm_id"`
	Price     int64      `json:"price"`
	Pushed    int64      `json:"pushed"`
	Error     string     `json:"error"`
	RetryAt   *time.Time `json:"retry_at"`
}

type settingsResponse struct {
	Enabled     bool            `json:"enabled"`
	TokenSet    bool            `json:"token_set"`
	Sandbox     bool            `json:"sandbox"`
	WarehouseID string          `json:"warehouse_id"`
	Linked      int             `json:"linked"`
	Unlinked    int             `json:"unlinked"`
	Pending     int             `json:"pending"`
	Failed      int             `json:"failed"`
	StockErrors []stockErrorRow `json:"stock_errors"`
	// InFlight: a price upload is accepted long before it is applied.
	PricePending  int             `json:"price_pending"`
	PriceInFlight int             `json:"price_in_flight"`
	PriceFailed   int             `json:"price_failed"`
	PriceErrors   []priceErrorRow `json:"price_errors"`
	// Sales counters: total, of them oversold and with unmatched items.
	OrdersTotal      int    `json:"orders_total"`
	OrdersOversold   int    `json:"orders_oversold"`
	OrdersUnresolved int    `json:"orders_unresolved"`
	PollError        string `json:"poll_error"`
}

type settingsRequest struct {
	Enabled     bool    `json:"enabled"`
	Token       *string `json:"token"` // nil = leave unchanged
	Sandbox     bool    `json:"sandbox"`
	WarehouseID string  `json:"warehouse_id"`
}

// Prices are in kopecks; an empty Title means only the platform card is left.
type wbLinkRow struct {
	ProductID   int64  `json:"product_id"`
	NmID        int64  `json:"nm_id"`
	Barcode     string `json:"barcode"`
	VendorCode  string `json:"vendor_code"`
	Title       string `json:"title"`
	SKU         string `json:"sku"`
	Stock       int64  `json:"stock"`
	ShopPrice   int64  `json:"shop_price"`
	Price       int64  `json:"price"`
	StockPushed int64  `json:"stock_pushed"`
	PricePushed int64  `json:"price_pushed"`
	InFlight    bool   `json:"in_flight"`
	StockError  string `json:"stock_error"`
	PriceError  string `json:"price_error"`
}

type wbLinksResponse struct {
	Links    []wbLinkRow `json:"links"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

type checkResponse struct {
	Total     int    `json:"total"`
	LegalName string `json:"legal_name"`
	TradeMark string `json:"trade_mark"`
	// NoStockScope: a token without the Marketplace section can never move stock.
	NoStockScope bool `json:"no_stock_scope"`
}

// Reason is empty for a plain "no such card" and set when we refused to guess.
type unlinkedProduct struct {
	ProductID int64  `json:"id"`
	Title     string `json:"title"`
	SKU       string `json:"sku"`
	Reason    string `json:"reason"`
}

// ProductID nil means the sale matched no shop product; the tab warns about it.
type wbOrderRow struct {
	OrderID   int64     `json:"order_id"`
	Status    string    `json:"status"`
	ProductID *int64    `json:"product_id"`
	Title     string    `json:"title"`
	Barcode   string    `json:"barcode"`
	Article   string    `json:"article"`
	NmID      int64     `json:"nm_id"`
	Qty       int       `json:"qty"`
	Oversold  bool      `json:"oversold"`
	CreatedAt time.Time `json:"created_at"`
}

type wbOrdersResponse struct {
	Orders   []wbOrderRow `json:"orders"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

func (h *Handlers) GetSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetWBSettings()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	linked, unlinked, err := h.db.CountWBLinks()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	pending, failed, err := h.db.CountWBStockState()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	bad, err := h.db.ListWBStockErrors()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	lang := h.db.Lang()
	errs := make([]stockErrorRow, 0, len(bad))
	for _, row := range bad {
		errs = append(errs, stockErrorRow{ProductID: row.ProductID, Barcode: row.Barcode,
			Stock: row.Stock, Pushed: row.StockPushed,
			Error: i18n.TIfKey(lang, row.Error), RetryAt: nullTime(row.RetryAt)})
	}
	pricePending, priceInFlight, priceFailed, err := h.db.CountWBPriceState()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	badPrices, err := h.db.ListWBPriceErrors()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	priceErrs := make([]priceErrorRow, 0, len(badPrices))
	for _, row := range badPrices {
		priceErrs = append(priceErrs, priceErrorRow{ProductID: row.ProductID, NmID: row.NmID,
			Price: row.Price, Pushed: row.PricePushed,
			Error: i18n.TIfKey(lang, row.Error), RetryAt: nullTime(row.RetryAt)})
	}
	total, oversold, unresolved, err := h.db.CountWBOrderState()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, settingsResponse{
		Enabled: s.Enabled, TokenSet: s.Token != "", Sandbox: s.Sandbox,
		WarehouseID: s.WarehouseID, Linked: linked, Unlinked: unlinked,
		Pending: pending, Failed: failed, StockErrors: errs,
		PricePending: pricePending, PriceInFlight: priceInFlight,
		PriceFailed: priceFailed, PriceErrors: priceErrs,
		OrdersTotal: total, OrdersOversold: oversold, OrdersUnresolved: unresolved,
		PollError: h.worker.PollError(),
	})
}

func (h *Handlers) SaveSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetWBSettings()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpjson.WriteBadRequest(w, "invalid body")
		return
	}
	s.Enabled, s.Sandbox, s.WarehouseID = req.Enabled, req.Sandbox, req.WarehouseID
	if req.Token != nil {
		s.Token = *req.Token
	}
	if err := h.db.SaveWBSettings(s); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	h.GetSettings(w, r)
}

func (h *Handlers) msg(key string) string { return i18n.T(h.db.Lang(), key) }

// A false second value means there is no token and the owner was already answered.
func (h *Handlers) client(w http.ResponseWriter) (*Client, bool) {
	s, err := h.db.GetWBSettings()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return nil, false
	}
	if s.Token == "" {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyWBNoToken))
		return nil, false
	}
	c := &Client{Token: s.Token, Hosts: h.Hosts}
	if h.Hosts == (Hosts{}) && s.Sandbox {
		c.Hosts = kSandbox
	}
	return c, true
}

func (h *Handlers) Check(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	cards, err := c.ListCards()
	if err != nil {
		httpjson.WriteBadRequest(w, err.Error())
		return
	}
	res := checkResponse{Total: len(cards)}
	// A cabinet that listed its cards is reachable even if this endpoint is not.
	if info, err := c.SellerInfo(); err != nil {
		log.Warnf("wb: seller info: %v", err)
	} else {
		res.LegalName, res.TradeMark = info.Name, info.TradeMark
	}
	// A token without the Marketplace section answers every stock call with 403.
	if _, err := c.ListWarehouses(); err != nil {
		var apiErr *APIError
		res.NoStockScope = errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden
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
		return h.msg(i18n.KeyWBPushBusy)
	case errors.Is(err, ErrBadWarehouse):
		return h.msg(i18n.KeyWBBadWarehouse)
	}
	return err.Error()
}

func (h *Handlers) Push(w http.ResponseWriter, r *http.Request) {
	if err := h.db.ClearWBBackoff(); err != nil {
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

// Platform sales never land in the shop's orders: the platform reports them itself.
func (h *Handlers) Orders(w http.ResponseWriter, r *http.Request) {
	page := channel.PageParam(r)
	total, err := h.db.CountWBOrders()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	list, err := h.db.ListWBOrdersPage(kOrdersPageSize, (page-1)*kOrdersPageSize)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	res := wbOrdersResponse{
		Orders: make([]wbOrderRow, 0, len(list)),
		Total:  total, Page: page, PageSize: kOrdersPageSize,
	}
	for _, o := range list {
		res.Orders = append(res.Orders, wbOrderRow{
			OrderID: o.OrderID, Status: o.Status, ProductID: o.ProductID,
			Title: o.Title, Barcode: o.Barcode, Article: o.Article, NmID: o.NmID,
			Qty: o.Qty, Oversold: o.Oversold, CreatedAt: o.CreatedAt,
		})
	}
	httpjson.WriteOK(w, res)
}

// Paged: a large catalogue must not go into the browser whole.
func (h *Handlers) Links(w http.ResponseWriter, r *http.Request) {
	page := channel.PageParam(r)
	total, err := h.db.CountWBLinkRows()
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	list, err := h.db.ListWBLinksPage(kLinksPageSize, (page-1)*kLinksPageSize)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	lang := h.db.Lang()
	res := wbLinksResponse{
		Links: make([]wbLinkRow, 0, len(list)),
		Total: total, Page: page, PageSize: kLinksPageSize,
	}
	for _, l := range list {
		res.Links = append(res.Links, wbLinkRow{
			ProductID: l.ProductID, NmID: l.NmID, Barcode: l.Barcode,
			VendorCode: l.VendorCode, Title: l.Title, SKU: l.SKU,
			Stock: l.Stock, ShopPrice: l.ShopPrice, Price: l.Price,
			StockPushed: l.StockPushed, PricePushed: l.PricePushed,
			InFlight:   l.InFlight,
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
	found, err := h.db.SetWBPrice(id, req.Price)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	if !found {
		httpjson.WriteNotFound(w, h.msg(i18n.KeyWBNotLinked))
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
	filled, err := h.db.FillWBPrices(req.MarkupBP)
	if err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	httpjson.WriteOK(w, channel.FillPricesResponse{Filled: filled})
}

func (h *Handlers) GetPriceRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.db.WBPriceRules()
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
	if err := h.db.SetWBPriceRules(req.Rules); err != nil {
		httpjson.WriteInternalError(w, err)
		return
	}
	h.GetPriceRules(w, r)
}

// FillPricesByRules is the ladder's counterpart of the flat "+N%" helper.
func (h *Handlers) FillPricesByRules(w http.ResponseWriter, r *http.Request) {
	n, err := h.db.FillWBPricesByRules()
	if err != nil {
		httpjson.WriteBadRequest(w, h.msg(i18n.KeyBadPriceRules))
		return
	}
	h.worker.StockChanged()
	httpjson.WriteOK(w, channel.FillPricesResponse{Filled: n})
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}
