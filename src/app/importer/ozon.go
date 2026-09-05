package importer

import (
	"strconv"
	"strings"
	"sync"

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

// post goes through the ozon package's client: one Seller API auth in one place.
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

// A taxonomy branch carries description_category_id/name, a leaf type_id/type_name.
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

// Category alone is not a place: two types under one category are different shelves.
type ozonCategoryKey struct {
	CategoryID int64
	TypeID     int64
}

// The taxonomy is one request per import, flattened into a path; a failure is not fatal.
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
			// A fresh slice per node: siblings would share the parent's backing array.
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

// Attributes hold ids; the category dictionary names and types them.

type ozonAttributesFilter struct {
	ProductID  []int64 `json:"product_id"`
	Visibility string  `json:"visibility"`
}

type ozonAttributesRequest struct {
	Filter ozonAttributesFilter `json:"filter"`
	Limit  int                  `json:"limit"`
}

type ozonAttrValue struct {
	DictionaryValueID int64  `json:"dictionary_value_id"`
	Value             string `json:"value"`
}

type ozonAttributesResponse struct {
	Result []struct {
		ID         int64 `json:"id"`
		Attributes []struct {
			// The dictionary calls the same field attribute_id.
			ID     int64           `json:"id"`
			Values []ozonAttrValue `json:"values"`
		} `json:"attributes"`
	} `json:"result"`
}

type ozonAttributeDictRequest struct {
	CategoryID int64  `json:"description_category_id"`
	TypeID     int64  `json:"type_id"`
	Language   string `json:"language"`
}

type ozonAttributeDef struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	IsCollection bool   `json:"is_collection"`
}

type ozonAttributeDictResponse struct {
	Result []ozonAttributeDef `json:"result"`
}

// One dictionary call per category in use; a failed one leaves values under their id.
func (o *Ozon) attributeDicts(keys map[ozonCategoryKey]bool) map[ozonCategoryKey]map[int64]ozonAttributeDef {
	out := make(map[ozonCategoryKey]map[int64]ozonAttributeDef, len(keys))
	for k := range keys {
		var resp ozonAttributeDictResponse
		if err := o.post("/v1/description-category/attribute",
			ozonAttributeDictRequest{CategoryID: k.CategoryID, TypeID: k.TypeID, Language: "RU"},
			&resp); err != nil {
			log.Warnf("ozon: attribute dictionary for %d/%d: %v", k.CategoryID, k.TypeID, err)
			continue
		}
		defs := make(map[int64]ozonAttributeDef, len(resp.Result))
		for _, d := range resp.Result {
			defs[d.ID] = d
		}
		out[k] = defs
	}
	return out
}

// attributes returns a card's characteristics keyed by product id.
func (o *Ozon) attributes(ids []int64) map[int64][]ozonAttrValueSet {
	var resp ozonAttributesResponse
	if err := o.post("/v4/product/info/attributes",
		ozonAttributesRequest{Limit: 1000,
			Filter: ozonAttributesFilter{ProductID: ids, Visibility: "ALL"}}, &resp); err != nil {
		// Characteristics are description, not the product: do not fail the import.
		log.Warnf("ozon: attributes: %v", err)
		return nil
	}
	out := make(map[int64][]ozonAttrValueSet, len(resp.Result))
	for _, p := range resp.Result {
		for _, a := range p.Attributes {
			out[p.ID] = append(out[p.ID], ozonAttrValueSet{ID: a.ID, Values: a.Values})
		}
	}
	return out
}

type ozonAttrValueSet struct {
	ID     int64
	Values []ozonAttrValue
}

// Attributes that are not characteristics: 85 is the brand, 11254 rich content.
const (
	kOzonAttrBrand       = 85
	kOzonAttrRichContent = 11254
)

// ozonBrand pulls the maker's name out of the attribute set.
func ozonBrand(sets []ozonAttrValueSet) string {
	for _, s := range sets {
		if s.ID != kOzonAttrBrand {
			continue
		}
		for _, v := range s.Values {
			if raw := strings.TrimSpace(v.Value); raw != "" {
				return raw
			}
		}
	}
	return ""
}

