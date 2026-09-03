package storefront

import (
	"strings"
	"testing"
)

// A quarter of a real imported catalogue carries a title past the limit search
// engines accept in structured data, and one such field costs the whole page
// its rich result. The visible heading keeps the full title, so what goes into
// the markup has to stay a prefix of it.
func TestClipName(t *testing.T) {
	short := "Чайник эмалированный 2 л"
	if got := clipName(short); got != short {
		t.Fatalf("a name within the limit must be returned as is, got %q", got)
	}

	long := "Постельное белье, комплект 1,5 спальный \"Розовый\" 4 предмета: " +
		"пододеяльник 145х215см, простыня 150х220см, 2 наволочки 70х70см с " +
		"клапаном-запахом, бязь \"Комфорт\" гладкое крашение, 100% хлопок"
	got := clipName(long)
	if r := []rune(got); len(r) > kMaxSchemaName {
		t.Fatalf("clipped to %d runes, want at most %d", len(r), kMaxSchemaName)
	}
	if !strings.HasPrefix(long, got) {
		t.Fatalf("clipped name is not a prefix of the visible title: %q", got)
	}
	if last := got[len(got)-1]; last == ' ' || last == ',' {
		t.Fatalf("clipped name ends on punctuation: %q", got)
	}

	noSpace := strings.Repeat("я", 200)
	if r := []rune(clipName(noSpace)); len(r) != kMaxSchemaName {
		t.Fatalf("a name with no space must still be cut, got %d runes", len(r))
	}
}
