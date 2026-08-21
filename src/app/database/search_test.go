package database

import "testing"

// SQLite folds case for ASCII only, so a catalogue written in Russian used to be
// invisible to a buyer typing in lower case: on a live shop "кастрюля" found 8
// products and "Кастрюля" found 521. Search compares through a Unicode-aware
// lower on both sides, and this is the test that says so.
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
		n, err := d.CountProducts(q, supplierAny)
		if err != nil {
			t.Fatal(err)
		}
		if n != 3 {
			t.Errorf("%q found %d, want 3", q, n)
		}
	}

	// An article is matched the same way, and a word that is not there is still
	// not there.
	if n, _ := d.CountProducts("чайник", supplierAny); n != 1 {
		t.Errorf("kettle: %d, want 1", n)
	}
	if n, _ := d.CountProducts("сковорода", supplierAny); n != 0 {
		t.Errorf("a word nobody wrote found %d", n)
	}
}

// A buyer types words, not a substring. One LIKE over the whole query matched
// their spacing and nothing else: on a live shop "кпб евро" found none of the
// products titled "КПБ Евро 4 предмета". Every word has to be there; their
// order is the buyer's business.
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
		n, err := d.CountProducts(c.q, supplierAny)
		if err != nil {
			t.Fatal(err)
		}
		if n != c.want {
			t.Errorf("%q found %d, want %d", c.q, n, c.want)
		}
	}
}
