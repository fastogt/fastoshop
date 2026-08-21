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

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

type Handlers struct {
	db     *database.Database
	worker *Worker
	// Hosts overrides the API addresses — tests point a mock here. Empty in
	// production: the client picks live or sandbox from the settings.
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
	// RetryAt nil means the row is due on the next pass. An error without a
	// "and then what" reads to the owner like the sync gave up.
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
	// Price counters live next to the stock ones instead of replacing them: a
	// price the platform refused must not hide a stock that did not arrive.
	// InFlight is what makes this channel different from Ozon — an upload is
	// accepted long before it is applied, and the owner has to see the wait.
	PricePending  int             `json:"price_pending"`
	PriceInFlight int             `json:"price_in_flight"`
	PriceFailed   int             `json:"price_failed"`
	PriceErrors   []priceErrorRow `json:"price_errors"`
	// Counters of incoming platform sales: total, of them oversold and those that
	// matched no linked product.
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

// Prices are in kopecks: Price is what the owner wants on the platform,
// ShopPrice is the shelf price of the shop. Title empty means the product is
// gone and only the card on the platform is left.
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
	Rules []database.PriceRule `json:"rules"`
}

type priceRulesRequest struct {
	Rules []database.PriceRule `json:"rules"`
}

type checkResponse struct {
	Total     int    `json:"total"`
	LegalName string `json:"legal_name"`
	TradeMark string `json:"trade_mark"`
}

// Reason is empty for the plain "no such card" case and carries an explanation
// when the article did match something we refused to guess at.
type unlinkedProduct struct {
	ProductID int64  `json:"id"`
	Title     string `json:"title"`
	SKU       string `json:"sku"`
	Reason    string `json:"reason"`
}

// unlinkedCard is a card in the cabinet nothing in the shop points at.
type unlinkedCard struct {
	NmID       int64  `json:"nm_id"`
	VendorCode string `json:"vendor_code"`
}

type linkResponse struct {
	Linked           int               `json:"linked"`
	UnlinkedProducts []unlinkedProduct `json:"unlinked_products"`
	UnlinkedCards    []unlinkedCard    `json:"unlinked_cards"`
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

// ProductID nil means the sale could not be matched to a shop product; the front
// end shows such a row as a warning instead of hiding it silently.
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
		writeInternalError(w, err)
		return
	}
	linked, unlinked, err := h.db.CountWBLinks()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	pending, failed, err := h.db.CountWBStockState()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	bad, err := h.db.ListWBStockErrors()
	if err != nil {
		writeInternalError(w, err)
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
		writeInternalError(w, err)
		return
	}
	badPrices, err := h.db.ListWBPriceErrors()
	if err != nil {
		writeInternalError(w, err)
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
		writeInternalError(w, err)
		return
	}
	writeOK(w, settingsResponse{
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
		writeInternalError(w, err)
		return
	}
	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	s.Enabled, s.Sandbox, s.WarehouseID = req.Enabled, req.Sandbox, req.WarehouseID
	if req.Token != nil {
		s.Token = *req.Token
	}
	if err := h.db.SaveWBSettings(s); err != nil {
		writeInternalError(w, err)
		return
	}
	h.GetSettings(w, r)
}

func (h *Handlers) msg(key string) string { return i18n.T(h.db.Lang(), key) }

// client builds a client on the saved token. A false second value means there is
// no token and the answer to the owner has already been sent.
func (h *Handlers) client(w http.ResponseWriter) (*Client, bool) {
	s, err := h.db.GetWBSettings()
	if err != nil {
		writeInternalError(w, err)
		return nil, false
	}
	if s.Token == "" {
		writeBadRequest(w, h.msg(i18n.KeyWBNoToken))
		return nil, false
	}
	c := &Client{Token: s.Token, Hosts: h.Hosts}
	if h.Hosts == (Hosts{}) && s.Sandbox {
		c.Hosts = kSandbox
	}
	return c, true
}

// Check is the "Check" button: a live request with the saved token so the owner
// sees that the cabinet answers at all.
func (h *Handlers) Check(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	cards, err := c.ListCards()
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	res := checkResponse{Total: len(cards)}
	// The seller's name is a nicety, not the point of the check: a cabinet that
	// listed its cards is reachable whether or not this endpoint answers.
	if info, err := c.SellerInfo(); err != nil {
		log.Warnf("wb: seller info: %v", err)
	} else {
		res.LegalName, res.TradeMark = info.Name, info.TradeMark
	}
	writeOK(w, res)
}

