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

type ozonCategoryTreeRequest struct {
	Language string `json:"language"`
}

// ozonCategoryNode is one node of the Ozon taxonomy. A branch carries
// description_category_id and a name, a leaf carries type_id and type_name —
// the same structure all the way down, so one type reads the whole tree.
type ozonCategoryNode struct {
	CategoryID int64              `json:"description_category_id"`
	Name       string             `json:"category_name"`
	TypeID     int64              `json:"type_id"`
	TypeName   string             `json:"type_name"`
	Children   []ozonCategoryNode `json:"children"`
}

type ozonCategoryTreeResponse struct {
	Result []ozonCategoryNode `json:"result"`
}

// ozonCategoryKey identifies a card's place in the taxonomy: the category alone
// is not enough, two types under one category are different shelves.
type ozonCategoryKey struct {
	CategoryID int64
	TypeID     int64
}

// categoryPaths downloads the taxonomy once per import and flattens it into
// "Дом и сад/Кухня/Посуда для сервировки/Тарелки". One request for the whole
// catalogue, not one per card. A failure is not fatal: the products are worth
// more than their categories, so the import goes on without them.
func (o *Ozon) categoryPaths() map[ozonCategoryKey]string {
	var resp ozonCategoryTreeResponse
	if err := o.post("/v1/description-category/tree",
		ozonCategoryTreeRequest{Language: "RU"}, &resp); err != nil {
		log.Warnf("ozon: category tree: %v", err)
		return nil
	}
	paths := map[ozonCategoryKey]string{}
	var walk func(nodes []ozonCategoryNode, parents []string, categoryID int64)
	walk = func(nodes []ozonCategoryNode, parents []string, categoryID int64) {
		for _, n := range nodes {
			id := categoryID
			if n.CategoryID != 0 {
				id = n.CategoryID
			}
			name := n.Name
			if name == "" {
				name = n.TypeName
			}
			// A fresh slice per node: append onto the parent's backing array and
			// siblings overwrite each other's last segment.
			path := append(append([]string{}, parents...), name)
			if n.TypeID != 0 {
				paths[ozonCategoryKey{CategoryID: id, TypeID: n.TypeID}] = database.CategoryPath(path...)
			}
			walk(n.Children, path, id)
		}
	}
	walk(resp.Result, nil, 0)
	return paths
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
			Price      string   `json:"price"`
			Images     []string `json:"images"`
			CategoryID int64    `json:"description_category_id"`
			TypeID     int64    `json:"type_id"`
		} `json:"items"`
	}
	if err := o.post("/v3/product/info/list", ozonProductIDsRequest{ProductID: ids}, &info); err != nil {
		return nil, err
	}
	stockByID := o.stocks()
	categories := o.categoryPaths()
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
			Category: categories[ozonCategoryKey{CategoryID: it.CategoryID, TypeID: it.TypeID}],
		})
	}
	return items, nil
}
