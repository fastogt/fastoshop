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

// YML is the Yandex.Market export format (yml_catalog): Bitrix, InSales, Tilda.
type YML struct {
	URL string
	// Data is the feed itself, when the owner uploads the file instead of hosting it.
	Data []byte
	// DefaultStock - the standard carries no quantity, only an availability flag.
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

// An XML declaration is optional, so the root element is what settles it.
func IsYML(raw []byte) bool {
	head := raw
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.Contains(head, []byte("<yml_catalog"))
}

// Currency is only known after Fetch: the feed has to be parsed to see it.
func (y *YML) Currency() string { return y.currency }

// Live feeds still use the legacy codes RUR and pre-2016 BYR.
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
	// Outside the standard: only a shop of ours puts a quantity in a feed.
	Count      string     `xml:"count"`
	Params     []ymlParam `xml:"param"`
	Weight     string     `xml:"weight"`
	Dimensions string     `xml:"dimensions"`
}

// param is the standard's own place for characteristics; every exporter writes them.
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

// kMaxCategoryDepth stops a feed whose parentId points in a circle.
const kMaxCategoryDepth = 10

// A missing id yields an empty string: a product with no category is normal.
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

// A feed's tree starts at the site's root; the shared segments go, the leaf stays.
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

// Parsing is streamed: exports run to tens of megabytes.
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
	// One spare byte: N == 0 on a decoder failure tells truncation from bad XML.
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
		// <categories> comes before <offers>: the tree is complete by the first offer.
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

// A per-offer quantity beats the seller's one number for the whole catalogue.
func (y *YML) stock(o *ymlOffer) int {
	if n, err := strconv.Atoi(strings.TrimSpace(o.Count)); err == nil && n >= 0 {
		return n
	}
	return y.DefaultStock
}

// A stated unit joins the caption and is what makes the value numeric, never the digits.
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

// The standard's <weight> is stated in kilograms.
func ymlWeight(raw string) *int64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || v <= 0 {
		return nil
	}
	g := int64(math.Round(v * 1000))
	return &g
}

// <dimensions> is l/w/h in centimetres ("20.1/30.5/11"), all three or nothing.
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
		// The first offer sets the feed's currency; a mixed feed is not importable.
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
