package importer

import (
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/ozon"
)

// Ozon Seller API: https://docs.ozon.ru/api/seller/
type Ozon struct {
	ClientID string
	APIKey   string
	BaseURL  string // defaults to https://api-seller.ozon.ru; a mock in tests
}

func (o *Ozon) Name() string { return "ozon" }

// Currency: an Ozon seller account is settled in roubles whatever the shop sells in.
func (o *Ozon) Currency() string { return database.ShopCurrencyRUB }

// post goes through the channel package's client: one implementation of the
// Seller API auth and error handling instead of a second copy here.
func (o *Ozon) post(path string, body any, out any) error {
	c := &ozon.Client{ClientID: o.ClientID, APIKey: o.APIKey, BaseURL: o.BaseURL}
	return c.Post(path, body, out)
}

type ozonListResponse struct {
	Result struct {
		Items []struct {
			ProductID int64  `json:"product_id"`
			OfferID   string `json:"offer_id"`
		} `json:"items"`
		Total int `json:"total"`
	} `json:"result"`
}

// Request bodies are named structs, not map[string]any (project rule).
type ozonListFilter struct {
	Visibility string `json:"visibility"`
}

type ozonListRequest struct {
	Filter ozonListFilter `json:"filter"`
	Limit  int            `json:"limit"`
}

type ozonProductIDsRequest struct {
	ProductID []int64 `json:"product_id"`
}

type ozonProductIDRequest struct {
	ProductID int64 `json:"product_id"`
}

type ozonStocksFilter struct {
	Visibility string `json:"visibility"`
}

type ozonStocksRequest struct {
	Cursor string           `json:"cursor"`
	Limit  int              `json:"limit"`
	Filter ozonStocksFilter `json:"filter"`
}

type ozonStocksResponse struct {
	Items []struct {
		ProductID int64 `json:"product_id"`
		Stocks    []struct {
			Present  int    `json:"present"`
			Reserved int    `json:"reserved"`
			Type     string `json:"type"`
		} `json:"stocks"`
	} `json:"items"`
}

// stocks returns the sellable FBS stock keyed by product_id. Free stock is
// present minus reserved: what is reserved has already been sold.
func (o *Ozon) stocks() map[int64]int {
	var out ozonStocksResponse
	// ponytail: one page without a cursor — the same amount list() pulls.
	if err := o.post("/v4/product/info/stocks",
		ozonStocksRequest{Limit: 1000, Filter: ozonStocksFilter{Visibility: "ALL"}}, &out); err != nil {
		// Stock is not critical for migrating cards: don't fail the import.
		log.Warnf("import ozon: stocks: %v", err)
		return nil
	}
	res := make(map[int64]int, len(out.Items))
	for _, it := range out.Items {
		for _, s := range it.Stocks {
			if !strings.EqualFold(s.Type, "fbs") {
				continue
			}
			if n := s.Present - s.Reserved; n > 0 {
				res[it.ProductID] += n
			}
		}
	}
	return res
}

func (o *Ozon) list() (*ozonListResponse, error) {
	var out ozonListResponse
	// ponytail: one page (1000 products); pagination comes when a client
	// with a bigger catalogue shows up.
	err := o.post("/v3/product/list",
		ozonListRequest{Filter: ozonListFilter{Visibility: "ALL"}, Limit: 1000}, &out)
	return &out, err
}

func (o *Ozon) Count() (int, error) {
	l, err := o.list()
	if err != nil {
		return 0, err
	}
	return l.Result.Total, nil
}

func (o *Ozon) Fetch() ([]Item, error) {
	l, err := o.list()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(l.Result.Items))
	for _, it := range l.Result.Items {
		ids = append(ids, it.ProductID)
	}
	var info struct {
		Items []struct {
			ID      int64  `json:"id"`
			OfferID string `json:"offer_id"`
			Name    string `json:"name"`
			// marketing_price was retired by Ozon on 12.11.2025 and now comes
			// back empty: reading it imported every catalogue at price 0.
			Price  string   `json:"price"`
			Images []string `json:"images"`
		} `json:"items"`
	}
	if err := o.post("/v3/product/info/list", ozonProductIDsRequest{ProductID: ids}, &info); err != nil {
		return nil, err
	}
	stockByID := o.stocks()
	items := make([]Item, 0, len(info.Items))
	for _, it := range info.Items {
		price, _ := strconv.ParseFloat(strings.TrimSpace(it.Price), 64)
		var desc struct {
			Result struct {
				Description string `json:"description"`
			} `json:"result"`
		}
		_ = o.post("/v1/product/info/description", ozonProductIDRequest{ProductID: it.ID}, &desc)
		items = append(items, Item{
			SKU: it.OfferID, Title: it.Name, Description: desc.Result.Description,
			Price: int64(price * 100), Stock: stockByID[it.ID], ImageURLs: it.Images,
		})
	}
	return items, nil
}
