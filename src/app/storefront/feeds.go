package storefront

import (
	"encoding/xml"
	"net/http"
	"strconv"
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
	Vendor      string   `xml:"vendor,omitempty"`
	VendorCode  string   `xml:"vendorCode,omitempty"`
	URL         string   `xml:"url"`
	Price       string   `xml:"price"`
	CurrencyID  string   `xml:"currencyId"`
	CategoryID  int      `xml:"categoryId"`
	Pictures    []string `xml:"picture"`
	Description string   `xml:"description,omitempty"`
	// Not part of the YML standard, which carries availability as a flag and no
	// quantity at all. Yandex ignores elements it does not know, and our own
	// importer reads this one - without it a shop copied from another instance
	// arrives with one made-up stock level for the whole catalogue.
	Count int `xml:"count"`
	// A feed without a parcel size is an advertising feed: Direct never asks
	// how big the box is, a marketplace refuses the card without it. Kilograms
	// and centimetres are what YML states; we store grams and millimetres.
	Weight     string     `xml:"weight,omitempty"`
	Dimensions string     `xml:"dimensions,omitempty"`
	Params     []ymlParam `xml:"param,omitempty"`
}

type ymlParam struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
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
	// Merchant Center matches an offer against the catalogue by brand; without
	// one a listing competes in fewer places and some categories refuse it.
	Brand string `xml:"g:brand,omitempty"`
	// Merchant Center quotes delivery from it; without a weight it quotes from
	// the account default, which is one number for a catalogue of every size.
	ShippingWeight string `xml:"g:shipping_weight,omitempty"`
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
// price sign uses - s.shop() can return settings with an empty currency.
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

func feedWeightKG(grams *int64) string {
	if grams == nil || *grams <= 0 {
		return ""
	}
	return strconv.FormatFloat(float64(*grams)/1000, 'f', -1, 64)
}

// All three or nothing: a box stated as two sides is not a box, and a partial
// value reads to the receiving side as a wrong one rather than a missing one.
func feedDimensionsCM(l, w, h *int64) string {
	if l == nil || w == nil || h == nil || *l <= 0 || *w <= 0 || *h <= 0 {
		return ""
	}
	cm := func(mm int64) string {
		return strconv.FormatFloat(float64(mm)/10, 'f', -1, 64)
	}
	return cm(*l) + "/" + cm(*w) + "/" + cm(*h)
}

// The owner's choice of what a buyer sees holds here too: a marketplace shows
// these in the card, so the same checkboxes decide it.
func feedParams(params []database.Param, hidden map[string]bool) []ymlParam {
	out := make([]ymlParam, 0, len(params))
	for _, p := range params {
		if p.Name == "" || hidden[p.Name] {
			continue
		}
		if v := propStr(p.Value); v != "" {
			out = append(out, ymlParam{Name: p.Name, Value: v})
		}
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
	// a shop of one level, and our own import - a feed of ours is a valid import
	// source - rebuilds the path as a single name with the separator rewritten,
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
	hidden := s.hiddenParams()
	for _, p := range products {
		catID := catIDs[p.Category]
		if catID == 0 {
			catID = catchAll
		}
		catalog.Shop.Offers = append(catalog.Shop.Offers, ymlOffer{
			ID: p.ID, Available: p.Stock > 0, Name: p.Title,
			Vendor: p.Brand, VendorCode: p.SKU,
			URL: s.baseURL + "/p/" + p.Slug, Price: priceStr(p.Price),
			CurrencyID: currency, CategoryID: catID,
			Pictures: s.feedPictures(p.ID, images), Description: p.Description,
			Count:      p.Stock,
			Weight:     feedWeightKG(p.WeightG),
			Dimensions: feedDimensionsCM(p.LengthMM, p.WidthMM, p.HeightMM),
			Params:     feedParams(p.Params, hidden),
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
			Condition: "new", ProductType: p.Category, Brand: p.Brand,
		}
		if kg := feedWeightKG(p.WeightG); kg != "" {
			item.ShippingWeight = kg + " kg"
		}
		if pics := s.feedPictures(p.ID, images); len(pics) > 0 {
			item.ImageLink = pics[0]
			item.MoreImages = pics[1:]
		}
		feed.Channel.Items = append(feed.Channel.Items, item)
	}
	writeFeed(w, feed)
}
