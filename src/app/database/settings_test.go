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

// The counters reach the storefront only through settings, and a shop that
// already has data gets the columns by ALTER — so both the round-trip and the
// upgrade of an older database are checked here.
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
	// Repeat migration: an install that already has the columns must survive a
	// restart, not fail on the duplicate.
	if err := d.addSettingsColumns(); err != nil {
		t.Fatalf("second migration: %v", err)
	}
}

func TestSettingsColumnsAddedToOldDatabase(t *testing.T) {
	d, err := OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	if _, err := d.db.Exec(`DROP TABLE settings;
	CREATE TABLE settings (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		owner_email TEXT NOT NULL, password_hash TEXT NOT NULL,
		shop_name TEXT NOT NULL DEFAULT '', shop_phone TEXT NOT NULL DEFAULT '',
		smtp_host TEXT NOT NULL DEFAULT '', smtp_port INTEGER NOT NULL DEFAULT 465,
		smtp_user TEXT NOT NULL DEFAULT '', smtp_password TEXT NOT NULL DEFAULT '',
		currency TEXT NOT NULL DEFAULT 'RUB', logo TEXT NOT NULL DEFAULT '',
		lang TEXT NOT NULL DEFAULT 'ru', price_coefficient REAL NOT NULL DEFAULT 1,
		feed_url TEXT NOT NULL DEFAULT '', feed_supplier TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if err := d.addSettingsColumns(); err != nil {
		t.Fatal(err)
	}
	if err := d.CreateSettings(&Settings{OwnerEmail: "a@b.c"}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetSettings(); err != nil {
		t.Fatalf("old database still missing the columns: %v", err)
	}
}
