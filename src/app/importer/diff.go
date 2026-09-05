package importer

import (
	"cmp"
	"math"
	"slices"

	"github.com/fastogt/fastoshop/app/database"
)

// kListLimit caps each list in the diff; the counts stay complete.
const kListLimit = 50

// Prices are in minor units: Was/Now are the supplier's, Shelf has the coefficient.
type DiffRow struct {
	SKU     string  `json:"sku"`
	Title   string  `json:"title"`
	Was     int64   `json:"was"`
	Now     int64   `json:"now"`
	Shelf   int64   `json:"shelf"`
	Percent float64 `json:"percent"`
	Stock   int     `json:"stock"`
}

// Diff is computed against products.source_price, not a snapshot of the last feed.
type Diff struct {
	// Currency the feed quotes in, empty when the source does not say.
	Currency string `json:"currency"`

	Total     int `json:"total"`
	New       int `json:"new"`
	Gone      int `json:"gone"`
	PriceUp   int `json:"price_up"`
	PriceDown int `json:"price_down"`
	Unchanged int `json:"unchanged"`
	// Conflicts are articles owned by another group; this import leaves them alone.
	Conflicts int `json:"conflicts"`
	// NoSKU are rows the feed sent without an article; they cannot be imported.
	NoSKU int `json:"no_sku"`

	NewItems     []DiffRow `json:"new_items"`
	GoneItems    []DiffRow `json:"gone_items"`
	PriceChanges []DiffRow `json:"price_changes"`
}

// Compare matches by article inside one supplier group; rows without one sit out.
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
					Shelf: database.ShelfPrice(rules, it.Price, coefficient), Stock: it.Stock,
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
			Shelf: database.ShelfPrice(rules, it.Price, coefficient), Stock: it.Stock,
		}
		// A zero source price would read as an infinite rise and top the list.
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

	// Biggest movement first, in either direction.
	slices.SortStableFunc(changes, func(a, b DiffRow) int {
		return cmp.Compare(math.Abs(b.Percent), math.Abs(a.Percent))
	})
	d.PriceChanges = changes[:min(kListLimit, len(changes))]
	return d
}
