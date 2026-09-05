package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// A live source against the parser, on demand:
//
//	FEED_URL=https://example.com/export.xml   go test ./app/importer -run Live -v
//	FEED_FILE=/path/to/price.xlsx             go test ./app/importer -run Live -v
//	WB_TOKEN=…                                go test ./app/importer -run Live -v
//	OZON_CLIENT_ID=… OZON_API_KEY=…           go test ./app/importer -run Live -v
//
// They assert almost nothing: only that something parsed and most of it survived.
func TestLiveYML(t *testing.T) {
	url := os.Getenv("FEED_URL")
	if url == "" {
		t.Skip("set FEED_URL to run against a live feed")
	}
	src := &YML{URL: url, DefaultStock: 1}
	items := fetchLive(t, src)
	t.Logf("currency: %q, rejected: %d", src.Currency(), src.FetchErrors())
	report(t, items)
}

// A price list off disk: a spreadsheet or a CSV, told apart by its own bytes.
func TestLiveFile(t *testing.T) {
	path := os.Getenv("FEED_FILE")
	if path == "" {
		t.Skip("set FEED_FILE to run against a local price list")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var src Source
	switch {
	case IsXLSX(raw):
		src = &XLSX{Data: raw}
	case IsYML(raw):
		src = &YML{Data: raw, DefaultStock: 1}
	default:
		src = &CSV{Data: raw}
	}
	t.Logf("%s, %d bytes", src.Name(), len(raw))
	items := fetchLive(t, src)
	if e, ok := src.(SourceErrors); ok {
		t.Logf("rejected rows: %d", e.FetchErrors())
	}
	report(t, items)
}

func TestLiveWB(t *testing.T) {
	token := os.Getenv("WB_TOKEN")
	if token == "" {
		t.Skip("set WB_TOKEN to run against a live cabinet")
	}
	report(t, fetchLive(t, &WB{Token: token}))
}

func TestLiveOzon(t *testing.T) {
	id, key := os.Getenv("OZON_CLIENT_ID"), os.Getenv("OZON_API_KEY")
	if id == "" || key == "" {
		t.Skip("set OZON_CLIENT_ID and OZON_API_KEY to run against a live cabinet")
	}
	report(t, fetchLive(t, &Ozon{ClientID: id, APIKey: key}))
}

func fetchLive(t *testing.T, src Source) []Item {
	t.Helper()
	items, err := src.Fetch()
	if err != nil {
		t.Fatalf("%s: %v", src.Name(), err)
	}
	if len(items) == 0 {
		t.Fatalf("%s: parsed into nothing", src.Name())
	}
	t.Logf("%s: %d items", src.Name(), len(items))
	return items
}

// report prints which fields a real catalogue fills and which come back empty.
func report(t *testing.T, items []Item) {
	t.Helper()
	filled := map[string]int{}
	count := func(name string, ok bool) {
		if ok {
			filled[name]++
		}
	}
	depth := map[int]int{}
	for _, it := range items {
		count("sku", it.SKU != "")
		count("title", it.Title != "")
		count("description", it.Description != "")
		count("price", it.Price > 0)
		count("stock", it.Stock > 0)
		count("category", it.Category != "")
		count("images", len(it.ImageURLs) > 0)
		count("weight", it.WeightG != nil)
		count("length", it.LengthMM != nil)
		count("width", it.WidthMM != nil)
		count("height", it.HeightMM != nil)
		count("params", len(it.Params) > 0)
		if it.Category != "" {
			depth[strings.Count(it.Category, database.CategorySep)+1]++
		}
	}
	names := make([]string, 0, len(filled))
	for k := range filled {
		names = append(names, k)
	}
	sort.Strings(names)
	t.Log("filled, of " + fmt.Sprint(len(items)) + ":")
	for _, n := range names {
		t.Logf("  %-12s %6d  %5.1f%%", n, filled[n],
			float64(filled[n])*100/float64(len(items)))
	}

	// How deep a source nests decides whether the storefront gets a tree.
	levels := make([]int, 0, len(depth))
	for d := range depth {
		levels = append(levels, d)
	}
	sort.Ints(levels)
	for _, d := range levels {
		t.Logf("  category depth %d: %d", d, depth[d])
	}

	// Which characteristics the source actually states, most common first.
	seen := map[string]int{}
	for _, it := range items {
		for _, p := range it.Params {
			seen[p.Name]++
		}
	}
	if len(seen) > 0 {
		type nc struct {
			name string
			n    int
		}
		list := make([]nc, 0, len(seen))
		for k, v := range seen {
			list = append(list, nc{k, v})
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].n != list[j].n {
				return list[i].n > list[j].n
			}
			return list[i].name < list[j].name
		})
		t.Logf("distinct characteristics: %d", len(list))
		for i, e := range list {
			if i == 25 {
				t.Logf("  …and %d more", len(list)-25)
				break
			}
			t.Logf("  %-40s %6d", e.name, e.n)
		}
	}

	// One whole item, as parsed: a count cannot say whether a value is usable.
	best := 0
	for i, it := range items {
		if len(it.Params) > len(items[best].Params) ||
			(len(it.Params) == len(items[best].Params) && len(it.ImageURLs) > len(items[best].ImageURLs)) {
			best = i
		}
	}
	raw, _ := json.MarshalIndent(items[best], "", "  ")
	t.Logf("fullest item:\n%s", raw)

	// The parser must not be quietly dropping most of the source.
	if filled["title"]*2 < len(items) || filled["price"]*2 < len(items) {
		t.Errorf("more than half the items have no title or no price: %+v", filled)
	}
}
