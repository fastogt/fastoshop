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
	// ImageBlobs are pictures that came inside the file itself (a photo pasted
	// into a price list cell). They have no URL to keep, so they are written to
	// our uploads at import time instead of being fetched later.
	ImageBlobs [][]byte
	// Category is a path from the root down, segments joined by "/". A source
	// with a tree (YML, Ozon, a price list cell written as "Textile > Bedroom")
	// fills every segment; one without (a WB subject) fills a single one.
	Category string
	// Gross weight in grams and packed size in millimetres, as the platform
	// stated them. nil means the source said nothing — the one thing a delivery
	// quote must tell apart from a zero. Marketplaces make both mandatory on a
	// card, which is why an import is the only way a catalogue of twenty
	// thousand ever gets weighed: nobody types that in by hand.
	WeightG  *int64
	LengthMM *int64
	WidthMM  *int64
	HeightMM *int64
	// Characteristics as the source stated them: colour, size, material. Weight
	// and dimensions are deliberately NOT in here — the core does arithmetic
	// with those, and two homes for one number is one home too many.
	Params database.ParamValues
}

// fill keeps what the product already has and takes the feed's value only when
// there is nothing to keep. The second return says whether anything changed, so
// a refresh that brings no news still writes nothing.
func fill(have, incoming *int64) (*int64, bool) {
	if have != nil || incoming == nil {
		return have, false
	}
	return incoming, true
}

// grams and millimetres convert a platform's own units into ours. An unknown
// unit returns nothing rather than a guess: a weight off by a factor of a
// thousand is worse than no weight at all, and it is silent.
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

// positive rounds and keeps a measurement only when it is one: zero is what a
// platform sends for "not filled in", and it must stay "not filled in" here.
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
	Count() (int, error) // the "Check" button: how many products were found
	Fetch() ([]Item, error)
}

// SourceErrors — a source may reject a card before it is ever written to the
// DB (e.g. a foreign currency in YML). Such losses must make it into Result,
// or the seller sees "20 migrated" without learning about the 3 lost.
type SourceErrors interface {
	FetchErrors() int
}

// SourceCurrency — the source knows which currency its prices came in. An empty
// string means "unknown" (our own CSV), where the owner fills the prices in
// themselves.
type SourceCurrency interface {
	Currency() string
}

// FeedCurrency answers the one question worth asking before an import: does the
// feed quote the same money the shop sells in. We hold no exchange rate and
// fetch none — it goes into the coefficient.
func FeedCurrency(src Source) string {
	if c, ok := src.(SourceCurrency); ok {
		return c.Currency()
	}
	return ""
}

type Result struct {
	Imported int `json:"imported"`
	// Updated counts products already in the shop whose supplier price or stock
	// moved; Skipped counts those the feed repeated unchanged plus rows the
	// import refused to create: no article, a duplicate article, a zero price,
	// or an article owned by another supplier group.
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
	// Zeroed counts products the feed no longer lists. They are not deleted:
	// ozon_links points at the product id and the slug is already indexed, so
	// recreating one later would break the channel link and a live URL.
	Zeroed int `json:"zeroed"`
	Errors int `json:"errors"`
}

// Stages travel to the admin as keys: the text around them is rendered in the
// owner's language on the screen, like everything else there.
const (
	StageFetch    = "fetch"
	StageProducts = "products"
)

var kHTTP = &http.Client{Timeout: 60 * time.Second}

