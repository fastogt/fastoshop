package importer

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// YML is the Yandex.Market export format (yml_catalog): the standard export
// of Bitrix, InSales, Tilda. A keyless source: the seller gives a link to the XML.
type YML struct {
	URL string
	// Data is the feed itself, when the owner uploads the file instead of
	// hosting it. Our own tools write one too - a generator that has the
	// catalogue in hand should hand over the format the shop already reads,
	// not invent a flat one beside it.
	Data []byte
	// DefaultStock - the standard carries no quantity, only an availability flag,
	// so the seller sets the stock as one number for the whole catalogue. Used
	// for offers that state no count of their own.
	DefaultStock int
	// MaxBytes - body size ceiling; 0 means kMaxFeedBytes.
	MaxBytes int64

	errors     int
	currency   string
	categories map[string]ymlCategory
}

// ponytail: 100 MB is plenty (a live export of 24k products is 33 MB);
// the ceiling exists so a bad link cannot drag gigabytes into memory.
const kMaxFeedBytes = 100 << 20

func (y *YML) Name() string { return "yml" }

// IsYML recognises an uploaded feed by its opening bytes, the way IsXLSX does.
// A declaration is optional in XML, so the root element is what settles it.
func IsYML(raw []byte) bool {
	head := raw
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.Contains(head, []byte("<yml_catalog"))
}

// Currency is only known after Fetch: the feed has to be parsed to see it.
func (y *YML) Currency() string { return y.currency }

// feedCurrency maps a YML code onto ours. RUR is the rouble spelling from the
// early versions of the format and BYR the Belarusian rouble from before the
// 2016 redenomination; both still show up in live feeds.
func feedCurrency(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "RUB", "RUR":
		return database.ShopCurrencyRUB
	case "BYN", "BYR":
		return database.ShopCurrencyBYN
	case "PLN":
		return database.ShopCurrencyPLN
	case "KZT":
		return database.ShopCurrencyKZT
	}
	return ""
}

// FetchErrors - cards rejected before hitting the DB (foreign currency, broken price).
func (y *YML) FetchErrors() int { return y.errors }

type ymlOffer struct {
	ID          string   `xml:"id,attr"`
	Available   string   `xml:"available,attr"`
	Name        string   `xml:"name"`
	VendorCode  string   `xml:"vendorCode"`
	Vendor      string   `xml:"vendor"`
	Description string   `xml:"description"`
	Price       string   `xml:"price"`
	CurrencyID  string   `xml:"currencyId"`
	CategoryID  string   `xml:"categoryId"`
	Pictures    []string `xml:"picture"`
	// Outside the standard: only a shop of ours puts a quantity in a feed. An
	// empty element keeps the old behaviour for everyone else.
	Count      string     `xml:"count"`
	Params     []ymlParam `xml:"param"`
	Weight     string     `xml:"weight"`
	Dimensions string     `xml:"dimensions"`
}

// param is the standard's own place for characteristics - colour, size,
// material. Every exporter writes them: Bitrix, InSales, Tilda.
type ymlParam struct {
	Name  string `xml:"name,attr"`
	Unit  string `xml:"unit,attr"`
	Value string `xml:",chardata"`
}

type ymlCategory struct {
	ID       string `xml:"id,attr"`
	ParentID string `xml:"parentId,attr"`
	Name     string `xml:",chardata"`
}

// kMaxCategoryDepth stops a feed whose parentId points in a circle from walking
// forever. Ten levels is deeper than any live catalogue.
const kMaxCategoryDepth = 10

// path walks up the parents and returns the category as a path from the root.
// A missing id yields an empty string: a product with no category is normal,
// a crashed import is not.
func categoryPath(cats map[string]ymlCategory, id string) string {
	var segments []string
	for i := 0; i < kMaxCategoryDepth; i++ {
		c, ok := cats[id]
		if !ok {
			break
		}
		segments = append([]string{c.Name}, segments...)
		if c.ParentID == "" || c.ParentID == c.ID {
			break
		}
		id = c.ParentID
	}
	return database.CategoryPath(segments...)
}

