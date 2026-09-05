package storefront

import (
	"embed"
	"encoding/xml"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	log "github.com/sirupsen/logrus"

	"time"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/media"
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
	privacy  *template.Template
	contacts *template.Template
	// Suffixed: a field may not share a name with the Category/Categories methods.
	categoryTpl   *template.Template
	categoriesTpl *template.Template
	// Optional one-way link: wakes the marketplace sync after an order deducts stock.
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
		privacy:       template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/privacy.html")),
		contacts:      template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/contacts.html")),
		categoryTpl:   template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/category.html")),
		categoriesTpl: template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/categories.html")),
	}
}

// chi answers 405 to HEAD; must sit on the outermost router or /admin* preempts it.
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
	r.Get("/privacy", s.Privacy)
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

// Anything longer than this is a bot or a paste, not a search.
const kMaxQueryRunes = 100

const kCatalogPageSize = 60

// product_images.path holds either a local file name or an absolute source URL.
func isRemoteImage(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

func imageURL(path string) string {
	if isRemoteImage(path) {
		return path
	}
	return "/uploads/" + path
}

// ponytail: one stat per card, sixty per page - microseconds on a local disk.
// An in-memory cache is for the day the catalogue moves to network storage.
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

// The same format is read by the CSV export (handler/orders.go, orderItem).
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

type categoryVM struct {
	Name  string
	URL   string
	Count int
}

// Filters are links, not JavaScript: the state lives in the address and is shareable.
type filterVM struct {
	Name   string
	URL    string
	Active bool
}

// BreadcrumbList numbers items from one, and html/template has no arithmetic.
type crumbVM struct {
	Name     string
	URL      string
	Position int
}

// Relative for <img>, absolute for og:image and JSON-LD.
type imageVM struct {
	URL    string
	AbsURL string
}

// A typed model instead of map[string]any (project rule: no any in signatures).
type pageVM struct {
	Shop     *database.Settings
	BaseURL  string
	CSS      template.CSS
	Products []cardVM
	P        *database.Product
	Images   []imageVM
	PriceStr string
	// Without a date the offer is treated as stale and dropped from the snippet.
	PriceValidUntil string
	// Search engines ask for it; the card's last change is the closest date we have.
	PriceValidFrom string
	// The title cut to what structured data accepts; stays a prefix of the heading.
	SchemaName      string
	MetaDescription string
	Specs           specVM
	// Stored units as plain numbers: a template cannot deref a pointer, 0 means unset.
	WeightG    int64
	LengthMM   int64
	WidthMM    int64
	HeightMM   int64
	OrderLinks []orderLinkVM
	Canonical  string
	NoIndex    bool
	Query      string
	FoundStr   string
	Category   string
	// The owner's own words: its first sentences become the page description.
	CategoryText string
	Crumbs       []crumbVM
	// The position the product itself takes in BreadcrumbList, after its categories.
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
	// Neither a phone nor an email was left, so no order was created.
	NoContact bool
	SoldOut   string
	// Typed before the order was refused, rendered back so nothing has to be retyped.
	FormName    string
	FormComment string
}

// A category is a page of its own, not a query parameter on the catalogue.
func categoryURL(path string) string {
	if path == "" {
		return "/"
	}
	return "/c/" + database.SlugPath(path)
}

// The first page never carries ?page=, so one set of products has exactly one URL.
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

// Sorted and filtered variants are one page for a search engine: all point here.
func canonicalURL(category string, page int) string {
	return catalogURL(database.CatalogFilter{Category: category}, page)
}

// Pluralization here, not in the template: html/template cannot pick a numeral form.
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
	// ?category= links are in the index and in bookmarks: move them, do not drop them.
	if old := r.URL.Query().Get("category"); old != "" {
		http.Redirect(w, r, categoryURL(old), http.StatusMovedPermanently)
		return
	}
	s.listing(w, r, "", s.index)
}

// A node shows its descendants too, so a parent is never an empty landing page.
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
	// A renamed category keeps its old address in the index: move it, do not drop it.
	if to, ok, err := s.db.CategoryRedirectBySlug(want); err == nil && ok {
		http.Redirect(w, r, "/c/"+to, http.StatusMovedPermanently)
		return
	}
	// A 404, not an empty listing: a soft 404 keeps the address and burns crawl budget.
	http.NotFound(w, r)
}

// Without this index a crawler finds only the top level of the tree.
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

// Clicking a filter drops the page number: page 7 of another ordering is unrelated.
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

