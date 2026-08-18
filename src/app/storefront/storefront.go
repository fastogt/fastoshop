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
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/style.css
var styleCSS string

type Storefront struct {
	db      *database.Database
	baseURL string
	uploads string
	index   *template.Template
	product *template.Template
	cart    *template.Template
	info    *template.Template
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
		categoryTpl:   template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/category.html")),
		categoriesTpl: template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/categories.html")),
	}
}

// HeadAsGet lets HEAD reach handlers registered as GET. chi answers 405
// otherwise, and HEAD is what monitoring, link checkers and some crawlers use to
// see whether a page is alive — a storefront that refuses it looks broken from
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
	r.Get("/c", s.Categories)
	r.Get("/c/*", s.Category)
	r.Post("/cart/add", s.CartAdd)
	r.Post("/cart/update", s.CartUpdate)
	r.Post("/cart/order", s.CartOrder)
	r.Get("/sitemap.xml", s.Sitemap)
	r.Get("/yml.xml", s.YML)
	r.Get("/gmc.xml", s.GMC)
	r.Get("/robots.txt", s.Robots)
	r.Get("/favicon.svg", s.Favicon)
	r.Get("/nophoto.svg", s.NoPhoto)
	return r
}

func priceStr(minor int64) string {
	return fmt.Sprintf("%.2f", float64(minor)/100)
}

// kCatalogPageSize — ~60 cards ≈ 100–200 KB of HTML: the page stays light for
// mobile and for the crawler, and a 20,000-item catalogue is no longer rendered
// in one piece.
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
// page. A photo with no small copy — uploaded before thumbnails existed, or
// still a link to the supplier — is served as it is: a hole in the grid would
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

// absImageURL — for og:image and JSON-LD, where an absolute address is required.
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

// imageVM — ready-made image addresses: relative for <img>, absolute for
// og:image and JSON-LD.
type imageVM struct {
	URL    string
	AbsURL string
}

// pageVM is the template data. One struct for both pages: a typed model
// instead of map[string]any (project rule: no any in signatures).
type pageVM struct {
	Shop            *database.Settings
	BaseURL         string
	CSS             template.CSS
	Products        []cardVM
	P               *database.Product
	Images          []imageVM
	PriceStr        string
	MetaDescription string
	Canonical       string
	NoIndex         bool
	Query           string
	FoundStr        string
	Category        string
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
	// NoContact — the buyer left neither a phone nor an email, so the order was
	// not created: an order nobody can be reached about is not an order.
	NoContact bool
	SoldOut   string
}

// categoryURL is the address of a node of the tree: every segment of the path
// transliterated, joined back with slashes. A category is a page of its own —
// the landing page for "купить X" — not a query parameter on the catalogue.
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
// the same goods in another order — one page for a search engine, several for a
// buyer — so every variant points at the plain one.
func canonicalURL(category string, page int) string {
	return catalogURL(database.CatalogFilter{Category: category}, page)
}

// foundStr — «нашёлся 1 товар» / «2 товара» / «5 товаров». Pluralization lives
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
	// The links are in the wild — in the index, in bookmarks — so they move
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
	// somebody's bookmarks, so it moves rather than dies — otherwise tidying the
	// tree would throw away everything the page had earned.
	if to, ok, err := s.db.CategoryRedirectBySlug(want); err == nil && ok {
		http.Redirect(w, r, "/c/"+to, http.StatusMovedPermanently)
		return
	}
	// An unknown or emptied category is a 404, not an empty listing: a soft 404
	// keeps the address in the index and spends the crawl budget on nothing.
	http.NotFound(w, r)
}

// Categories is the index of the tree — one page linking to every node. Without
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

// children returns the nodes one level below path — the links that let a buyer
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
// — page 7 of another ordering shows goods the buyer never asked for.
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
	// browsed page by page, and the storefront has no JavaScript to search with —
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
	// here — the home page needs the top level, a category needs its own level.
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
	// out of the index. noindex,follow rather than robots.txt — a page blocked
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
	data := pageVM{Shop: s.shop(), BaseURL: s.baseURL,
		CSS: template.CSS(styleCSS), P: p, Images: imgs,
		PriceStr: priceStr(p.Price), MetaDescription: desc,
		CartCount: cartCount(r)}
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

func (s *Storefront) Robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "User-agent: *\nDisallow: /admin\nDisallow: /api\nDisallow: /cart\n\nSitemap: %s/sitemap.xml\n", s.baseURL)
}
