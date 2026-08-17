package storefront

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/fastogt/fastoshop/app/database"
)

// Product feeds are the storefront's second traffic channel: the same catalogue
// the crawler sees on the pages, packaged for Yandex Tovary / Direct (YML) and
// Google Merchant Center (RSS 2.0). The owner pastes the URL into the
// provider's cabinet once; the provider re-fetches on its own schedule.
// ponytail: rendered per request like the sitemap; pre-generate when a real
// crawler makes that hurt.

// Both providers cap the pictures per offer at around ten; sending more only
// gets the tail ignored.
const kMaxFeedPictures = 10

// kFeedCatchAllCategory names the YML category for products without one:
// <categoryId> is mandatory per offer. Product language, like the storefront.
const kFeedCatchAllCategory = "Товары"

type ymlCurrency struct {
	ID   string `xml:"id,attr"`
	Rate string `xml:"rate,attr"`
}

type ymlCategory struct {
	ID       int    `xml:"id,attr"`
	ParentID int    `xml:"parentId,attr,omitempty"`
	Name     string `xml:",chardata"`
}

type ymlOffer struct {
	ID          int64    `xml:"id,attr"`
	Available   bool     `xml:"available,attr"`
	Name        string   `xml:"name"`
	VendorCode  string   `xml:"vendorCode,omitempty"`
	URL         string   `xml:"url"`
	Price       string   `xml:"price"`
	CurrencyID  string   `xml:"currencyId"`
	CategoryID  int      `xml:"categoryId"`
	Pictures    []string `xml:"picture"`
	Description string   `xml:"description,omitempty"`
	// Not part of the YML standard, which carries availability as a flag and no
	// quantity at all. Yandex ignores elements it does not know, and our own
	// importer reads this one — without it a shop copied from another instance
	// arrives with one made-up stock level for the whole catalogue.
	Count int `xml:"count"`
}

type ymlShop struct {
	Name       string        `xml:"name"`
	Company    string        `xml:"company"`
	URL        string        `xml:"url"`
	Currencies []ymlCurrency `xml:"currencies>currency"`
	Categories []ymlCategory `xml:"categories>category"`
	Offers     []ymlOffer    `xml:"offers>offer"`
}

type ymlCatalog struct {
	XMLName xml.Name `xml:"yml_catalog"`
	Date    string   `xml:"date,attr"`
	Shop    ymlShop  `xml:"shop"`
}

type gmcItem struct {
	ID           int64    `xml:"g:id"`
	Title        string   `xml:"title"`
	Description  string   `xml:"description,omitempty"`
	Link         string   `xml:"link"`
	ImageLink    string   `xml:"g:image_link,omitempty"`
	MoreImages   []string `xml:"g:additional_image_link,omitempty"`
	Price        string   `xml:"g:price"`
	Availability string   `xml:"g:availability"`
	Condition    string   `xml:"g:condition"`
	ProductType  string   `xml:"g:product_type,omitempty"`
}

type gmcChannel struct {
	Title string    `xml:"title"`
	Link  string    `xml:"link"`
	Items []gmcItem `xml:"item"`
}

type gmcFeed struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	NSG     string     `xml:"xmlns:g,attr"`
	Channel gmcChannel `xml:"channel"`
}

// feedData is the shared fetch for both feeds: the visible catalogue, all
// photos in one query, and the shop currency with the same RUB fallback the
// price sign uses — s.shop() can return settings with an empty currency.
func (s *Storefront) feedData() ([]database.Product, map[int64][]string, string, error) {
	products, err := s.db.ListVisibleProductsPage(database.CatalogFilter{}, -1, 0)
	if err != nil {
		return nil, nil, "", err
	}
	images, err := s.db.AllImages()
	if err != nil {
		return nil, nil, "", err
	}
	currency := s.shop().Currency
	if !database.IsValidShopCurrency(currency) {
		currency = database.ShopCurrencyRUB
	}
	return products, images, currency, nil
}

func (s *Storefront) feedPictures(productID int64, images map[int64][]string) []string {
	paths := images[productID]
	if len(paths) > kMaxFeedPictures {
		paths = paths[:kMaxFeedPictures]
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, s.absImageURL(p))
	}
	return out
}

func writeFeed(w http.ResponseWriter, feed any) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(feed)
}

func (s *Storefront) YML(w http.ResponseWriter, r *http.Request) {
	products, images, currency, err := s.feedData()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	cats, err := s.db.Categories()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	catIDs := make(map[string]int, len(cats))
	categories := make([]ymlCategory, 0, len(cats)+1)
	// A segment per element, tied by parentId. Naming one element after the whole
	// path is what a flat list forces, and it costs the tree twice: Yandex reads
	// a shop of one level, and our own import — a feed of ours is a valid import
	// source — rebuilds the path as a single name with the separator rewritten,
	// so a copied catalogue lands beside the tree instead of inside it.
	var ensure func(path string) int
	ensure = func(path string) int {
		if id, ok := catIDs[path]; ok {
			return id
		}
		parent, name := 0, path
		if i := strings.LastIndex(path, database.CategorySep); i >= 0 {
			parent = ensure(path[:i])
			name = path[i+len(database.CategorySep):]
		}
		id := len(catIDs) + 1
		catIDs[path] = id
		categories = append(categories, ymlCategory{ID: id, ParentID: parent, Name: name})
		return id
	}
	for _, c := range cats {
		ensure(c)
	}
	catchAll := 0
	for _, p := range products {
		if p.Category == "" {
			catchAll = len(catIDs) + 1
			categories = append(categories, ymlCategory{ID: catchAll, Name: kFeedCatchAllCategory})
			break
		}
	}
	shop := s.shop()
	catalog := ymlCatalog{
		Date: time.Now().Format("2006-01-02 15:04"),
		Shop: ymlShop{
			Name: shop.ShopName, Company: shop.ShopName, URL: s.baseURL + "/",
			Currencies: []ymlCurrency{{ID: currency, Rate: "1"}},
			Categories: categories,
		},
	}
	for _, p := range products {
		catID := catIDs[p.Category]
		if catID == 0 {
			catID = catchAll
		}
		catalog.Shop.Offers = append(catalog.Shop.Offers, ymlOffer{
			ID: p.ID, Available: p.Stock > 0, Name: p.Title, VendorCode: p.SKU,
			URL: s.baseURL + "/p/" + p.Slug, Price: priceStr(p.Price),
			CurrencyID: currency, CategoryID: catID,
			Pictures: s.feedPictures(p.ID, images), Description: p.Description,
			Count: p.Stock,
		})
	}
	writeFeed(w, catalog)
}

func (s *Storefront) GMC(w http.ResponseWriter, r *http.Request) {
	products, images, currency, err := s.feedData()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	feed := gmcFeed{
		Version: "2.0", NSG: "http://base.google.com/ns/1.0",
		Channel: gmcChannel{Title: s.shop().ShopName, Link: s.baseURL + "/"},
	}
	for _, p := range products {
		availability := "in_stock"
		if p.Stock <= 0 {
			availability = "out_of_stock"
		}
		item := gmcItem{
			ID: p.ID, Title: p.Title, Description: p.Description,
			Link:  s.baseURL + "/p/" + p.Slug,
			Price: priceStr(p.Price) + " " + currency, Availability: availability,
			Condition: "new", ProductType: p.Category,
		}
		if pics := s.feedPictures(p.ID, images); len(pics) > 0 {
			item.ImageLink = pics[0]
			item.MoreImages = pics[1:]
		}
		feed.Channel.Items = append(feed.Channel.Items, item)
	}
	writeFeed(w, feed)
}
