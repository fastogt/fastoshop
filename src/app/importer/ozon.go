package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
)

// Ozon Seller API: https://docs.ozon.ru/api/seller/
type Ozon struct {
	ClientID string
	APIKey   string
	BaseURL  string // по умолчанию https://api-seller.ozon.ru; в тестах — мок
}

func (o *Ozon) Name() string { return "ozon" }

// Currency: an Ozon seller account is settled in roubles whatever the shop sells in.
func (o *Ozon) Currency() string { return database.ShopCurrencyRUB }

func (o *Ozon) base() string {
	if o.BaseURL != "" {
		return o.BaseURL
	}
	return "https://api-seller.ozon.ru"
}

func (o *Ozon) post(path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", o.base()+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Client-Id", o.ClientID)
	req.Header.Set("Api-Key", o.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := kHTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("ozon %s: %d: %s", path, resp.StatusCode, raw)
	}
	return json.Unmarshal(raw, out)
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

// Тела запросов — именованные структуры, не map[string]any (правило проекта).
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

// stocks отдаёт доступный к продаже FBS-остаток по product_id. Свободный
// остаток — present минус reserved: зарезервированное уже продано.
func (o *Ozon) stocks() map[int64]int {
	var out ozonStocksResponse
	// ponytail: одна страница без курсора — столько же, сколько тянет list().
	if err := o.post("/v4/product/info/stocks",
		ozonStocksRequest{Limit: 1000, Filter: ozonStocksFilter{Visibility: "ALL"}}, &out); err != nil {
		// Остаток не критичен для переноса карточек: импорт не валим.
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
	// ponytail: одна страница (1000 товаров), пагинация — когда появится
	// клиент с каталогом больше.
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
