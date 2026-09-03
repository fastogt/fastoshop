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

// cardIndex maps an article to a card size.
//
// Our catalogue never carries a barcode: products come from an Excel price list,
// a YML feed or a marketplace export, and the only thing they share with a card
// is the seller's article. So matching goes through vendorCode, exactly like the
// Ozon slice matches offer_id - the barcode is then read off the card, because
// stock is set by it.
//
// A card with several sizes has several barcodes and cannot be resolved from one
// article: which size the product is meant to be is not knowable. Those are
// reported as ambiguous instead of guessed. The exception costs one lookup: a
// catalogue imported from Wildberries itself carries "vendorCode-<size>" in its
// article, because that is what the importer writes there.
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

// sizeLabel repeats what the importer writes into the article of a card split by
// size, so a catalogue imported from Wildberries links back to itself.
func sizeLabel(s Size) string {
	if s.TechSize != "" {
		return s.TechSize
	}
	if s.WBSize != "" {
		return s.WBSize
	}
	return strconv.FormatInt(s.ChrtID, 10)
}

// key normalises an article the way the two sides are allowed to differ: case
// and surrounding spaces, nothing else. Leading zeros are meaningful here - a
// vendorCode is whatever the seller typed into their own cabinet.
func key(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// lookup resolves one product's article. The second return value is the reason
// it did not match, as an i18n key, so the tab can say more than "not found".
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

// matchProducts resolves a batch of shop products against the cabinet and
// returns the links to store plus what could not be resolved.
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
