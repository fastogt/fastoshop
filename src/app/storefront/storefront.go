package storefront

import (
	"embed"
	"encoding/xml"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/media"
	"time"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/style.css
var styleCSS string

type Storefront struct {
	db       *database.Database
	baseURL  string
	uploads  string
	index    *template.Template
	product  *template.Template
	cart     *template.Template
	info     *template.Template
	contacts *template.Template
	// Named with a suffix: the handlers are Category and Categories, and a field
	// may not share a name with a method.
	categoryTpl   *template.Template
	categoriesTpl *template.Template
	// OnStockChange wakes the marketplace sync after an order deducts stock. A
	// field rather than a constructor argument: the link is one-way and optional.
	OnStockChange func()
}

func (s *Storefront) stockChanged() {
	if s.OnStockChange != nil {
		s.OnStockChange()
	}
}

func New(db *database.Database, baseURL, uploadsDir string) *Storefront {
	base := template.Must(template.ParseFS(templatesFS, "templates/base.html"))
	return &Storefront{
		db: db, baseURL: strings.TrimRight(baseURL, "/"), uploads: uploadsDir,
		index:         template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/index.html")),
		product:       template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/product.html")),
		cart:          template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/cart.html")),
		info:          template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/info.html")),
		contacts:      template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/contacts.html")),
		categoryTpl:   template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/category.html")),
		categoriesTpl: template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/categories.html")),
	}
}

// HeadAsGet lets HEAD reach handlers registered as GET. chi answers 405
// otherwise, and HEAD is what monitoring, link checkers and some crawlers use to
// see whether a page is alive - a storefront that refuses it looks broken from
// the outside. The body is dropped by net/http itself: the server still sees the
// original HEAD request and suppresses it.
//
// Exported because it has to sit on the outermost router: a method-specific
// route registered there (/admin*) makes chi answer 405 before this router is
// ever reached, and the storefront's own copy never runs.
func HeadAsGet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			probe := r.Clone(r.Context())
			probe.Method = http.MethodGet
			next.ServeHTTP(w, probe)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Storefront) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(HeadAsGet)
	r.Get("/", s.Index)
	r.Get("/p/{slug}", s.Product)
	r.Get("/cart", s.Cart)
	r.Get("/info", s.Info)
	r.Get("/contacts", s.Contacts)
	r.Get("/c", s.Categories)
	r.Get("/c/*", s.Category)
	r.Post("/cart/add", s.CartAdd)
	r.Post("/cart/update", s.CartUpdate)
	r.Post("/cart/order", s.CartOrder)
	r.Get("/sitemap.xml", s.Sitemap)
	r.Get("/yml.xml", s.YML)
	r.Get("/gmc.xml", s.GMC)
	r.Get("/robots.txt", s.Robots)
	r.Get("/llms.txt", s.LlmsTxt)
	r.Get("/favicon.svg", s.Favicon)
	r.Get("/favicon.ico", s.FaviconICO)
	r.Get("/nophoto.svg", s.NoPhoto)
	return r
}

func priceStr(minor int64) string {
	return fmt.Sprintf("%.2f", float64(minor)/100)
}

// kCatalogPageSize - ~60 cards ≈ 100–200 KB of HTML: the page stays light for
// mobile and for the crawler, and a 20,000-item catalogue never renders in one
// piece.
// A search box is not a text field: anything longer than this is a bot or a
// paste, and it has no business reaching the database or the page title.
const kMaxQueryRunes = 100

const kCatalogPageSize = 60