// Whole sentences up to the length a snippet shows, so the tail is never broken.
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
	images, _ := s.db.ImagesFor(productIDs(products))
	cards := make([]cardVM, 0, len(products))
	for _, p := range products {
		vm := cardVM{Product: p, PriceStr: priceStr(p.Price)}
		if imgs := images[p.ID]; len(imgs) > 0 {
			vm.ImageURL = s.thumbURL(imgs[0].Path)
		}
		cards = append(cards, vm)
	}
	data := pageVM{Shop: s.shop(), BaseURL: s.baseURL,
		CSS: template.CSS(styleCSS), Products: cards, CartCount: cartCount(r),
		Canonical: s.baseURL + canonicalURL(category, page), Page: page, Pages: pages,
		Query: query, FoundStr: foundStr(total)}
	data.Filters = filterLinks(filter)
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
	// noindex,follow rather than robots.txt: a page blocked from crawling never reads it.
	if query != "" {
		data.NoIndex = true
		data.Canonical = ""
		// The empty-result message below already names the query; two in a row is noise.
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

type specVM struct {
	Weight string
	Size   string
	// The source's own characteristics, in its order; weight and size are not among them.
	Props []specProp
}

type specProp struct {
	Name  string
	Value string
}

func (s specVM) Empty() bool {
	return s.Weight == "" && s.Size == "" && len(s.Props) == 0
}

// We store grams and millimetres; a buyer reads kilograms and centimetres.
func specs(p *database.Product, hidden map[string]bool) specVM {
	var out specVM
	if p.WeightG != nil {
		out.Weight = weightStr(*p.WeightG)
	}
	// All three or none: one side on its own does not tell a buyer whether it fits.
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

// On failure show everything: a page with no characteristics is the worse outcome.
func (s *Storefront) hiddenParams() map[string]bool {
	h, err := s.db.HiddenParams()
	if err != nil {
		return nil
	}
	return h
}

// A marketplace states a single value as a list of one; unreadable values are dropped.
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
		return strconv.FormatFloat(math.Round(float64(g)/10)/100, 'f', -1, 64) + " кг"
	}
	return fmt.Sprintf("%d г", g)
}

func cmStr(mm int64) string {
	return strconv.FormatFloat(float64(mm)/10, 'f', -1, 64)
}

// The ceiling search engines put on a product name in structured data.
const kMaxSchemaName = 150

// Cut on a word boundary; a title with no space inside the limit is cut as it stands.
func clipName(s string) string {
	r := []rune(s)
	if len(r) <= kMaxSchemaName {
		return s
	}
	cut := string(r[:kMaxSchemaName])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:-")
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
	shop := s.shop()
	data := pageVM{Shop: shop, BaseURL: s.baseURL,
		CSS: template.CSS(styleCSS), P: p, Images: imgs,
		PriceStr: priceStr(p.Price), PriceValidUntil: time.Now().AddDate(0, 1, 0).Format(time.DateOnly),
		PriceValidFrom:  p.UpdatedAt.Format(time.DateOnly),
		SchemaName:      clipName(p.Title),
		OrderLinks:      orderLinks(shop, p, s.baseURL+"/p/"+p.Slug),
		MetaDescription: metaFrom(p.Description),
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

// Shopping engines reject a shop with no delivery and payment terms; 404 while unwritten.
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

// ponytail: the privacy text is hardcoded, one wording for every shop.
// A settings field the day a shop needs its own terms, retention or processor.
func (s *Storefront) Privacy(w http.ResponseWriter, r *http.Request) {
	data := pageVM{Shop: s.shop(), BaseURL: s.baseURL, CSS: template.CSS(styleCSS),
		CartCount: cartCount(r), Canonical: s.baseURL + "/privacy"}
	if err := s.privacy.ExecuteTemplate(w, "base", data); err != nil {
		log.Errorf("render privacy: %v", err)
	}
}

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
	shop := s.shop()
	if shop.ShopPhone != "" || shop.Requisites != "" {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + "/contacts"})
	}
	if shop.Terms != "" {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + "/info"})
	}
	set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + "/privacy"})
	// Category pages are the shop's landing pages; out of the map they go unindexed.
	if nodes, err := s.db.VisibleCategories(); err == nil && len(nodes) > 0 {
		set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + "/c"})
		for _, n := range nodes {
			set.URLs = append(set.URLs, sitemapURL{Loc: s.baseURL + categoryURL(n.Path),
				LastMod: n.LastMod})
		}
	}
	for _, p := range products {
		set.URLs = append(set.URLs, sitemapURL{
			Loc: s.baseURL + "/p/" + p.Slug, LastMod: p.UpdatedAt.Format(time.DateOnly)})
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(set)
}

// Params that reorder a page without changing it; page is excluded, it holds other goods.
const kCleanParams = "sort&desc&instock"

func (s *Storefront) Robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "User-agent: *\nDisallow: /admin\nDisallow: /api\nDisallow: /cart\nClean-param: %s\n\nSitemap: %s/sitemap.xml\n",
		kCleanParams, s.baseURL)
}

// The shop's shape in one read for AI assistants; Russian, like the storefront.
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
