package channel

import (
	"regexp"
	"strconv"
)

// MaxPackQty caps a pack size: a typo of 40000 would hide a card at zero stock forever.
const MaxPackQty = 10000

// A pack size is written next to "шт" or after an x; a bare number is a length or a model.
var packQtyRe = regexp.MustCompile(`(?i)(\d{1,5})\s*шт|(?:^|[^\p{L}])[xх×]\s*(\d{1,5})(?:\D|$)`)

// SuggestPackQty reads a pack size out of a seller's article: "Шарниры/4шт" is 4.
func SuggestPackQty(article string) int {
	m := packQtyRe.FindStringSubmatch(article)
	if m == nil {
		return 1
	}
	raw := m[1]
	if raw == "" {
		raw = m[2]
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > MaxPackQty {
		return 1
	}
	return n
}

// ValidPackQty is the check behind a hand-made link.
func ValidPackQty(n int64) bool { return n >= 1 && n <= MaxPackQty }
