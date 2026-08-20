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
