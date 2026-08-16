package database

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

// PriceRule is one band of a channel's markup ladder: everything below UpTo (in
// kopecks) is multiplied by Multiplier. UpTo 0 is the open-ended top band.
//
// The ladder itself is not channel knowledge — every channel stores its bands in
// its own table, but the arithmetic and its validation are one thing, and having
// two copies of it means the second one is wrong the day the first is fixed.
type PriceRule struct {
	UpTo       int64   `json:"up_to"`
	Multiplier float64 `json:"multiplier"`
}

// kMaxMultiplier is the same kind of guard as the import coefficient: a typo
// must not send a catalogue to the platform at a hundred times its price.
const kMaxMultiplier = 100

func ValidPriceRules(rules []PriceRule) error {
	if len(rules) == 0 {
		return nil
	}
	open := 0
	for _, r := range rules {
		if r.Multiplier <= 0 || r.Multiplier > kMaxMultiplier {
			return fmt.Errorf("invalid multiplier: %v", r.Multiplier)
		}
		if r.UpTo < 0 {
			return fmt.Errorf("invalid band bound: %d", r.UpTo)
		}
		if r.UpTo == 0 {
			open++
		}
	}
	// Without an open-ended band the most expensive goods would silently get no
	// price at all, which looks exactly like "the ladder did not work".
	if open != 1 {
		return fmt.Errorf("exactly one open-ended band is required, got %d", open)
	}
	return nil
}

// sortRules puts the bands in ascending order with the open-ended one last, so
// the first match is the right one regardless of how the owner typed them in.
func sortRules(rules []PriceRule) {
	slices.SortStableFunc(rules, func(a, b PriceRule) int {
		if a.UpTo == 0 {
			return 1
		}
		if b.UpTo == 0 {
			return -1
		}
		return cmp.Compare(a.UpTo, b.UpTo)
	})
}

// ApplyRule returns the platform price for a shelf price, or 0 when no band
// matches — the caller must not invent a price the ladder does not define.
func ApplyRule(rules []PriceRule, shelf int64) int64 {
	for _, r := range rules {
		if r.UpTo == 0 || shelf < r.UpTo {
			return int64(math.Round(float64(shelf) * r.Multiplier))
		}
	}
	return 0
}