// product_images.path holds either a local file name (uploaded via the admin)
// or an absolute source URL (import no longer downloads photos).
func isRemoteImage(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

func imageURL(path string) string {
	if isRemoteImage(path) {
		return path
	}
	return "/uploads/" + path
}

// thumbURL is what the catalogue grid shows. A 220 px card has no business
// pulling the supplier's 150 KB original, and there are sixty of them on a
// page. A photo with no small copy - uploaded before thumbnails existed, or
// still a link to the supplier - is served as it is: a hole in the grid would
// be worse than a heavy image.
//
// ponytail: one stat per card, sixty per page. Microseconds on a local disk;
// an in-memory cache is for the day the catalogue moves to network storage.
func (s *Storefront) thumbURL(path string) string {
	if media.HasThumb(s.uploads, path) {
		return "/uploads/" + media.ThumbName(path)
	}
	return imageURL(path)
}

// absImageURL - for og:image and JSON-LD, where an absolute address is required.
func (s *Storefront) absImageURL(path string) string {
	if isRemoteImage(path) {
		return path
	}
	return s.baseURL + "/uploads/" + path
}

// orderItemJSON is the snapshot of a line item in orders.items_json. The same
// format is read by the CSV export (handler/orders.go, orderItem).
type orderItemJSON struct {
	SKU   string `json:"sku"`
	Title string `json:"title"`
	Price int64  `json:"price"`
	Qty   int    `json:"qty"`
}

func (s *Storefront) shop() *database.Settings {
	st, err := s.db.GetSettings()
	if err != nil {
		return &database.Settings{ShopName: "Магазин"}
	}
	if st.ShopName == "" {
		st.ShopName = "Магазин"
	}
	return st
}

type cardVM struct {
	database.Product
	ImageURL string
	PriceStr string
}

// categoryVM is a node of the catalogue tree as the page shows it: the leaf
// name for the eye, the full path for the address, the count for the buyer.
type categoryVM struct {
	Name  string
	URL   string
	Count int
}

// filterVM is one link of the sorting and stock strip. Filters are links, not a
// form with JavaScript: the server renders the result, and the state lives in
// the address, so a filtered listing can be sent to someone in a message.
type filterVM struct {
	Name   string
	URL    string
	Active bool
}

// crumbVM is one step of the breadcrumbs, from the catalogue root to the page.
// Position is counted here rather than in the template: BreadcrumbList numbers
// its items from one, and html/template has no arithmetic.
type crumbVM struct {
	Name     string
	URL      string
	Position int
}

// imageVM - ready-made image addresses: relative for <img>, absolute for
// og:image and JSON-LD.
type imageVM struct {
	URL    string
	AbsURL string
}

// pageVM is the template data. One struct for both pages: a typed model
// instead of map[string]any (project rule: no any in signatures).
type pageVM struct {
	Shop     *database.Settings
	BaseURL  string
	CSS      template.CSS
	Products []cardVM
	P        *database.Product
	Images   []imageVM
	PriceStr string
	// PriceValidUntil keeps the offer fresh in search results: without a date a
	// price is eventually treated as stale and dropped from the snippet. A month
	// ahead, recomputed on every render - a shop that reprices weekly stays
	// truthful, and one that never does still looks current.
	PriceValidUntil string
	MetaDescription string
	// Specs are the measurements the owner filled in - only those. A product
	// nobody weighed shows no specs block at all rather than a table of dashes.
	Specs specVM
	// The same measurements for the markup, in the units they are stored in and
	// as plain numbers: a template cannot dereference a pointer, and 0 is the
	// "not set" that the markup must skip rather than publish.
	WeightG  int64
	LengthMM int64
	WidthMM  int64
	HeightMM int64
	// OrderLinks are the "order in one message" buttons - plain links to the
	// owner's messengers with the product already written into the text. Empty
	// when no messenger is set, which is the default.
	OrderLinks []orderLinkVM
	Canonical  string
	NoIndex    bool
	Query      string
	FoundStr   string
	Category   string
	// CategoryText is the owner's own words above the listing, and the first
	// sentences of it become the page description: generated text is the same on
	// every page of every shop, and a search engine has seen it a million times.
	CategoryText string
	Crumbs       []crumbVM
	// CrumbsEnd is the position the product itself takes in BreadcrumbList,
	// after every category above it.
	CrumbsEnd  int
	Children   []categoryVM
	Categories []categoryVM
	Filters    []filterVM
	Page       int
	Pages      int
	PrevURL    string
	NextURL    string
	Ordered    bool
	Cart       []cartRowVM
	TotalStr   string
	CartCount  int
	Dropped    bool
	// NoContact - the buyer left neither a phone nor an email, so the order was
	// not created: an order nobody can be reached about is not an order.
	NoContact bool
	SoldOut   string
	// What the buyer had already typed when the order was refused. Rendered
	// back into the form: a phone user who loses their name and comment to a
	// validation error does not retype them - they leave.
	FormName    string
	FormComment string
}

// categoryURL is the address of a node of the tree: every segment of the path
// transliterated, joined back with slashes. A category is a page of its own -
// the landing page for "купить X" - not a query parameter on the catalogue.
func categoryURL(path string) string {
	if path == "" {
		return "/"
	}
	segments := strings.Split(path, database.CategorySep)
	for i, seg := range segments {
		segments[i] = database.Slugify(seg)
	}
	return "/c/" + strings.Join(segments, "/")
}

// catalogURL is the catalogue page address with everything the buyer chose kept
// in it: the category in the path, the search and the filters in the query. The
// first page never carries ?page=, so one set of products has exactly one URL.
func catalogURL(f database.CatalogFilter, page int) string {
	q := url.Values{}
	if f.Query != "" {
		q.Set("q", f.Query)
	}
	if f.Sort != "" {
		q.Set("sort", f.Sort)
	}
	if f.Desc {
		q.Set("desc", "1")
	}
	if f.InStock {
		q.Set("instock", "1")
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	base := categoryURL(f.Category)
	if len(q) == 0 {
		return base
	}
	return base + "?" + q.Encode()
}

// canonicalURL is the address without the filters. Sorting and "in stock" show
// the same goods in another order - one page for a search engine, several for a
// buyer - so every variant points at the plain one.
func canonicalURL(category string, page int) string {
	return catalogURL(database.CatalogFilter{Category: category}, page)
}

// foundStr - «нашёлся 1 товар» / «2 товара» / «5 товаров». Pluralization lives
// here, not in the template: the storefront renders in the language of the
// products, and the Russian numeral rule is one for the whole shop.
func foundStr(n int) string {
	word := "товаров"
	switch {
	case n%10 == 1 && n%100 != 11:
		word = "товар"
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		word = "товара"
	}
	return fmt.Sprintf("нашлось %d %s", n, word)
}

func (s *Storefront) Index(w http.ResponseWriter, r *http.Request) {
	// ?category= is where categories lived before they had pages of their own.
	// The links are in the wild - in the index, in bookmarks - so they move
	// permanently instead of dying.
	if old := r.URL.Query().Get("category"); old != "" {
		http.Redirect(w, r, categoryURL(old), http.StatusMovedPermanently)
		return
	}
	s.listing(w, r, "", s.index)
}

// Category is the landing page of a node of the tree: the page a search for
// "купить КПБ евро оптом" should arrive at. It renders the node's own products
// and everything below it, so a parent is never an empty page.
func (s *Storefront) Category(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.db.VisibleCategories()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	want := strings.Trim(chi.URLParam(r, "*"), "/")
	for _, n := range nodes {
		if strings.TrimPrefix(categoryURL(n.Path), "/c/") == want {
			s.listing(w, r, n.Path, s.categoryTpl)
			return
		}
	}
	// The address may be one the owner renamed away. It is in the index and in
	// somebody's bookmarks, so it moves rather than dies - otherwise tidying the
	// tree would throw away everything the page had earned.
	if to, ok, err := s.db.CategoryRedirectBySlug(want); err == nil && ok {
		http.Redirect(w, r, "/c/"+to, http.StatusMovedPermanently)
		return
	}
	// An unknown or emptied category is a 404, not an empty listing: a soft 404
	// keeps the address in the index and spends the crawl budget on nothing.
	http.NotFound(w, r)
}

// Categories is the index of the tree - one page linking to every node. Without
// it a crawler reaching the home page finds only the top level, and the deeper
// landing pages stay invisible.
func (s *Storefront) Categories(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.db.VisibleCategories()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	if len(nodes) == 0 {
		http.NotFound(w, r)
		return
	}
	data := pageVM{Shop: s.shop(), BaseURL: s.baseURL, CSS: template.CSS(styleCSS),
		CartCount: cartCount(r), Canonical: s.baseURL + "/c",
		Categories: categoryVMs(nodes)}
	if err := s.categoriesTpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Errorf("render categories: %v", err)
	}
}

func categoryVMs(nodes []database.Category) []categoryVM {
	out := make([]categoryVM, 0, len(nodes))
	for _, n := range nodes {
		segments := strings.Split(n.Path, database.CategorySep)
		out = append(out, categoryVM{Name: segments[len(segments)-1],
			URL: categoryURL(n.Path), Count: n.Count})
	}
	return out
}

// children returns the nodes one level below path - the links that let a buyer
// and a crawler walk down the tree.
func children(nodes []database.Category, path string) []categoryVM {
	prefix := path + database.CategorySep
	depth := 1
	if path != "" {
		depth = len(strings.Split(path, database.CategorySep)) + 1
	}
	var out []database.Category
	for _, n := range nodes {
		if path != "" && !strings.HasPrefix(n.Path, prefix) {
			continue
		}
		if len(strings.Split(n.Path, database.CategorySep)) == depth {
			out = append(out, n)
		}
	}
	return categoryVMs(out)
}

func crumbs(path string) []crumbVM {
	segments := strings.Split(path, database.CategorySep)
	out := make([]crumbVM, 0, len(segments))
	for i, seg := range segments {
		out = append(out, crumbVM{Name: seg,
			URL:      categoryURL(strings.Join(segments[:i+1], database.CategorySep)),
			Position: i + 2, // 1 is the catalogue root
		})
	}
	return out
}

// filterLinks renders the strip a buyer clicks: the shop's own order, price up
// and down, and "in stock" as a toggle. Clicking a filter drops the page number
// - page 7 of another ordering shows goods the buyer never asked for.
func filterLinks(f database.CatalogFilter) []filterVM {
	sorts := []struct {
		name string
		sort string
		desc bool
	}{
		{"по умолчанию", "", false},
		{"сначала дешёвые", "price", false},
		{"сначала дорогие", "price", true},
		{"по названию", "title", false},
	}
	out := make([]filterVM, 0, len(sorts)+1)
	for _, s := range sorts {
		v := f
		v.Sort, v.Desc = s.sort, s.desc
		out = append(out, filterVM{Name: s.name, URL: catalogURL(v, 1),
			Active: f.Sort == s.sort && f.Desc == s.desc})
	}
	stock := f
	stock.InStock = !f.InStock
	out = append(out, filterVM{Name: "только в наличии", URL: catalogURL(stock, 1),
		Active: f.InStock})
	return out
}

// metaFrom cuts a description out of the owner's text: whole sentences up to
// the length a snippet shows, so the tail is never a word broken in half.
func metaFrom(text string) string {
	text = strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
	if len([]rune(text)) <= 160 {
		return text
	}
	cut := string([]rune(text)[:160])
	if i := strings.LastIndexAny(cut, ".!?"); i > 60 {
		return cut[:i+1]
	}
	if i := strings.LastIndex(cut, " "); i > 0 {
		return cut[:i] + "…"
	}
	return cut
}

func (s *Storefront) listing(w http.ResponseWriter, r *http.Request, category string, tpl *template.Template) {
	// The buyer's search. A shop the size of a marketplace catalogue cannot be
	// browsed page by page, and the storefront has no JavaScript to search with -
	// so it is a plain GET form and a server-rendered result.
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) > kMaxQueryRunes {
		query = string([]rune(query)[:kMaxQueryRunes])
	}
	filter := database.CatalogFilter{Category: category, Query: query,
		Sort: r.URL.Query().Get("sort"), Desc: r.URL.Query().Get("desc") == "1",
		InStock: r.URL.Query().Get("instock") == "1"}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		// Garbage in ?page= is a 404, not an empty listing: soft 404s poison the index.
		if err != nil || n < 1 {
			http.NotFound(w, r)
			return
		}
		if n == 1 {
			http.Redirect(w, r, catalogURL(filter, 1), http.StatusMovedPermanently)
			return
		}
		page = n
	}
	total, err := s.db.CountVisibleProducts(filter)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	pages := max((total+kCatalogPageSize-1)/kCatalogPageSize, 1)
	if page > pages {
		http.NotFound(w, r)
		return
	}
	products, err := s.db.ListVisibleProductsPage(filter,
		kCatalogPageSize, (page-1)*kCatalogPageSize)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	cards := make([]cardVM, 0, len(products))
	for _, p := range products {
		vm := cardVM{Product: p, PriceStr: priceStr(p.Price)}
		if imgs, _ := s.db.ListImages(p.ID); len(imgs) > 0 {
			vm.ImageURL = s.thumbURL(imgs[0].Path)
		}
		cards = append(cards, vm)
	}
	data := pageVM{Shop: s.shop(), BaseURL: s.baseURL,
		CSS: template.CSS(styleCSS), Products: cards, CartCount: cartCount(r),
		Canonical: s.baseURL + canonicalURL(category, page), Page: page, Pages: pages,
		Query: query, FoundStr: foundStr(total)}
	data.Filters = filterLinks(filter)
	// The tree: children to walk down into, crumbs to walk back up. Read once
	// here - the home page needs the top level, a category needs its own level.
	if nodes, err := s.db.VisibleCategories(); err == nil {
		data.Children = children(nodes, category)
	}
	if category != "" {
		segments := strings.Split(category, database.CategorySep)
		data.Category = segments[len(segments)-1]
		data.Crumbs = crumbs(category)
		if text, err := s.db.CategoryTextOf(category); err == nil {
			data.CategoryText = text
			data.MetaDescription = metaFrom(text)
		}
	}
	// A result page is thin, endless and duplicates the catalogue: search belongs
	// out of the index. noindex,follow rather than robots.txt - a page blocked
	// from crawling is a page whose noindex is never read.
	if query != "" {
		data.NoIndex = true
		data.Canonical = ""
		// On an empty result the counter stays silent: below there is already a
		// message with the query itself and a link back to the catalogue, and two
		// identical phrases in a row are noise.
		if total == 0 {
			data.FoundStr = ""
		}
	}
	if page > 1 {
		data.PrevURL = catalogURL(filter, page-1)
	}
	if page < pages {
		data.NextURL = catalogURL(filter, page+1)
	}
	if err := tpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Errorf("render listing: %v", err)
	}
}

