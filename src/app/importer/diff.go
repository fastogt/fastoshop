package importer

import (
	"cmp"
	"math"
	"slices"

	"github.com/fastogt/fastoshop/app/database"
)

// kListLimit caps each list in the diff. Three thousand changed rows is not
// something a person reads; the counts plus the outliers are what a decision is
// made on. The response says how many were left out — a silently truncated list
// reads as a complete one.
const kListLimit = 50

// DiffRow is one product in the comparison. Prices are in minor units: Was and
// Now are the supplier's price, Shelf is what the coefficient makes of it.
type DiffRow struct {
	SKU     string  `json:"sku"`
	Title   string  `json:"title"`
	Was     int64   `json:"was"`
	Now     int64   `json:"now"`
	Shelf   int64   `json:"shelf"`
	Percent float64 `json:"percent"`
	Stock   int     `json:"stock"`
}

// Diff is what an import would change, computed against the current catalogue.
// No snapshot of the previous feed is needed: products.source_price already
// holds what the supplier charged last time.
type Diff struct {
	// Currency the feed quotes in, empty when the source does not say. The admin
	// compares it with the shop's own currency: the coefficient is the only thing
	// that converts, and it converts nothing unless the owner puts a rate in it.
	Currency string `json:"currency"`

	Total     int `json:"total"`
	New       int `json:"new"`
	Gone      int `json:"gone"`
	PriceUp   int `json:"price_up"`
	PriceDown int `json:"price_down"`
	Unchanged int `json:"unchanged"`
	// Conflicts are articles owned by another group: this import will leave them
	// alone, and the owner has to decide whose they are.
	Conflicts int `json:"conflicts"`
	// NoSKU are rows the feed sent without an article; they cannot be imported.
	NoSKU int `json:"no_sku"`

	NewItems     []DiffRow `json:"new_items"`
	GoneItems    []DiffRow `json:"gone_items"`
	PriceChanges []DiffRow `json:"price_changes"`
}

// shelfPrice is the shop's own pricing rule in one place: the coefficient
// carries the supplier's number into our money, the ladder adds the margin.
// The preview must use it too, or "Check" promises a price the import will not
// produce.
func shelfPrice(rules []database.PriceRule, source int64, coefficient float64) int64 {
	return database.ShelfPrice(rules, source, coefficient)
}

// Compare matches the incoming catalogue against the shop by article, inside one
// supplier group: counting another supplier's goods as "gone" would be a lie.
// Rows without an article take no part — there is nothing to match them by, and
// guessing by title would silently merge different goods.
func Compare(items []Item, existing []database.Product, supplier string,
	coefficient float64, rules []database.PriceRule) *Diff {
	bySKU := make(map[string]database.Product, len(existing))
	for _, p := range existing {
		if p.SKU != "" {
			bySKU[p.SKU] = p
		}
	}
	seen := make(map[string]bool, len(items))

	d := &Diff{
		Total:        len(items),
		NewItems:     []DiffRow{},
		GoneItems:    []DiffRow{},
		PriceChanges: []DiffRow{},
	}
	var changes []DiffRow
	for _, it := range items {
		if it.SKU == "" {
			d.NoSKU++
			continue
		}
		seen[it.SKU] = true
		old, found := bySKU[it.SKU]
		if found && old.Supplier != supplier {
			d.Conflicts++
			continue
		}
		if !found {
			d.New++
			if len(d.NewItems) < kListLimit {
				d.NewItems = append(d.NewItems, DiffRow{
					SKU: it.SKU, Title: it.Title, Now: it.Price,
					Shelf: shelfPrice(rules, it.Price, coefficient), Stock: it.Stock,
				})
			}
			continue
		}
		if old.SourcePrice == it.Price {
			d.Unchanged++
			continue
		}
		if it.Price > old.SourcePrice {
			d.PriceUp++
		} else {
			d.PriceDown++
		}
		row := DiffRow{
			SKU: it.SKU, Title: it.Title, Was: old.SourcePrice, Now: it.Price,
			Shelf: shelfPrice(rules, it.Price, coefficient), Stock: it.Stock,
		}
		// A product imported before source prices were tracked has 0 there;
		// calling that an infinite rise would push it to the top of the list and
		// bury the real news.
		if old.SourcePrice > 0 {
			row.Percent = float64(it.Price-old.SourcePrice) / float64(old.SourcePrice) * 100
		}
		changes = append(changes, row)
	}

	for _, p := range existing {
		if p.SKU == "" || p.Supplier != supplier || seen[p.SKU] {
			continue
		}
		d.Gone++
		if len(d.GoneItems) < kListLimit {
			d.GoneItems = append(d.GoneItems, DiffRow{
				SKU: p.SKU, Title: p.Title, Was: p.SourcePrice, Stock: p.Stock,
			})
		}
	}

	// Biggest movement first, in either direction: a 60% cut matters as much as
	// a 60% rise.
	slices.SortStableFunc(changes, func(a, b DiffRow) int {
		return cmp.Compare(math.Abs(b.Percent), math.Abs(a.Percent))
	})
	d.PriceChanges = changes[:min(kListLimit, len(changes))]
	return d
}
