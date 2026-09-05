package database

import "testing"

// SQLite folds case for ASCII only: Cyrillic needs a Unicode-aware lower on both sides.
func TestSearchIgnoresCyrillicCase(t *testing.T) {
	d, err := OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	for _, title := range []string{
		"Кастрюля эмалированная 3 л",
		"кастрюля алюминиевая 2 л",
		"КАСТРЮЛЯ чугунная",
		"Чайник заварочный",
	} {
		if err := d.CreateProduct(&Product{Title: title, Price: 100}); err != nil {
			t.Fatal(err)
		}
	}

	for _, q := range []string{"кастрюля", "Кастрюля", "КАСТРЮЛЯ", "кАсТрЮлЯ"} {
		n, err := d.CountProducts(q, AnySupplier)
		if err != nil {
			t.Fatal(err)
		}
		if n != 3 {
			t.Errorf("%q found %d, want 3", q, n)
		}
	}

	if n, _ := d.CountProducts("чайник", AnySupplier); n != 1 {
		t.Errorf("kettle: %d, want 1", n)
	}
	if n, _ := d.CountProducts("сковорода", AnySupplier); n != 0 {
		t.Errorf("a word nobody wrote found %d", n)
	}
}

// A buyer types words, not a substring: every word must match, in any order.
func TestSearchMatchesEveryWordInAnyOrder(t *testing.T) {
	d, err := OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	for _, title := range []string{
		"КПБ Евро 4 предмета сатин",
		"КПБ 1,5 спальный бязь",
		"Кастрюля эмалированная 3 л с крышкой",
		"Кастрюля алюминиевая 5 л",
	} {
		if err := d.CreateProduct(&Product{Title: title, Price: 100}); err != nil {
			t.Fatal(err)
		}
	}

	for _, c := range []struct {
		q    string
		want int
	}{
		{"кпб евро", 1},        // the case that found nothing at all
		{"евро кпб", 1},        // the buyer's order is not ours to insist on
		{"кастрюля 3 л", 1},    // words separated by other words
		{"кпб", 2},             // one word still behaves as it did
		{"кастрюля крышка", 0}, // every word must be there, not just one
		{"  кпб   евро  ", 1},  // stray spaces are not empty words
	} {
		n, err := d.CountProducts(c.q, AnySupplier)
		if err != nil {
			t.Fatal(err)
		}
		if n != c.want {
			t.Errorf("%q found %d, want %d", c.q, n, c.want)
		}
	}
}