// specVM carries the measurements ready for the eye - a named field each, not a
// list of label-value pairs: a spec is a known thing, and adding one should be
// a field here rather than another entry in a bag of strings. Empty means the
// owner did not state it, and the page says nothing at all instead.
type specVM struct {
	Weight string
	Size   string
	// Props are the characteristics the source stated, in its order. Weight and
	// size are not among them: they are named fields the shop does arithmetic
	// with, and one number with two homes is one home too many.
	Props []specProp
}

type specProp struct {
	Name  string
	Value string
}

// Empty reports whether there is anything to show, so the template can drop the
// whole block rather than render an empty table.
func (s specVM) Empty() bool {
	return s.Weight == "" && s.Size == "" && len(s.Props) == 0
}

// specs formats what is set. Grams and millimetres are what we store -
// arithmetic wants one unit - but a buyer reads kilograms and centimetres.
func specs(p *database.Product, hidden map[string]bool) specVM {
	var out specVM
	if p.WeightG != nil {
		out.Weight = weightStr(*p.WeightG)
	}
	// All three or none: "длина 30 см" on its own tells a buyer nothing about
	// whether the thing fits.
	if p.LengthMM != nil && p.WidthMM != nil && p.HeightMM != nil {
		out.Size = fmt.Sprintf("%s × %s × %s см",
			cmStr(*p.LengthMM), cmStr(*p.WidthMM), cmStr(*p.HeightMM))
	}
	for _, prm := range p.Params {
		if hidden[prm.Name] {
			continue
		}
		if v := propStr(prm.Value); v != "" {
			out.Props = append(out.Props, specProp{Name: prm.Name, Value: v})
		}
	}
	return out
}

