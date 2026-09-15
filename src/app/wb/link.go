package wb

import (
	"strconv"
	"strings"

	"github.com/fastogt/fastoshop/app/database"
	"github.com/fastogt/fastoshop/app/i18n"
)

// match is a card size resolved to the two keys a link needs.
type match struct {
	NmID       int64
	Barcode    string
	VendorCode string
}

// Articles match by vendorCode, the barcode comes off the card; multi-size is ambiguous.
type cardIndex struct {
	single    map[string]match // vendorCode of a one-size card
	sized     map[string]match // vendorCode-<size label>
	ambiguous map[string]bool  // vendorCode of a multi-size card
}

func newCardIndex(cards []Card) *cardIndex {
	idx := &cardIndex{
		single:    map[string]match{},
		sized:     map[string]match{},
		ambiguous: map[string]bool{},
	}
	for _, c := range cards {
		multi := len(c.Sizes) > 1
		if multi {
			idx.ambiguous[key(c.VendorCode)] = true
		}
		for _, s := range c.Sizes {
			if len(s.Skus) == 0 {
				continue
			}
			m := match{NmID: c.NmID, Barcode: s.Skus[0], VendorCode: c.VendorCode}
			if !multi {
				idx.single[key(c.VendorCode)] = m
				continue
			}
			idx.sized[key(c.VendorCode+"-"+sizeLabel(s))] = m
		}
	}
	return idx
}

// Repeats what the importer writes into the article of a card split by size.
func sizeLabel(s Size) string {
	if s.TechSize != "" {
		return s.TechSize
	}
	if s.WBSize != "" {
		return s.WBSize
	}
	return strconv.FormatInt(s.ChrtID, 10)
}

// Only case and surrounding spaces may differ; leading zeros are meaningful.
func key(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// The second return value is the reason it did not match, as an i18n key.
func (idx *cardIndex) lookup(sku string) (match, string, bool) {
	k := key(sku)
	if m, ok := idx.single[k]; ok {
		return m, "", true
	}
	if m, ok := idx.sized[k]; ok {
		return m, "", true
	}
	if idx.ambiguous[k] {
		return match{}, i18n.KeyWBAmbiguousCard, false
	}
	return match{}, "", false
}

// Returns the links to store plus what could not be resolved.
func matchProducts(products []database.Product, idx *cardIndex) ([]database.WBLink, []unlinkedProduct) {
	var links []database.WBLink
	var missing []unlinkedProduct
	for _, p := range products {
		m, reason, ok := idx.lookup(p.SKU)
		if !ok {
			missing = append(missing, unlinkedProduct{
				ProductID: p.ID, SKU: p.SKU, Title: p.Title, Reason: reason,
			})
			continue
		}
		links = append(links, database.WBLink{
			ProductID: p.ID, NmID: m.NmID, Barcode: m.Barcode, VendorCode: m.VendorCode,
		})
	}
	return links, missing
}
