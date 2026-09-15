package channel

import "testing"

func TestSuggestPackQty(t *testing.T) {
	for article, want := range map[string]int{
		"ШарнирыД8/4шт":            4,
		"ШарнирыД12/30шт.":         30,
		"ШарнирыТриоД10/2шт/":      2,
		"РучкаКрышкиБагажника2шт.": 2,
		"Шпалера_70х5шт":           5,
		"Банка x12":                12,
		"LABUBUжелт":               1,
		"Шпалера240":               1,
		"ОПОРЫ100/8/20_ЗЕЛ":        1,
		"Посейдон17см/бел":         1,
		"Box":                      1,
		"":                         1,
	} {
		if got := SuggestPackQty(article); got != want {
			t.Errorf("%q: %d, want %d", article, got, want)
		}
	}
}