// hiddenParams is what the owner ticked off in the settings. A failure shows
// everything rather than nothing: a page missing its characteristics is worse
// than a page carrying one the owner would rather hide.
func (s *Storefront) hiddenParams() map[string]bool {
	h, err := s.db.HiddenParams()
	if err != nil {
		return nil
	}
	return h
}

// propStr renders a characteristic for a buyer. A marketplace states one value
// as a list of one, so a list is joined rather than shown with its brackets;
// anything a buyer cannot read is left out instead of printed as Go syntax.
func propStr(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "да"
		}
		return "нет"
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if s := propStr(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// value flattens "not set" to 0 for the template, which has no notion of nil.
func value(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func weightStr(g int64) string {
	if g >= 1000 {
		return strings.TrimSuffix(strings.TrimRight(
			fmt.Sprintf("%.2f", float64(g)/1000), "0"), ".") + " кг"
	}
	return fmt.Sprintf("%d г", g)
}

func cmStr(mm int64) string {
	return strings.TrimSuffix(strings.TrimRight(
		fmt.Sprintf("%.1f", float64(mm)/10), "0"), ".")
}

func (s *Storefront) Product(w http.ResponseWriter, r *http.Request) {
	p, err := s.db.GetVisibleProductBySlug(chi.URLParam(r, "slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	raw, _ := s.db.ListImages(p.ID)
	imgs := make([]imageVM, 0, len(raw))
	for _, im := range raw {
		imgs = append(imgs, imageVM{URL: imageURL(im.Path), AbsURL: s.absImageURL(im.Path)})
	}
	desc := p.Description
	if len([]rune(desc)) > 160 {
		desc = string([]rune(desc)[:157]) + "…"
	}
	shop := s.shop()
	data := pageVM{Shop: shop, BaseURL: s.baseURL,
		CSS: template.CSS(styleCSS), P: p, Images: imgs,
		PriceStr: priceStr(p.Price), PriceValidUntil: time.Now().AddDate(0, 1, 0).Format("2006-01-02"),
		OrderLinks:      orderLinks(shop, p, s.baseURL+"/p/"+p.Slug),
		MetaDescription: desc,
		Specs:           specs(p, s.hiddenParams()),
		CartCount:       cartCount(r)}
	data.WeightG, data.LengthMM = value(p.WeightG), value(p.LengthMM)
	data.WidthMM, data.HeightMM = value(p.WidthMM), value(p.HeightMM)
	if p.Category != "" {
		data.Crumbs = crumbs(p.Category)
		data.CrumbsEnd = len(data.Crumbs) + 2
	}
	if err := s.product.ExecuteTemplate(w, "base", data); err != nil {
		log.Errorf("render product: %v", err)
	}
}

// Info publishes delivery, payment and returns. A shop that does not say how it
// ships and how it takes money is rejected by Yandex and Google shopping alike,
// and the buyer has nowhere to read the terms they are agreeing to. 404 while
// the owner has not written them: an empty page under a link in every footer is
// worse than no link.
func (s *Storefront) Info(w http.ResponseWriter, r *http.Request) {
	shop := s.shop()
	if shop.Terms == "" {
		http.NotFound(w, r)
		return
	}
	data := pageVM{Shop: shop, BaseURL: s.baseURL, CSS: template.CSS(styleCSS),
		CartCount: cartCount(r), Canonical: s.baseURL + "/info"}
	if err := s.info.ExecuteTemplate(w, "base", data); err != nil {
		log.Errorf("render info: %v", err)
	}
}

// Contacts is a page a shop is expected to have and this one had nowhere: the
// buyer looks for whom they are paying, and both search engines weigh a
// published contact when they rank a shop. It writes nothing new - the phone
// and the legal details are already in settings - so an owner who filled the
// footer gets the page for free.
//
// The owner's email is on it. It doubles as the admin login, which is why it
// was left off at first, but a contact address is what a buyer looks for and
// what a search engine counts. Secrecy of a login is not a defence anyway: the
// password is, and guessing it is now slowed down deliberately (see the login
// throttle in app/handler).
func (s *Storefront) Contacts(w http.ResponseWriter, r *http.Request) {
	shop := s.shop()
	if shop.ShopPhone == "" && shop.Requisites == "" {
		http.NotFound(w, r)
		return
	}
	data := pageVM{Shop: shop, BaseURL: s.baseURL, CSS: template.CSS(styleCSS),
		CartCount: cartCount(r), Canonical: s.baseURL + "/contacts"}
	if err := s.contacts.ExecuteTemplate(w, "base", data); err != nil {
		log.Errorf("render contacts: %v", err)
	}
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

func (s *Storefront) Sitemap(w http.ResponseWriter, r *http.Request) {
	products, err := s.db.ListVisibleProductsPage(database.CatalogFilter{}, -1, 0)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	set := sitemapSet{NS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs: []sitemapURL{{Loc: s.baseURL + "/"}}}
	if shop := s.shop(); shop.ShopPhone != "" || shop.Requisites != "" {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + "/contacts"})
	}
	if s.shop().Terms != "" {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + "/info"})
	}
	// Category pages are the landing pages of the shop: a crawler that never
	// sees them in the map indexes cards and nothing to hold them together.
	if nodes, err := s.db.VisibleCategories(); err == nil && len(nodes) > 0 {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + "/c"})
		for _, n := range nodes {
			set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + categoryURL(n.Path),
				LastMod: n.LastMod})
		}
	}
	for _, p := range products {
		set.URLs = append(set.URLs, sitemapURL{
			Loc: s.baseURL + "/p/" + p.Slug, LastMod: p.UpdatedAt.Format("2006-01-02")})
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(set)
}

