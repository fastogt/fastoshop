package importer

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
)

// YML — выгрузка для Яндекс.Маркета (yml_catalog): стандартный экспорт
// Битрикса, InSales, Тильды. Источник без ключей: продавец даёт ссылку на XML.
type YML struct {
	URL string
	// DefaultStock — YML не передаёт количество, только флаг наличия,
	// поэтому остаток задаёт продавец одним числом на весь каталог.
	DefaultStock int
	// MaxBytes — потолок размера тела; 0 означает kMaxFeedBytes.
	MaxBytes int64

	errors   int
	currency string
}

// ponytail: 100 МБ хватает с запасом (живая выгрузка на 24k товаров — 33 МБ);
// потолок нужен, чтобы кривая ссылка не утянула гигабайты в память.
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

// FetchErrors — карточки, отбракованные до записи в БД (чужая валюта, битая цена).
func (y *YML) FetchErrors() int { return y.errors }

type ymlOffer struct {
	ID          string   `xml:"id,attr"`
	Available   string   `xml:"available,attr"`
	Name        string   `xml:"name"`
	VendorCode  string   `xml:"vendorCode"`
	Description string   `xml:"description"`
	Price       string   `xml:"price"`
	CurrencyID  string   `xml:"currencyId"`
	Barcode     string   `xml:"barcode"`
	Pictures    []string `xml:"picture"`
}

// each качает фид и отдаёт офферы по одному. Разбор потоковый: выгрузки бывают
// в десятки мегабайт, xml.Unmarshal держал бы в памяти и документ, и дерево.
func (y *YML) each(fn func(o *ymlOffer)) error {
	if !strings.HasPrefix(y.URL, "http://") && !strings.HasPrefix(y.URL, "https://") {
		return fmt.Errorf("yml: ссылка должна начинаться с http:// или https://")
	}
	resp, err := kHTTP.Get(y.URL)
	if err != nil {
		return fmt.Errorf("yml: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("yml: сервер ответил %d", resp.StatusCode)
	}
	limit := y.MaxBytes
	if limit <= 0 {
		limit = kMaxFeedBytes
	}
	dec := xml.NewDecoder(&capReader{r: resp.Body, left: limit})
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(err, errFeedTooBig) {
				return fmt.Errorf("yml: выгрузка больше %d МБ — такой каталог мы не тянем", limit>>20)
			}
			return fmt.Errorf("yml: разбор XML: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "offer" {
			continue
		}
		var o ymlOffer
		if err := dec.DecodeElement(&o, &se); err != nil {
			return fmt.Errorf("yml: разбор оффера: %w", err)
		}
		if strings.EqualFold(strings.TrimSpace(o.Available), "false") {
			continue
		}
		fn(&o)
	}
	return nil
}

var errFeedTooBig = errors.New("feed too big")

type capReader struct {
	r    io.Reader
	left int64
}

func (c *capReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.left -= int64(n)
	if c.left < 0 {
		return n, errFeedTooBig
	}
	return n, err
}

// Count скачивает фид ещё раз — вторая загрузка дешевле, чем держать 33 МБ
// между «Проверить» и «Импортировать» в памяти процесса.
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
			Barcode:     strings.TrimSpace(o.Barcode),
		})
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}
