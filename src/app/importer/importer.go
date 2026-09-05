package importer

import (
	"math"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/fastogt/fastoshop/app/database"
)

// Item is a card from an external seller account in canonical form.
type Item struct {
	SKU         string
	Title       string
	Description string
	Price       int64 // minor units
	Stock       int
	ImageURLs   []string
	// Category is a path from the root down, segments joined by "/".
	Category string
	// Gross weight in grams and packed size in millimetres; nil means unstated.
	WeightG  *int64
	LengthMM *int64
	WidthMM  *int64
	HeightMM *int64
	// Brand is the manufacturer (Ozon attribute 85, YML <vendor>), not the supplier.
	Brand string
	// Characteristics as stated; weight and dimensions deliberately stay out.
	Params []database.Param
}

// fill takes the feed's value only when there is nothing to keep.
func fill(have, incoming *int64) (*int64, bool) {
	if have != nil || incoming == nil {
		return have, false
	}
	return incoming, true
}

// An unknown unit returns nothing rather than a guess off by a factor of a thousand.
func grams(v float64, unit string) *int64 {
	var g float64
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "g", "г", "":
		g = v
	case "kg", "кг":
		g = v * 1000
	default:
		return nil
	}
	return positive(g)
}

func millimetres(v float64, unit string) *int64 {
	var mm float64
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "mm", "мм", "":
		mm = v
	case "cm", "см":
		mm = v * 10
	case "m", "м":
		mm = v * 1000
	default:
		return nil
	}
	return positive(mm)
}

// Platforms send zero for "not filled in", so it must not become a measurement.
func positive(v float64) *int64 {
	n := int64(math.Round(v))
	if n <= 0 {
		return nil
	}
	return &n
}

// Source is a one-off catalogue source (Ozon, WB). Not a Channel: read-only.
type Source interface {
	Name() string
	Fetch() ([]Item, error)
}

// SourceErrors - cards a source rejected before the DB; they belong in Result.
type SourceErrors interface {
	FetchErrors() int
}

// SourceCurrency - the currency the prices came in; empty means unknown.
type SourceCurrency interface {
	Currency() string
}

// We hold no exchange rate and fetch none - it goes into the coefficient.
func FeedCurrency(src Source) string {
	if c, ok := src.(SourceCurrency); ok {
		return c.Currency()
	}
	return ""
}

type Result struct {
	Imported int `json:"imported"`
	// Skipped counts unchanged rows plus rows the import refused to create.
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	// Zeroed products are not deleted: channel links and indexed slugs point at them.
	Zeroed int `json:"zeroed"`
	Errors int `json:"errors"`
}

// Stages travel to the admin as keys and are rendered in the owner's language.
const (
	StageFetch    = "fetch"
	StageProducts = "products"
)

var kHTTP = &http.Client{Timeout: 60 * time.Second}