// kCleanParams are the query parameters that reorder or filter a page without
// changing which page it is. A canonical already points every sorted variant at
// the plain address, and Google honours it - but a canonical is a hint given
// after the fetch, so the crawler still spends a request on each permutation.
// On a live shop Yandex reported the waste directly: with 24 000 products and
// four sort orders crossed with an in-stock filter, the sorted copies of one
// section outnumber its real pages.
//
// page is deliberately absent. Page two holds different products, and telling a
// crawler to fold it into page one would hide most of the catalogue.
const kCleanParams = "sort&desc&instock"

func (s *Storefront) Robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "User-agent: *\nDisallow: /admin\nDisallow: /api\nDisallow: /cart\nClean-param: %s\n\nSitemap: %s/sitemap.xml\n",
		kCleanParams, s.baseURL)
}

// LlmsTxt is the shop's map for AI assistants - the answer engines already
// send buyers, measured: a visitor arrives with utm_source=chatgpt.com. A crawler pieces the shop together from twenty thousand pages;
// an assistant asked "where do I buy X" wants the shape of the shop in one
// read: what is sold, in which sections, how an order works. Russian on
// purpose - the storefront speaks the language of its products.
func (s *Storefront) LlmsTxt(w http.ResponseWriter, r *http.Request) {
	shop := s.shop()
	total, err := s.db.CountVisibleProducts(database.CatalogFilter{})
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "# %s\n\n", shop.ShopName)
	fmt.Fprintf(w, "> Интернет-магазин: %d товаров в наличии. Оплата при получении, без предоплаты; заказ подтверждается звонком. Цены в %s.\n\n",
		total, shop.Currency)
	fmt.Fprintf(w, "Заказ оформляется на сайте без регистрации: корзина → имя и телефон или почта.\n")
	if shop.ShopPhone != "" {
		fmt.Fprintf(w, "Телефон магазина: %s\n", shop.ShopPhone)
	}
	fmt.Fprintf(w, "\n## Каталог\n\n")
	if nodes, err := s.db.VisibleCategories(); err == nil {
		for _, c := range children(nodes, "") {
			fmt.Fprintf(w, "- [%s](%s%s): %d товаров\n", c.Name, s.baseURL, c.URL, c.Count)
		}
	}
	fmt.Fprintf(w, "\n## Страницы\n\n")
	fmt.Fprintf(w, "- [Все категории](%s/c)\n", s.baseURL)
	if shop.Terms != "" {
		fmt.Fprintf(w, "- [Доставка и оплата](%s/info)\n", s.baseURL)
	}
	if shop.ShopPhone != "" || shop.Requisites != "" {
		fmt.Fprintf(w, "- [Контакты](%s/contacts)\n", s.baseURL)
	}
	fmt.Fprintf(w, "- [Карта сайта](%s/sitemap.xml): каждый товар с датой обновления\n", s.baseURL)
	fmt.Fprintf(w, "\nСтраница товара (%s/p/<slug>) отдаётся сервером без скриптов и несёт разметку schema.org/Product с ценой и наличием.\n", s.baseURL)
}
