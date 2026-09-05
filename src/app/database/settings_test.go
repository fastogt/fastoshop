package database

import "testing"

// Every currency the profile offers must be storable and have a sign of its own.
func TestShopCurrencies(t *testing.T) {
	d, _ := OpenInMemory()
	defer func() { _ = d.Close() }()
	if err := d.CreateSettings(&Settings{OwnerEmail: "a@b.c"}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"RUB": "₽", "BYN": "Br", "PLN": "zł", "KZT": "₸"}
	for code, sign := range want {
		if !IsValidShopCurrency(code) {
			t.Fatalf("%s rejected", code)
		}
		s, _ := d.GetSettings()
		s.Currency = code
		if err := d.UpdateSettings(s); err != nil {
			t.Fatalf("%s: %v", code, err)
		}
		got, _ := d.GetSettings()
		if got.Currency != code || got.Sign() != sign {
			t.Fatalf("%s stored as %q with sign %q", code, got.Currency, got.Sign())
		}
	}
	if IsValidShopCurrency("USD") {
		t.Fatal("USD accepted without a sign to render it")
	}
}

func TestSEOSettings(t *testing.T) {
	d, _ := OpenInMemory()
	defer func() { _ = d.Close() }()
	if err := d.CreateSettings(&Settings{OwnerEmail: "a@b.c"}); err != nil {
		t.Fatal(err)
	}
	s, _ := d.GetSettings()
	s.GAMeasurementID = "G-ABC123"
	s.MetrikaCounterID = "12345678"
	if err := d.UpdateSettings(s); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetSettings()
	if got.GAMeasurementID != "G-ABC123" || got.MetrikaCounterID != "12345678" {
		t.Fatalf("counters lost on round-trip: %+v", got)
	}
}
