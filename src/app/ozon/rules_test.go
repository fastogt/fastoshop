package ozon

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fastogt/fastoshop/app/database"
)

// A ladder taken apart from a reseller's script: on a cheap product a percentage
// doesn't cover the commission, so the margin is taken as a multiple.
var kLadder = []database.OzonPriceRule{
	{UpTo: 5000, Multiplier: 13},
	{UpTo: 10000, Multiplier: 8},
	{UpTo: 20000, Multiplier: 7},
	{UpTo: 50000, Multiplier: 5},
	{UpTo: 100000, Multiplier: 4},
	{UpTo: 0, Multiplier: 2.5},
}

func TestPriceLadderBands(t *testing.T) {
	rules := append([]database.OzonPriceRule(nil), kLadder...)
	cases := []struct{ shelf, want int64 }{
		{3000, 39000},    // 30 ₽ -> ×13
		{5000, 40000},    // the boundary belongs to the next band: ×8
		{15000, 105000},  // ×7
		{30000, 150000},  // ×5
		{70000, 280000},  // ×4
		{150000, 375000}, // and above: ×2.5
	}
	for _, c := range cases {
		if got := database.ApplyRule(rules, c.shelf); got != c.want {
			t.Errorf("цена %d: получили %d, ждали %d", c.shelf, got, c.want)
		}
	}
}

// Without the "and above" band expensive products would silently stay without
// a price, and that would look like "the ladder didn't work".
func TestPriceRulesRequireOpenBand(t *testing.T) {
	h, _, _ := publishTest(t)
	body, _ := json.Marshal(priceRulesRequest{Rules: []database.OzonPriceRule{
		{UpTo: 5000, Multiplier: 2},
	}})
	if w := do(t, h, "PUT", "/price/rules", string(body)); w.Code != http.StatusBadRequest {
		t.Fatalf("лестница без открытой полосы принята: %d", w.Code)
	}
	body, _ = json.Marshal(priceRulesRequest{Rules: []database.OzonPriceRule{
		{UpTo: 0, Multiplier: 0},
	}})
	if w := do(t, h, "PUT", "/price/rules", string(body)); w.Code != http.StatusBadRequest {
		t.Fatalf("нулевой множитель принят: %d", w.Code)
	}
}

func TestFillPricesByRules(t *testing.T) {
	h, d, _ := publishTest(t, "CHEAP", "PRICEY", "MINE")
	cheap := seedProduct(t, d, "CHEAP", 1)
	pricey := seedProduct(t, d, "PRICEY", 1)
	mine := seedProduct(t, d, "MINE", 1)
	for id, price := range map[int64]int64{cheap: 3000, pricey: 150000, mine: 30000} {
		p, _ := d.GetProduct(id)
		p.Price = price
		if err := d.UpdateProduct(p); err != nil {
			t.Fatal(err)
		}
	}
	body, _ := json.Marshal(publishRequest{ProductIDs: []int64{cheap, pricey, mine}})
	do(t, h, "POST", "/publish", string(body))
	// A price of one's own is already set — the ladder has no right to touch it.
	setPrice(t, d, mine, 111111)

	body, _ = json.Marshal(priceRulesRequest{Rules: kLadder})
	if w := do(t, h, "PUT", "/price/rules", string(body)); w.Code != http.StatusOK {
		t.Fatalf("сохранение лестницы: %d %s", w.Code, w.Body.String())
	}
	got := decode[fillPricesResponse](t, do(t, h, "POST", "/price/fill-by-rules", ""))
	if got.Filled != 2 {
		t.Fatalf("заполнено: %d, ждали 2", got.Filled)
	}
	if r := linkRow(t, d, cheap); r.Price != 39000 {
		t.Errorf("дешёвый: %d", r.Price)
	}
	if r := linkRow(t, d, pricey); r.Price != 375000 {
		t.Errorf("дорогой: %d", r.Price)
	}
	if r := linkRow(t, d, mine); r.Price != 111111 {
		t.Errorf("своя цена затёрта: %d", r.Price)
	}
}

// The order the bands are entered in must not affect the result.
func TestPriceRulesSortedOnSave(t *testing.T) {
	h, d, _ := publishTest(t)
	shuffled := []database.OzonPriceRule{
		{UpTo: 0, Multiplier: 2.5},
		{UpTo: 10000, Multiplier: 8},
		{UpTo: 5000, Multiplier: 13},
	}
	body, _ := json.Marshal(priceRulesRequest{Rules: shuffled})
	do(t, h, "PUT", "/price/rules", string(body))
	rules, err := d.OzonPriceRules()
	if err != nil {
		t.Fatal(err)
	}
	if rules[0].UpTo != 5000 || rules[1].UpTo != 10000 || rules[2].UpTo != 0 {
		t.Fatalf("порядок полос: %+v", rules)
	}
}