// Values are read as the type Ozon declared, not as their digits look.
func ozonParams(sets []ozonAttrValueSet, defs map[int64]ozonAttributeDef) []database.Param {
	var out []database.Param
	for _, s := range sets {
		if s.ID == kOzonAttrBrand || s.ID == kOzonAttrRichContent {
			continue
		}
		def, known := defs[s.ID]
		name := def.Name
		if !known || name == "" {
			// An id is not a caption, but a value without one still beats no value.
			name = "attribute " + strconv.FormatInt(s.ID, 10)
		}
		var vals []any
		for _, v := range s.Values {
			if raw := strings.TrimSpace(v.Value); raw != "" {
				vals = append(vals, ozonValue(def.Type, raw))
			}
		}
		switch {
		case len(vals) == 0:
		case len(vals) == 1 && !def.IsCollection:
			out = append(out, database.Param{Name: name, Value: vals[0]})
		default:
			out = append(out, database.Param{Name: name, Value: vals})
		}
	}
	return out
}

// An unparseable number stays a string: Ozon types the field, the seller fills it.
func ozonValue(kind, raw string) any {
	switch strings.ToLower(kind) {
	case "integer", "decimal":
		if f, err := strconv.ParseFloat(strings.Replace(raw, ",", ".", 1), 64); err == nil {
			return f
		}
	case "boolean":
		if b, err := strconv.ParseBool(raw); err == nil {
			return b
		}
	}
	return raw
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

// Free FBS stock is present minus reserved: what is reserved has already been sold.
func (o *Ozon) stocks() map[int64]int {
	var out ozonStocksResponse
	// ponytail: one page without a cursor - the same amount list() pulls.
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

// The description endpoint has no batch form: one call per card, a few at a time.
func (o *Ozon) descriptions(ids []int64) map[int64]string {
	out := make([]string, len(ids))
	var wg sync.WaitGroup
	sem := make(chan struct{}, kImageWorkers)
	for i, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			var desc struct {
				Result struct {
					Description string `json:"description"`
				} `json:"result"`
			}
			if err := o.post("/v1/product/info/description", ozonProductIDRequest{ProductID: id}, &desc); err == nil {
				out[i] = desc.Result.Description
			}
		}()
	}
	wg.Wait()
	m := make(map[int64]string, len(ids))
	for i, id := range ids {
		m[id] = out[i]
	}
	return m
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
			// Ozon retired marketing_price on 12.11.2025; it comes back empty.
			Price      string   `json:"price"`
			Images     []string `json:"images"`
			CategoryID int64    `json:"description_category_id"`
			TypeID     int64    `json:"type_id"`
			// Ozon states its own units alongside the numbers.
			Weight     float64 `json:"weight"`
			WeightUnit string  `json:"weight_unit"`
			Depth      float64 `json:"depth"`
			Width      float64 `json:"width"`
			Height     float64 `json:"height"`
			DimUnit    string  `json:"dimension_unit"`
		} `json:"items"`
	}
	if err := o.post("/v3/product/info/list", ozonProductIDsRequest{ProductID: ids}, &info); err != nil {
		return nil, err
	}
	stockByID := o.stocks()
	categories := o.categoryPaths()
	attrs := o.attributes(ids)
	// Only the categories this catalogue sells in: a dictionary per card is per-card calls.
	used := map[ozonCategoryKey]bool{}
	for _, it := range info.Items {
		used[ozonCategoryKey{CategoryID: it.CategoryID, TypeID: it.TypeID}] = true
	}
	dicts := o.attributeDicts(used)
	descriptions := o.descriptions(ids)
	items := make([]Item, 0, len(info.Items))
	for _, it := range info.Items {
		price, _ := strconv.ParseFloat(strings.TrimSpace(it.Price), 64)
		key := ozonCategoryKey{CategoryID: it.CategoryID, TypeID: it.TypeID}
		items = append(items, Item{
			SKU: it.OfferID, Title: it.Name, Description: descriptions[it.ID],
			Price: int64(price * 100), Stock: stockByID[it.ID], ImageURLs: it.Images,
			Category: categories[key], Brand: ozonBrand(attrs[it.ID]),
			Params:   ozonParams(attrs[it.ID], dicts[key]),
			WeightG:  grams(it.Weight, it.WeightUnit),
			LengthMM: millimetres(it.Depth, it.DimUnit),
			WidthMM:  millimetres(it.Width, it.DimUnit),
			HeightMM: millimetres(it.Height, it.DimUnit),
		})
	}
	return items, nil
}