// Run imports into one supplier group, which alone decides what may be touched.
func Run(src Source, db *database.Database, supplier string, coefficient float64,
	onProgress func(stage string, done, total int)) (*Result, error) {
	progress := func(stage string, done, total int) {
		if onProgress != nil {
			onProgress(stage, done, total)
		}
	}
	// The markup ladder belongs to the shop, not the feed: read it once per import.
	rules, err := db.ShopPriceRules()
	if err != nil {
		return nil, err
	}
	progress(StageFetch, 0, 0)
	items, err := src.Fetch()
	if err != nil {
		return nil, err
	}
	existing, err := db.ListProducts()
	if err != nil {
		return nil, err
	}
	bySKU := map[string]database.Product{}
	for _, p := range existing {
		if p.SKU != "" {
			bySKU[p.SKU] = p
		}
	}
	seen := map[string]bool{}
	res := &Result{}
	if se, ok := src.(SourceErrors); ok {
		res.Errors += se.FetchErrors()
	}
	w, err := db.NewImportWriter()
	if err != nil {
		return nil, err
	}
	for i, it := range items {
		progress(StageProducts, i, len(items))
		if it.SKU == "" || seen[it.SKU] {
			res.Skipped++
			continue
		}
		seen[it.SKU] = true
		if old, found := bySKU[it.SKU]; found {
			// An article owned by another supplier group is left untouched.
			if old.Supplier != supplier {
				res.Skipped++
				continue
			}
			changed, err := merge(w, old, it, coefficient, rules)
			if err != nil {
				log.Warnf("import %s: update %q: %v", src.Name(), it.Title, err)
				res.Errors++
				continue
			}
			if changed {
				res.Updated++
			} else {
				res.Skipped++
			}
			continue
		}
		// A zero price is refused: free goods are worse than missing ones.
		if it.Price <= 0 {
			res.Skipped++
			continue
		}
		p := &database.Product{SKU: it.SKU, Title: it.Title,
			Description: it.Description, Price: database.ShelfPrice(rules, it.Price, coefficient),
			SourcePrice: it.Price, Stock: max(it.Stock, 0),
			Category: it.Category, Brand: it.Brand, Supplier: supplier,
			WeightG: it.WeightG, LengthMM: it.LengthMM,
			WidthMM: it.WidthMM, HeightMM: it.HeightMM,
			Params: it.Params}
		if err := w.CreateProduct(p); err != nil {
			log.Warnf("import %s: create %q: %v", src.Name(), it.Title, err)
			res.Errors++
			continue
		}
		// product_images.path takes an absolute URL as well as a local name.
		_ = w.AddImages(p.ID, it.ImageURLs)
		res.Imported++
	}

	progress(StageProducts, len(items), len(items))

	// An empty feed is a supplier's outage, not a decision to withdraw the catalogue.
	if len(items) > 0 {
		gone := missing(existing, seen, supplier)
		if err := w.Close(w.ZeroStock(gone)); err != nil {
			return nil, err
		}
		res.Zeroed = len(gone)
		return res, nil
	}
	return res, w.Close(nil)
}

// merge writes only what the source owns; the owner's title, text and photos stay.
func merge(wr *database.ImportWriter, old database.Product, it Item, coefficient float64,
	rules []database.PriceRule) (bool, error) {
	price := old.Price
	if !old.PriceManual {
		price = database.ShelfPrice(rules, it.Price, coefficient)
	}
	// A category the owner already set is theirs; an empty one gets filled.
	category := old.Category
	if category == "" {
		category = it.Category
	}
	// Measurements follow the category rule: empty is filled, stated is left alone.
	weight, filled := fill(old.WeightG, it.WeightG)
	length, l := fill(old.LengthMM, it.LengthMM)
	width, w := fill(old.WidthMM, it.WidthMM)
	height, hh := fill(old.HeightMM, it.HeightMM)
	measured := filled || l || w || hh
	// Characteristics are all-or-nothing: no key-by-key merge into a half-owned set.
	params, gained := old.Params, false
	if len(params) == 0 && len(it.Params) > 0 {
		params, gained = it.Params, true
	}
	brand := old.Brand
	if brand == "" {
		brand = it.Brand
	}
	if old.SourcePrice == it.Price && old.Stock == max(it.Stock, 0) &&
		old.Price == price && old.Category == category && old.Brand == brand &&
		!measured && !gained {
		return false, nil
	}
	old.Params = params
	old.Brand = brand
	old.WeightG, old.LengthMM = weight, length
	old.WidthMM, old.HeightMM = width, height
	old.SourcePrice = it.Price
	old.Stock = max(it.Stock, 0)
	old.Price = price
	old.Category = category
	return true, wr.UpdateProduct(&old)
}

// missing is what this group's feed stopped listing and still has stock.
func missing(existing []database.Product, seen map[string]bool, supplier string) []int64 {
	var ids []int64
	for _, p := range existing {
		if p.SKU != "" && p.Supplier == supplier && !seen[p.SKU] && p.Stock != 0 {
			ids = append(ids, p.ID)
		}
	}
	return ids
}
