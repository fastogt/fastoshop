package ozon

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

type Handlers struct {
	db     *database.Database
	worker *Worker
	// BaseURL overrides the Seller API address — tests point a mock here. Empty
	// in production: the client substitutes the live host itself.
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
	r.Post("/link", h.Link)
	r.Post("/warehouses", h.Warehouses)
	r.Post("/push", h.Push)
	r.Get("/orders", h.Orders)
	r.Get("/links", h.Links)
	r.Put("/price/{productID}", h.SetPrice)
	r.Post("/price/fill", h.FillPrices)
	r.Get("/price/rules", h.GetPriceRules)
	r.Put("/price/rules", h.SetPriceRules)
	r.Post("/price/fill-by-rules", h.FillPricesByRules)
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
	Enabled     bool            `json:"enabled"`
	ClientID    string          `json:"client_id"`
	APIKeySet   bool            `json:"api_key_set"`
	WarehouseID string          `json:"warehouse_id"`
	Currency    string          `json:"currency"`
	Linked      int             `json:"linked"`
	Unlinked    int             `json:"unlinked"`
	Pending     int             `json:"pending"`
	Failed      int             `json:"failed"`
	StockErrors []stockErrorRow `json:"stock_errors"`
	// Price counters live next to the stock ones instead of replacing them: a
	// price rejected by Ozon must not hide a stock that did not arrive.
	PricePending int             `json:"price_pending"`
	PriceFailed  int             `json:"price_failed"`
	PriceErrors  []priceErrorRow `json:"price_errors"`
	// Counters of incoming platform sales: total, of them oversold and with
	// items that did not match any linked product.
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
	Currency    string  `json:"currency"`
}

// Prices are in kopecks: Price is what the owner wants on Ozon, ShopPrice is the
// shelf price of the shop. Title empty means the product is gone and only the
// card on the platform is left.
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

type setPriceRequest struct {
	Price int64 `json:"price"`
}

type fillPricesRequest struct {
	MarkupBP int64 `json:"markup_bp"`
}

type fillPricesResponse struct {
	Filled int `json:"filled"`
}

type priceRulesResponse struct {
	Rules []database.OzonPriceRule `json:"rules"`
}

type priceRulesRequest struct {
	Rules []database.OzonPriceRule `json:"rules"`
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

type linkResponse struct {
	Linked           int               `json:"linked"`
	UnlinkedProducts []unlinkedProduct `json:"unlinked_products"`
	UnlinkedOffers   []string          `json:"unlinked_offers"`
}

type okStatusResponse struct {
	Status string `json:"status"`
}

// ID as a string: warehouse_id is stored as text in the settings, and turning it
// into a number for a dropdown only to turn it back is pointless.
type warehouseRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type warehousesResponse struct {
	Warehouses []warehouseRow `json:"warehouses"`
}

type pushResponse struct {
	Pushed int `json:"pushed"`
	Failed int `json:"failed"`
}

// ProductID nil means the item could not be matched to a shop product; the
// front end shows such a row as a warning instead of hiding it silently.
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
		writeInternalError(w, err)
		return
	}
	linked, unlinked, err := h.db.CountOzonLinks()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	pending, failed, err := h.db.CountOzonStockState()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	bad, err := h.db.ListOzonStockErrors()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	lang := h.lang()
	errs := make([]stockErrorRow, 0, len(bad))
	for _, r := range bad {
		errs = append(errs, stockErrorRow{ProductID: r.ProductID, OfferID: r.OfferID,
			Stock: r.Stock, Pushed: r.StockPushed,
			Error: i18n.TIfKey(lang, r.Error)})
	}
	pricePending, priceFailed, err := h.db.CountOzonPriceState()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	badPrices, err := h.db.ListOzonPriceErrors()
	if err != nil {
		writeInternalError(w, err)
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
		writeInternalError(w, err)
		return
	}
	writeOK(w, settingsResponse{
		Enabled: s.Enabled, ClientID: s.ClientID, APIKeySet: s.APIKey != "",
		WarehouseID: s.WarehouseID, Currency: s.Currency, Linked: linked, Unlinked: unlinked,
		Pending: pending, Failed: failed, StockErrors: errs,
		PricePending: pricePending, PriceFailed: priceFailed, PriceErrors: priceErrs,
		OrdersTotal: total, OrdersOversold: oversold, OrdersUnresolved: unresolved,
		PollError: h.worker.PollError(),
	})
}