// trimCommonRoot drops the segments every product shares. A feed's tree starts
// at the site's own root - "Главная / Каталог товаров / …" in a Bitrix export -
// and a level that holds the whole catalogue tells a buyer and a search engine
// nothing. The leaf always survives: a shop selling one category must keep it.
func trimCommonRoot(items []Item) {
	var common []string
	first := true
	for _, it := range items {
		if it.Category == "" {
			continue
		}
		segments := strings.Split(it.Category, database.CategorySep)
		if first {
			common, first = segments, false
			continue
		}
		n := 0
		for n < len(common) && n < len(segments) && common[n] == segments[n] {
			n++
		}
		common = common[:n]
		if len(common) == 0 {
			return
		}
	}
	for i, it := range items {
		if it.Category == "" {
			continue
		}
		segments := strings.Split(it.Category, database.CategorySep)
		if drop := min(len(common), len(segments)-1); drop > 0 {
			items[i].Category = strings.Join(segments[drop:], database.CategorySep)
		}
	}
}

// each downloads the feed and yields offers one at a time. Parsing is streamed:
// exports run to tens of megabytes, and xml.Unmarshal would hold both the
// document and the tree in memory.
func (y *YML) each(fn func(o *ymlOffer)) error {
	y.categories = map[string]ymlCategory{}
	limit := y.MaxBytes
	if limit <= 0 {
		limit = kMaxFeedBytes
	}
	var body io.Reader
	if len(y.Data) > 0 {
		body = bytes.NewReader(y.Data)
	} else {
		if !strings.HasPrefix(y.URL, "http://") && !strings.HasPrefix(y.URL, "https://") {
			return &i18n.KeyError{Key: i18n.KeyYMLBadURL}
		}
		resp, err := kHTTP.Get(y.URL)
		if err != nil {
			return fmt.Errorf("yml: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return &i18n.KeyError{Key: i18n.KeyYMLBadStatus, Args: []any{resp.StatusCode}}
		}
		body = resp.Body
	}
	// LimitedReader with one spare byte: when N runs out mid-document the
	// decoder fails, and N == 0 tells truncation apart from real bad XML.
	lr := &io.LimitedReader{R: body, N: limit + 1}
	dec := xml.NewDecoder(lr)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if lr.N <= 0 {
				return &i18n.KeyError{Key: i18n.KeyYMLTooBig, Args: []any{limit >> 20}}
			}
			log.Warnf("yml: parse: %v", err)
			return &i18n.KeyError{Key: i18n.KeyYMLBadXML}
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// <categories> comes before <offers> in the format, so by the time the
		// first offer arrives the tree is complete. Only the map is kept in
		// memory - hundreds of nodes against tens of megabytes of offers.
		if se.Name.Local == "category" {
			var c ymlCategory
			if err := dec.DecodeElement(&c, &se); err != nil {
				log.Warnf("yml: category: %v", err)
				continue
			}
			c.Name = strings.TrimSpace(c.Name)
			y.categories[c.ID] = c
			continue
		}
		if se.Name.Local != "offer" {
			continue
		}
		var o ymlOffer
		if err := dec.DecodeElement(&o, &se); err != nil {
			log.Warnf("yml: offer: %v", err)
			return &i18n.KeyError{Key: i18n.KeyYMLBadXML}
		}
		if strings.EqualFold(strings.TrimSpace(o.Available), "false") {
			continue
		}
		fn(&o)
	}
	return nil
}

// stock prefers the feed's own number over the one the seller typed: a feed that
// states a quantity per offer - ours does - carries the truth, and spreading one
// number over the whole catalogue oversells everything that has less.
func (y *YML) stock(o *ymlOffer) int {
	if n, err := strconv.Atoi(strings.TrimSpace(o.Count)); err == nil && n >= 0 {
		return n
	}
	return y.DefaultStock
}

