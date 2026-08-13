package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
)

// WB: Content API (cards) + Prices API (prices) + Marketplace API (stock on
// the seller's warehouses) — three different hosts. https://dev.wildberries.ru/
type WB struct {
	Token          string
	ContentURL     string // defaults to https://content-api.wildberries.ru
	PricesURL      string // defaults to https://discounts-prices-api.wildberries.ru
	MarketplaceURL string // defaults to https://marketplace-api.wildberries.ru
}

func (w *WB) Name() string { return "wb" }

// Currency: a Wildberries seller account is settled in roubles.
func (w *WB) Currency() string { return database.ShopCurrencyRUB }

func (w *WB) content() string {
	if w.ContentURL != "" {
		return w.ContentURL
	}
	return "https://content-api.wildberries.ru"
}

func (w *WB) prices() string {
	if w.PricesURL != "" {
		return w.PricesURL
	}
	return "https://discounts-prices-api.wildberries.ru"
}

func (w *WB) marketplace() string {
	if w.MarketplaceURL != "" {
		return w.MarketplaceURL
	}
	return "https://marketplace-api.wildberries.ru"
}

func (w *WB) do(method, url string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", w.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := kHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("wb %s: %d: %s", url, resp.StatusCode, raw)
	}
	return json.Unmarshal(raw, out)
}

type wbCardsResponse struct {
	Cards []struct {
		NmID        int64  `json:"nmID"`
		VendorCode  string `json:"vendorCode"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Photos      []struct {
			Big string `json:"big"`
		} `json:"photos"`
		Sizes []struct {
			ChrtID   int64    `json:"chrtID"`
			TechSize string   `json:"techSize"`
			WBSize   string   `json:"wbSize"`
			Skus     []string `json:"skus"` // the size's barcodes
		} `json:"sizes"`
	} `json:"cards"`
	Cursor struct {
		Total int `json:"total"`
	} `json:"cursor"`
}

type wbCursor struct {
	Limit int `json:"limit"`
}

type wbFilter struct {
	WithPhoto int `json:"withPhoto"`
}

type wbSettings struct {
	Cursor wbCursor `json:"cursor"`
	Filter wbFilter `json:"filter"`
}

type wbCardsRequest struct {
	Settings wbSettings `json:"settings"`
}

func (w *WB) cards() (*wbCardsResponse, error) {
	var out wbCardsResponse
	// ponytail: one page (100 cards); pagination comes when it is needed.
	err := w.do("POST", w.content()+"/content/v2/get/cards/list",
		wbCardsRequest{Settings: wbSettings{
			Cursor: wbCursor{Limit: 100}, Filter: wbFilter{WithPhoto: -1}}}, &out)
	return &out, err
}

func (w *WB) Count() (int, error) {
	c, err := w.cards()
	if err != nil {
		return 0, err
	}
	return c.Cursor.Total, nil
}

type wbWarehouse struct {
	ID int64 `json:"id"`
}

type wbStocksRequest struct {
	Skus []string `json:"skus"`
}

type wbStocksResponse struct {
	Stocks []struct {
		SKU    string `json:"sku"`
		Amount int    `json:"amount"`
	} `json:"stocks"`
}

// stocks sums a size's stock across all the seller's warehouses: our model has
// one warehouse while WB allows keeping goods on several. The key is the
// barcode, not chrtID: on WB the FBS stock hangs off the size's barcode.
func (w *WB) stocks(barcodes []string) map[string]int {
	if len(barcodes) == 0 {
		return nil
	}
	var whs []wbWarehouse
	if err := w.do("GET", w.marketplace()+"/api/v3/warehouses", nil, &whs); err != nil {
		// Stock is not critical for migrating cards: don't fail the import.
		log.Warnf("import wb: warehouses: %v", err)
		return nil
	}
	// ponytail: the method's limit is 1000 barcodes per request; a bigger
	// catalogue is cut at the first thousand, like everything else in this importer.
	if len(barcodes) > 1000 {
		barcodes = barcodes[:1000]
	}
	res := map[string]int{}
	for _, wh := range whs {
		var out wbStocksResponse
		url := w.marketplace() + "/api/v3/stocks/" + strconv.FormatInt(wh.ID, 10)
		if err := w.do("POST", url, wbStocksRequest{Skus: barcodes}, &out); err != nil {
			log.Warnf("import wb: stocks %d: %v", wh.ID, err)
			continue
		}
		for _, s := range out.Stocks {
			res[s.SKU] += s.Amount
		}
	}
	return res
}

func (w *WB) priceBySize() (map[int64]int64, map[int64]int64) {
	var priceResp struct {
		Data struct {
			ListGoods []struct {
				NmID  int64 `json:"nmID"`
				Sizes []struct {
					SizeID          int64   `json:"sizeID"`
					DiscountedPrice float64 `json:"discountedPrice"`
				} `json:"sizes"`
			} `json:"listGoods"`
		} `json:"data"`
	}
	byNm, bySize := map[int64]int64{}, map[int64]int64{}
	if err := w.do("GET", w.prices()+"/api/v2/list/goods/filter?limit=1000", nil, &priceResp); err != nil {
		log.Warnf("import wb: prices: %v", err)
		return byNm, bySize
	}
	for _, g := range priceResp.Data.ListGoods {
		for i, s := range g.Sizes {
			kop := int64(s.DiscountedPrice * 100)
			// sizeID in the Prices API is the same chrtID as in Content.
			bySize[s.SizeID] = kop
			if i == 0 {
				byNm[g.NmID] = kop
			}
		}
	}
	return byNm, bySize
}

// Fetch expands a WB card into a product per size: we have one price and one
// stock per product with no variants in the model, while on WB the FBS stock
// lives exactly on the size.
func (w *WB) Fetch() ([]Item, error) {
	c, err := w.cards()
	if err != nil {
		return nil, err
	}
	priceByNm, priceBySize := w.priceBySize()
	var barcodes []string
	for _, card := range c.Cards {
		for _, s := range card.Sizes {
			// ponytail: a size may have several barcodes; take the first —
			// it is the same one we ask the stock for.
			if len(s.Skus) > 0 {
				barcodes = append(barcodes, s.Skus[0])
			}
		}
	}
	stockByBarcode := w.stocks(barcodes)

	items := make([]Item, 0, len(c.Cards))
	for _, card := range c.Cards {
		urls := make([]string, 0, len(card.Photos))
		for _, ph := range card.Photos {
			urls = append(urls, ph.Big)
		}
		if len(card.Sizes) == 0 {
			items = append(items, Item{
				SKU: card.VendorCode, Title: card.Title, Description: card.Description,
				Price: priceByNm[card.NmID], ImageURLs: urls,
			})
			continue
		}
		multi := len(card.Sizes) > 1
		for _, s := range card.Sizes {
			var barcode string
			if len(s.Skus) > 0 {
				barcode = s.Skus[0]
			}
			title, sku := card.Title, card.VendorCode
			if multi {
				label := s.TechSize
				if label == "" {
					label = s.WBSize
				}
				if label == "" {
					label = strconv.FormatInt(s.ChrtID, 10)
				}
				title += ", " + label
				sku += "-" + label
			}
			price := priceBySize[s.ChrtID]
			if price == 0 {
				price = priceByNm[card.NmID]
			}
			items = append(items, Item{
				SKU: sku, Title: title, Description: card.Description,
				Price: price, Stock: stockByBarcode[barcode], ImageURLs: urls,
			})
		}
	}
	return items, nil
}