// Orders is the platform sales log. These sales never land in the shop's orders
// (Ozon reports them itself, duplicating would double the revenue in the tax
// CSV), so this is the only place the owner sees them.
func (h *Handlers) Orders(w http.ResponseWriter, r *http.Request) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	total, err := h.db.CountOzonOrders()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	list, err := h.db.ListOzonOrdersPage(kOrdersPageSize, (page-1)*kOrdersPageSize)
	if err != nil {
		writeInternalError(w, err)
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
	writeOK(w, res)
}

func (h *Handlers) SaveSettings(w http.ResponseWriter, r *http.Request) {
	s, err := h.db.GetOzonSettings()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	currency := req.Currency
	if currency == "" {
		currency = database.OzonCurrencyRUB
	}
	if !database.IsValidOzonCurrency(currency) {
		writeBadRequest(w, h.msg(i18n.KeyOzonBadCurrency))
		return
	}
	s.Enabled, s.ClientID, s.WarehouseID, s.Currency = req.Enabled, req.ClientID, req.WarehouseID, currency
	if req.APIKey != nil {
		s.APIKey = *req.APIKey
	}
	if err := h.db.SaveOzonSettings(s); err != nil {
		writeInternalError(w, err)
		return
	}
	h.GetSettings(w, r)
}

// client builds a client on the saved keys. A false second value means there are
// no keys and the answer to the owner has already been sent.
// lang reports the owner's language. A broken settings row must not blank out
// the error the owner needs to read, so it falls back to the default.
func (h *Handlers) lang() string {
	s, err := h.db.GetSettings()
	if err != nil {
		return i18n.LangRU
	}
	return s.Lang
}

func (h *Handlers) msg(key string) string { return i18n.T(h.lang(), key) }

func (h *Handlers) client(w http.ResponseWriter) (*Client, bool) {
	s, err := h.db.GetOzonSettings()
	if err != nil {
		writeInternalError(w, err)
		return nil, false
	}
	if s.ClientID == "" || s.APIKey == "" {
		writeBadRequest(w, h.msg(i18n.KeyOzonNoKeys))
		return nil, false
	}
	return &Client{ClientID: s.ClientID, APIKey: s.APIKey, BaseURL: h.BaseURL}, true
}

// Check is the "Check" button: a live request with the saved keys so the
// owner sees that the cabinet answers at all.
func (h *Handlers) Check(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	offers, err := c.ListProducts()
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	res := checkResponse{Total: len(offers)}
	// The cabinet's currency follows from the legal entity behind it, so it is
	// read here instead of being asked: a seller who picks the wrong one sends
	// prices in the wrong money. A failure here must not fail the check itself.
	if info, err := c.SellerInfo(); err != nil {
		log.Warnf("ozon: seller info: %v", err)
	} else {
		res.LegalName, res.Currency = info.Company.LegalName, info.Company.Currency
		if database.IsValidOzonCurrency(res.Currency) {
			if err := h.db.SetOzonCurrency(res.Currency); err != nil {
				writeInternalError(w, err)
				return
			}
		}
	}
	writeOK(w, res)
}

// Link matches shop products to cabinet cards by exact equality of products.sku
// and offer_id. What did not match on either side comes back as lists: a card
// lost silently is a product that will not travel to the platform later.
func (h *Handlers) Link(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	offers, err := c.ListProducts()
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	products, err := h.db.ListProducts()
	if err != nil {
		writeInternalError(w, err)
		return
	}

	byOffer := make(map[string]Offer, len(offers))
	for _, o := range offers {
		if o.OfferID != "" {
			byOffer[o.OfferID] = o
		}
	}

	res := linkResponse{UnlinkedProducts: []unlinkedProduct{}, UnlinkedOffers: []string{}}
	matched := make(map[string]bool, len(products))
	for _, p := range products {
		o, found := byOffer[p.SKU]
		if p.SKU == "" || !found {
			res.UnlinkedProducts = append(res.UnlinkedProducts,
				unlinkedProduct{ID: p.ID, Title: p.Title, SKU: p.SKU})
			continue
		}
		link := &database.OzonLink{ProductID: p.ID, OfferID: o.OfferID}
		if err := h.db.UpsertOzonLink(link); err != nil {
			writeInternalError(w, err)
			return
		}
		matched[o.OfferID] = true
		res.Linked++
	}
	for _, o := range offers {
		if o.OfferID != "" && !matched[o.OfferID] {
			res.UnlinkedOffers = append(res.UnlinkedOffers, o.OfferID)
		}
	}
	writeOK(w, res)
}