// Link matches shop products to cabinet cards by the seller's article. What did
// not match on either side comes back as lists: a card lost silently is a
// product that will not travel to the platform later.
func (h *Handlers) Link(w http.ResponseWriter, r *http.Request) {
	c, ok := h.client(w)
	if !ok {
		return
	}
	cards, err := c.ListCards()
	if err != nil {
		writeBadRequest(w, err.Error())
		return
	}
	products, err := h.db.ListProducts()
	if err != nil {
		writeInternalError(w, err)
		return
	}

	idx := newCardIndex(cards)
	links, missing := matchProducts(products, idx)
	res := linkResponse{UnlinkedProducts: missing, UnlinkedCards: []unlinkedCard{}}
	if res.UnlinkedProducts == nil {
		res.UnlinkedProducts = []unlinkedProduct{}
	}
	matched := make(map[int64]bool, len(links))
	for i := range links {
		if err := h.db.UpsertWBLink(&links[i]); err != nil {
			writeInternalError(w, err)
			return
		}
		matched[links[i].NmID] = true
		res.Linked++
	}
	for _, card := range cards {
		if !matched[card.NmID] {
			res.UnlinkedCards = append(res.UnlinkedCards,
				unlinkedCard{NmID: card.NmID, VendorCode: card.VendorCode})
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

// pushError turns our own sentinels into the owner's language and leaves
// anything else as is: an unexpected failure is more useful verbatim.
func (h *Handlers) pushError(err error) string {
	switch {
	case errors.Is(err, ErrPushBusy):
		return h.msg(i18n.KeyWBPushBusy)
	case errors.Is(err, ErrBadWarehouse):
		return h.msg(i18n.KeyWBBadWarehouse)
	}
	return err.Error()
}

// Push is the "Push now" button: the same pass the worker runs, only synchronous
// and with the counters in the answer.
func (h *Handlers) Push(w http.ResponseWriter, r *http.Request) {
	pushed, failed, err := h.worker.Pass()
	if err != nil {
		writeBadRequest(w, h.pushError(err))
		return
	}
	writeOK(w, pushResponse{Pushed: pushed, Failed: failed})
}

// Orders is the platform sales log. These sales never land in the shop's orders
// — the platform reports them itself, and duplicating would double the revenue
// in the tax CSV — so this is the only place the owner sees them.
func (h *Handlers) Orders(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	total, err := h.db.CountWBOrders()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	list, err := h.db.ListWBOrdersPage(kOrdersPageSize, (page-1)*kOrdersPageSize)
	if err != nil {
		writeInternalError(w, err)
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
	writeOK(w, res)
}

// Links is the linked-products table: what we know about every link, including
// the price the owner set for the platform. Paged, because a shop of 20 000
// products would otherwise send its whole catalogue into the browser.
func (h *Handlers) Links(w http.ResponseWriter, r *http.Request) {
	page := pageParam(r)
	total, err := h.db.CountWBLinkRows()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	list, err := h.db.ListWBLinksPage(kLinksPageSize, (page-1)*kLinksPageSize)
	if err != nil {
		writeInternalError(w, err)
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
	writeOK(w, res)
}

// SetPrice sets the price of one product ON WILDBERRIES, in kopecks. Zero
// switches the management off — the price stays whatever the cabinet holds, we
// simply stop touching it. A product without a link is a 404 and not a silently
// created row.
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
		writeBadRequest(w, h.msg(i18n.KeyWBNegativePrice))
		return
	}
	found, err := h.db.SetWBPrice(id, req.Price)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if !found {
		writeNotFound(w, h.msg(i18n.KeyWBNotLinked))
		return
	}
	writeOK(w, okStatusResponse{Status: "ok"})
}

// FillPrices is the bulk helper "shelf price + N%". It fills only links whose
// price is still zero: the owner's own numbers are never overwritten in bulk, so
// the button is safe to press twice.
func (h *Handlers) FillPrices(w http.ResponseWriter, r *http.Request) {
	var req fillPricesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid body")
		return
	}
	if req.MarkupBP < 0 {
		writeBadRequest(w, h.msg(i18n.KeyWBNegativeMarkup))
		return
	}
	filled, err := h.db.FillWBPrices(req.MarkupBP)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeOK(w, fillPricesResponse{Filled: filled})
}

func (h *Handlers) GetPriceRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.db.WBPriceRules()
	if err != nil {
		writeInternalError(w, err)
		return
	}
	if rules == nil {
		rules = []database.PriceRule{}
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
		writeBadRequest(w, h.msg(i18n.KeyWBBadRules))
		return
	}
	if err := h.db.SetWBPriceRules(req.Rules); err != nil {
		writeInternalError(w, err)
		return
	}
	h.GetPriceRules(w, r)
}

// FillPricesByRules is the ladder's counterpart of the flat "+N%" helper.
func (h *Handlers) FillPricesByRules(w http.ResponseWriter, r *http.Request) {
	n, err := h.db.FillWBPricesByRules()
	if err != nil {
		writeBadRequest(w, h.msg(i18n.KeyWBBadRules))
		return
	}
	h.worker.StockChanged()
	writeOK(w, fillPricesResponse{Filled: n})
}

func nullTime(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func pageParam(r *http.Request) int {
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 {
		return 1
	}
	return page
}
