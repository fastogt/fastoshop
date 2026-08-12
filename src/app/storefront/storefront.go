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
	// OnStockChange будит синк площадок после списания остатка заказом. Поле, а
	// не аргумент конструктора: связь односторонняя и необязательная.
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
		index:   template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/index.html")),
		product: template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/product.html")),
		cart:    template.Must(template.Must(base.Clone()).ParseFS(templatesFS, "templates/cart.html")),
	}
}

// headAsGet lets HEAD reach handlers registered as GET. chi answers 405
// otherwise, and HEAD is what monitoring, link checkers and some crawlers use to
// see whether a page is alive — a storefront that refuses it looks broken from
// the outside. The body is dropped by net/http itself: the server still sees the
// original HEAD request and suppresses it.
func headAsGet(next http.Handler) http.Handler {
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
	r.Use(headAsGet)
	r.Get("/", s.Index)
	r.Get("/p/{slug}", s.Product)
	r.Get("/cart", s.Cart)
	r.Post("/cart/add", s.CartAdd)
	r.Post("/cart/update", s.CartUpdate)
	r.Post("/cart/order", s.CartOrder)
	r.Get("/sitemap.xml", s.Sitemap)
	r.Get("/yml.xml", s.YML)
	r.Get("/gmc.xml", s.GMC)
	r.Get("/robots.txt", s.Robots)
	r.Get("/favicon.svg", s.Favicon)
	return r
}

func priceStr(minor int64) string {
	return fmt.Sprintf("%.2f", float64(minor)/100)
}

// kCatalogPageSize — ~60 карточек ≈ 100–200 КБ HTML: страница остаётся лёгкой
// для мобильного и для краулера, каталог на 20 000 позиций больше не рендерится
// целиком.
// A search box is not a text field: anything longer than this is a bot or a
// paste, and it has no business reaching the database or the page title.
const kMaxQueryRunes = 100

const kCatalogPageSize = 60

// product_images.path хранит либо имя локального файла (загрузка из админки),
// либо абсолютный URL источника (импорт больше не качает фото).
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

// absImageURL — для og:image и JSON-LD, где нужен абсолютный адрес.
func (s *Storefront) absImageURL(path string) string {
	if isRemoteImage(path) {
		return path
	}
	return s.baseURL + "/uploads/" + path
}

// orderItemJSON — снапшот позиции в orders.items_json. Тот же формат читает
// CSV-экспорт (handler/orders.go, orderItem).
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

// imageVM — готовые адреса картинки: относительный для <img>, абсолютный для
// og:image и JSON-LD.
type imageVM struct {
	URL    string
	AbsURL string
}

// pageVM — данные шаблона. Одна структура на обе страницы: типизированная
// модель вместо map[string]any (правило проекта: никаких any в сигнатурах).
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
	CategoryURL     string
	Page            int
	Pages           int
	PrevURL         string
	NextURL         string
	Ordered         bool
	Cart            []cartRowVM
	TotalStr        string
	CartCount       int
	Dropped         bool
	SoldOut         string
}

// catalogURL — адрес страницы каталога с сохранённой категорией. Первая
// страница всегда без ?page=, чтобы у одного набора товаров был один URL.
func catalogURL(category string, page int, query string) string {
	q := url.Values{}
	if category != "" {
		q.Set("category", category)
	}
	if query != "" {
		q.Set("q", query)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if len(q) == 0 {
		return "/"
	}
	return "/?" + q.Encode()
}

// foundStr — «нашёлся 1 товар» / «2 товара» / «5 товаров». Плюрализация живёт
// здесь, а не в шаблоне: витрина рендерится на языке товаров, и правило у
// русского числительного одно на весь магазин.
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
	category := r.URL.Query().Get("category")
	// The buyer's search. A shop the size of a marketplace catalogue cannot be
	// browsed page by page, and the storefront has no JavaScript to search with —
	// so it is a plain GET form and a server-rendered result.
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(query)) > kMaxQueryRunes {
		query = string([]rune(query)[:kMaxQueryRunes])
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		// Мусор в ?page= — 404, а не пустая выдача: мягкие 404 отравляют индекс.
		if err != nil || n < 1 {
			http.NotFound(w, r)
			return
		}
		if n == 1 {
			http.Redirect(w, r, catalogURL(category, 1, query), http.StatusMovedPermanently)
			return
		}
		page = n
	}
	total, err := s.db.CountVisibleProducts(category, query)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	pages := max((total+kCatalogPageSize-1)/kCatalogPageSize, 1)
	if page > pages {
		http.NotFound(w, r)
		return
	}
	products, err := s.db.ListVisibleProductsPage(category, query,
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
		Canonical: s.baseURL + catalogURL(category, page, ""), Page: page, Pages: pages,
		Query: query}
	// A result page is thin, endless and duplicates the catalogue: search belongs
	// out of the index. noindex,follow rather than robots.txt — a page blocked
	// from crawling is a page whose noindex is never read.
	if query != "" {
		data.NoIndex = true
		data.Canonical = ""
		// На пустой выдаче счётчик молчит: ниже уже стоит сообщение с самим
		// запросом и ссылкой в каталог, две одинаковые фразы подряд — шум.
		if total > 0 {
			data.FoundStr = foundStr(total)
		}
	}
	if page > 1 {
		data.PrevURL = catalogURL(category, page-1, query)
	}
	if page < pages {
		data.NextURL = catalogURL(category, page+1, query)
	}
	if err := s.index.ExecuteTemplate(w, "base", data); err != nil {
		log.Errorf("render index: %v", err)
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
		CartCount: cartCount(r), CategoryURL: catalogURL(p.Category, 1, "")}
	if err := s.product.ExecuteTemplate(w, "base", data); err != nil {
		log.Errorf("render product: %v", err)
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
	products, err := s.db.ListVisibleProductsPage("", "", -1, 0)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	set := sitemapSet{NS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs: []sitemapURL{{Loc: s.baseURL + "/"}}}
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