// offerParams turns the feed's <param> list into ours. A nameless or empty param
// is dropped rather than stored as a blank row on the card.
//
// The unit joins the caption - "Вес, кг" reads as well as "Вес: 1.5 кг" did and
// leaves 1.5 a number a filter can compare. The unit is also what decides to
// read the value as one: the feed stating a measure is the feed's own word that
// the field is numeric, whereas the shape of the digits is a guess, and it is
// the guess that turns an article number "007" into 7.
func offerParams(ps []ymlParam) []database.Param {
	var out []database.Param
	for _, p := range ps {
		name := strings.TrimSpace(p.Name)
		value := strings.TrimSpace(p.Value)
		if name == "" || value == "" {
			continue
		}
		if unit := strings.TrimSpace(p.Unit); unit != "" {
			name += ", " + unit
			// A decimal comma is how half the feeds in this country write 1,5.
			if n, err := strconv.ParseFloat(strings.Replace(value, ",", ".", 1), 64); err == nil {
				out = append(out, database.Param{Name: name, Value: n})
				continue
			}
		}
		out = append(out, database.Param{Name: name, Value: value})
	}
	return out
}

// ymlWeight reads the standard's <weight>, stated in kilograms. Anything
// unparseable is no weight at all: a number off by a thousand is worse than a
// missing one, and it is silent.
func ymlWeight(raw string) *int64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v <= 0 {
		return nil
	}
	g := int64(math.Round(v * 1000))
	return &g
}

// ymlDimensions reads the standard's <dimensions>: length/width/height in
// centimetres, slash-separated ("20.1/30.5/11"). All three or nothing - a
// parcel with two sides is not a parcel, and a marketplace card refuses it just
// as a delivery quote would.
func ymlDimensions(raw string) (l, w, h *int64) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) != 3 {
		return nil, nil, nil
	}
	out := make([]*int64, 3)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return nil, nil, nil
		}
		if out[i] = millimetres(v, "cm"); out[i] == nil {
			return nil, nil, nil
		}
	}
	return out[0], out[1], out[2]
}

func (y *YML) Fetch() ([]Item, error) {
	y.errors = 0
	y.currency = ""
	var items []Item
	err := y.each(func(o *ymlOffer) {
		// The feed's currency is whatever its first offer quotes. A mixed feed is
		// not importable: two currencies cannot share one price column and we
		// hold no exchange rate.
		c := feedCurrency(o.CurrencyID)
		if c == "" && strings.TrimSpace(o.CurrencyID) != "" {
			// A currency the shop does not deal in would land in the price as is.
			y.errors++
			return
		}
		if c != "" {
			if y.currency == "" {
				y.currency = c
			}
			if c != y.currency {
				y.errors++
				return
			}
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(o.Price), 64)
		if err != nil {
			y.errors++
			return
		}
		sku := strings.TrimSpace(o.VendorCode)
		if sku == "" {
			sku = strings.TrimSpace(o.ID)
		}
		item := Item{
			SKU:         sku,
			Title:       strings.TrimSpace(o.Name),
			Description: strings.TrimSpace(o.Description),
			Price:       int64(math.Round(v * 100)),
			Stock:       y.stock(o),
			ImageURLs:   o.Pictures,
			Category:    categoryPath(y.categories, strings.TrimSpace(o.CategoryID)),
			Brand:       strings.TrimSpace(o.Vendor),
			Params:      offerParams(o.Params),
			WeightG:     ymlWeight(o.Weight),
		}
		item.LengthMM, item.WidthMM, item.HeightMM = ymlDimensions(o.Dimensions)
		items = append(items, item)
	})
	if err != nil {
		return nil, err
	}
	trimCommonRoot(items)
	return items, nil
}