// Run imports the catalogue, turning each source price into a shelf price with
// the owner's coefficient: exchange rate and import costs folded into one
// number. The source price is stored as it came, so changing the coefficient
// later recomputes exactly instead of rescaling a rescaled value.
// Run imports into one supplier group. The group, not the source, decides what
// this import may touch: a shop can live off two feeds plus its own goods, and
// one feed must never reprice or zero out another's.
// onProgress may be nil. It reports the stage and how far along it is, so the
// admin can show a bar instead of a spinner that says nothing for two minutes.
// uploadsDir is where pictures that came inside the source file are written; a
// source without them never touches it.
func Run(src Source, db *database.Database, supplier string, coefficient float64,
	uploadsDir string, onProgress func(stage string, done, total int)) (*Result, error) {
	progress := func(stage string, done, total int) {
		if onProgress != nil {
			onProgress(stage, done, total)
		}
	}
	// The shop's markup ladder is read once per import: it belongs to the shop,
	// not to the feed, and re-reading it per product would be twenty thousand
	// queries to answer the same question.
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
	for i, it := range items {
		progress(StageProducts, i, len(items))
		if it.SKU == "" || seen[it.SKU] {
			res.Skipped++
			continue
		}
		seen[it.SKU] = true
		if old, found := bySKU[it.SKU]; found {
			// An article owned by another supplier group is left untouched: two
			// suppliers claiming one article is the owner's call, not ours.
			if old.Supplier != supplier {
				res.Skipped++
				continue
			}
			changed, err := merge(db, old, it, coefficient, rules)
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
		// A zero price is refused: a product given away for free is worse than a
		// product missing.
		if it.Price <= 0 {
			res.Skipped++
			continue
		}
		p := &database.Product{SKU: it.SKU, Title: it.Title,
			Description: it.Description, Price: database.ShelfPrice(rules, it.Price, coefficient),
			SourcePrice: it.Price, Stock: max(it.Stock, 0),
			Category: it.Category, Supplier: supplier,
			WeightG: it.WeightG, LengthMM: it.LengthMM,
			WidthMM: it.WidthMM, HeightMM: it.HeightMM,
			Params: it.Params}
		if err := db.CreateProduct(p); err != nil {
			log.Warnf("import %s: create %q: %v", src.Name(), it.Title, err)
			res.Errors++
			continue
		}
		// The supplier's link is stored, not the file: 20 000 cards mean 60 000
		// downloads, and the import would take hours instead of a minute. The
		// storefront renders an absolute URL from product_images.path as happily
		// as a local name, and the owner pulls the photos onto our disk when they
		// want to, with "Download photos" in the products table.
		for _, u := range it.ImageURLs {
			_ = db.AddImage(p.ID, u)
		}
		// A picture from inside the file has no link to fall back on, so it is
		// written now or lost.
		for _, blob := range it.ImageBlobs {
			name, err := SaveBlob(p.ID, blob, uploadsDir)
			if err != nil {
				log.Warnf("import %s: image for %q: %v", src.Name(), it.Title, err)
				continue
			}
			_ = db.AddImage(p.ID, name)
		}
		res.Imported++
	}

	progress(StageProducts, len(items), len(items))

	// A feed that came back empty is a supplier's outage, not a decision to
	// withdraw the entire catalogue — zeroing everything on it would take the
	// shop off sale, and on Ozon too.
	if len(items) > 0 {
		zeroed, err := zeroMissing(db, existing, seen, supplier)
		if err != nil {
			return nil, err
		}
		res.Zeroed = zeroed
	}
	return res, nil
}

// merge updates what the source owns — its price and the stock — and leaves
// alone what the owner owns: the title, the description and the photos are
// their SEO work, and a weekly feed must not undo it. A price the owner typed
// keeps its manual mark and its value.
func merge(db *database.Database, old database.Product, it Item, coefficient float64,
	rules []database.PriceRule) (bool, error) {
	price := old.Price
	if !old.PriceManual {
		price = database.ShelfPrice(rules, it.Price, coefficient)
	}
	// A category the owner already set is theirs; an empty one gets filled, so a
	// catalogue imported before categories existed picks them up on the next run
	// instead of needing a wipe.
	category := old.Category
	if category == "" {
		category = it.Category
	}
	// Measurements follow the same rule as the category: an empty one is filled,
	// a stated one is left alone. A catalogue imported before this existed picks
	// up its weights on the next refresh, and an owner who corrected one keeps
	// the correction — the platform's number is a starting point, not a verdict.
	weight, filled := fill(old.WeightG, it.WeightG)
	length, l := fill(old.LengthMM, it.LengthMM)
	width, w := fill(old.WidthMM, it.WidthMM)
	height, hh := fill(old.HeightMM, it.HeightMM)
	measured := filled || l || w || hh
	// Characteristics follow the category: an empty set is filled, a set the
	// owner has touched is theirs. We do not merge key by key — a half-owned
	// set is a set nobody can reason about.
	params, gained := old.Params, false
	if len(params) == 0 && len(it.Params) > 0 {
		params, gained = it.Params, true
	}
	if old.SourcePrice == it.Price && old.Stock == max(it.Stock, 0) &&
		old.Price == price && old.Category == category && !measured && !gained {
		return false, nil
	}
	old.Params = params
	old.WeightG, old.LengthMM = weight, length
	old.WidthMM, old.HeightMM = width, height
	old.SourcePrice = it.Price
	old.Stock = max(it.Stock, 0)
	old.Price = price
	old.Category = category
	return true, db.UpdateProduct(&old)
}

// zeroMissing takes off sale what this group's feed stopped listing. Other
// groups and the owner's own goods are none of its business.
func zeroMissing(db *database.Database, existing []database.Product, seen map[string]bool, supplier string) (int, error) {
	n := 0
	for _, p := range existing {
		if p.SKU == "" || p.Supplier != supplier || seen[p.SKU] || p.Stock == 0 {
			continue
		}
		p.Stock = 0
		if err := db.UpdateProduct(&p); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
