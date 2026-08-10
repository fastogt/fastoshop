package database

import "testing"

// Every currency the profile offers must be storable and have a sign: a shop
// saving "PLN" and then showing "₽" would misprice its whole catalogue.
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
