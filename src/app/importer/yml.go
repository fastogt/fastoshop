package importer

import (
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
	// DefaultStock — YML carries no quantity, only an availability flag,
	// so the seller sets the stock as one number for the whole catalogue.
	DefaultStock int
	// MaxBytes — body size ceiling; 0 means kMaxFeedBytes.
	MaxBytes int64

	errors   int
	currency string
}

// ponytail: 100 MB is plenty (a live export of 24k products is 33 MB);
// the ceiling exists so a bad link cannot drag gigabytes into memory.
const kMaxFeedBytes = 100 << 20

func (y *YML) Name() string { return "yml" }

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

// FetchErrors — cards rejected before hitting the DB (foreign currency, broken price).
func (y *YML) FetchErrors() int { return y.errors }

type ymlOffer struct {
	ID          string   `xml:"id,attr"`
	Available   string   `xml:"available,attr"`
	Name        string   `xml:"name"`
	VendorCode  string   `xml:"vendorCode"`
	Description string   `xml:"description"`
	Price       string   `xml:"price"`
	CurrencyID  string   `xml:"currencyId"`
	Pictures    []string `xml:"picture"`
}

// each downloads the feed and yields offers one at a time. Parsing is streamed:
// exports run to tens of megabytes, and xml.Unmarshal would hold both the
// document and the tree in memory.
func (y *YML) each(fn func(o *ymlOffer)) error {
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
	limit := y.MaxBytes
	if limit <= 0 {
		limit = kMaxFeedBytes
	}
	// LimitedReader with one spare byte: when N runs out mid-document the
	// decoder fails, and N == 0 tells truncation apart from real bad XML.
	lr := &io.LimitedReader{R: resp.Body, N: limit + 1}
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
		if !ok || se.Name.Local != "offer" {
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

// Count downloads the feed again — a second download is cheaper than keeping
// 33 MB in process memory between "Check" and "Import".
func (y *YML) Count() (int, error) {
	n := 0
	if err := y.each(func(*ymlOffer) { n++ }); err != nil {
		return 0, err
	}
	return n, nil
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
		items = append(items, Item{
			SKU:         sku,
			Title:       strings.TrimSpace(o.Name),
			Description: strings.TrimSpace(o.Description),
			Price:       int64(math.Round(v * 100)),
			Stock:       y.DefaultStock,
			ImageURLs:   o.Pictures,
		})
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}