// Warehouses fills the warehouse dropdown. The error goes to the owner as text:
// the tab degrades to typing warehouse_id by hand, not to an empty list with no
// explanation.
func (h *Handlers) Warehouses(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	list, err := c.ListWarehouses()
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	res := warehousesResponse{Warehouses: make([]warehouseRow, 0, len(list))}
	for _, wh := range list {
		res.Warehouses = append(res.Warehouses,
			warehouseRow{ID: strconv.FormatInt(wh.ID, 10), Name: wh.Name})
	}
	writeOK(w, res)
}

// Push is the "Push now" button: the same pass the worker runs, only
// synchronous and with the counters in the answer.
// pushError turns our own sentinels into the owner's language and leaves
// anything else as is: an unexpected failure is more useful verbatim.
func (h *Handlers) pushError(err error) string {
	switch {
	case errors.Is(err, ErrPushBusy):
		return h.msg(i18n.KeyOzonPushBusy)
	case errors.Is(err, ErrBadWarehouse):
		return h.msg(i18n.KeyOzonBadWarehouse)
	}
	return err.Error()
}

func (h *Handlers) Push(w http.ResponseWriter, r *http.Request) {
	pushed, failed, err := h.worker.Pass()
	if err != nil {
		writeBadRequest(w, h.pushError(err))
		return
	}
	writeOK(w, pushResponse{Pushed: pushed, Failed: failed})
}

// Links is the linked-products table: what we know about every link, including
// the price the owner set for the platform. Paged, because a shop of 20 000
// products would otherwise send its whole catalogue into the browser.
func (h *Handlers) Links(w http.ResponseWriter, r *http.Request) {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	total, err := h.db.CountOzonLinkRows()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	list, err := h.db.ListOzonLinksPage(kLinksPageSize, (page-1)*kLinksPageSize)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	lang := h.lang()
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
	writeOK(w, res)
}

// SetPrice sets the price of one product ON OZON, in kopecks. Zero switches the
// management off — the price stays whatever the cabinet holds, we simply stop
// touching it. A product without a link is a 404 and not a silently created row:
// a price for something unlinked would never be sent anywhere.
func (h *Handlers) SetPrice(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "productID"), 10, 64)
	if err != nil {
		writeBadRequest(w, "invalid product id")
		return
	}
	var req setPriceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	if req.Price < 0 {
		writeBadRequest(w, h.msg(i18n.KeyOzonNegativePrice))
		return
	}
	found, err := h.db.SetOzonPrice(id, req.Price)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !found {
		writeNotFound(w, h.msg(i18n.KeyOzonNotLinked))
		return
	}
	writeOK(w, okStatusResponse{Status: "ok"})
}

// FillPrices is the bulk helper "shelf price + N%". It fills only links whose
// price is still zero: the owner's own numbers are never overwritten in bulk,
// so the button is safe to press twice.
func (h *Handlers) FillPrices(w http.ResponseWriter, r *http.Request) {
	var req fillPricesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	if req.MarkupBP < 0 {
		writeBadRequest(w, h.msg(i18n.KeyOzonNegativeMarkup))
		return
	}
	filled, err := h.db.FillOzonPrices(req.MarkupBP)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, fillPricesResponse{Filled: filled})
}

// GetPriceRules returns the markup ladder of the channel.
func (h *Handlers) GetPriceRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.db.OzonPriceRules()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if rules == nil {
		rules = []database.OzonPriceRule{}
	}
	writeOK(w, priceRulesResponse{Rules: rules})
}

func (h *Handlers) SetPriceRules(w http.ResponseWriter, r *http.Request) {
	var req priceRulesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	if err := database.ValidPriceRules(req.Rules); err != nil {
		writeBadRequest(w, h.msg(i18n.KeyOzonBadRules))
		return
	}
	if err := h.db.SetOzonPriceRules(req.Rules); err != nil {
		writeInternalError(w, err)
		return
	}
	h.GetPriceRules(w, r)
}

// FillPricesByRules is the ladder's counterpart of the flat "+N%" helper.
func (h *Handlers) FillPricesByRules(w http.ResponseWriter, r *http.Request) {
	n, err := h.db.FillOzonPricesByRules()
	if err != nil {
		writeBadRequest(w, h.msg(i18n.KeyOzonBadRules))
		return
	}
	h.worker.StockChanged()
	writeOK(w, fillPricesResponse{Filled: n})
}
