package database

import "testing"

// The channel tab's four states come from two places at once: our link table
// knows what is linked, and only the platform knows what has a card. The filter
// is how that second half reaches a query which cannot see it - the tab passes
// the ids it learned when it opened. So what has to hold is that the id lists
// and the linked flag combine, and that the count agrees with the list: a table
// that counts 24 000 while listing 7 grows pages that turn out empty.
func TestCandidateFilter(t *testing.T) {
	d := openTest(t)
	ids := map[string]int64{}
	for _, sku := range []string{"a1", "a2", "a3", "a4"} {
		p := &Product{SKU: sku, Title: "товар " + sku}
		if err := d.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
		ids[sku] = p.ID
	}
	// a1 is linked; a2 and a3 have a card on the platform; a4 has nothing.
	if err := d.UpsertOzonLink(&OzonLink{ProductID: ids["a1"], OfferID: "a1"}); err != nil {
		t.Fatal(err)
	}
	ready := []int64{ids["a2"], ids["a3"]}
	yes, no := true, false

	for _, c := range []struct {
		name string
		f    CandidateFilter
		want int
	}{
		{"без фильтра - весь каталог", CandidateFilter{}, 4},
		{"можно связать", CandidateFilter{IDs: ready}, 2},
		{"связано", CandidateFilter{Linked: &yes}, 1},
		{"нет карточки", CandidateFilter{Linked: &no, ExcludeIDs: ready}, 1},
		{"поиск сужает состояние", CandidateFilter{IDs: ready, Q: "a2"}, 1},
	} {
		n, err := d.CountOzonCandidates(c.f)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if n != c.want {
			t.Errorf("%s: насчитано %d, ожидалось %d", c.name, n, c.want)
		}
		list, err := d.ListOzonCandidates(c.f, 100, 0)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(list) != n {
			t.Errorf("%s: строк %d, а счётчик обещал %d", c.name, len(list), n)
		}
	}
}

// Wildberries keeps its own link table, and the tab is the same tab. A filter
// that works for one channel and quietly ignores the other is worse than none:
// the owner would trust a number that means something else.
func TestCandidateFilterWB(t *testing.T) {
	d := openTest(t)
	var linked, plain int64
	for _, sku := range []string{"w1", "w2"} {
		p := &Product{SKU: sku, Title: "товар " + sku}
		if err := d.CreateProduct(p); err != nil {
			t.Fatal(err)
		}
		if sku == "w1" {
			linked = p.ID
		} else {
			plain = p.ID
		}
	}
	if err := d.UpsertWBLink(&WBLink{ProductID: linked, Barcode: "brc", NmID: 1}); err != nil {
		t.Fatal(err)
	}
	yes := true
	n, err := d.CountWBCandidates(CandidateFilter{Linked: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("связанных %d, ожидалась одна", n)
	}
	n, err = d.CountWBCandidates(CandidateFilter{IDs: []int64{plain}})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("по списку id вернулось %d, ожидался один", n)
	}
}
