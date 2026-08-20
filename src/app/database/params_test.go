package database

import "testing"

var testDefs = []ParamDef{
	{Key: "ves", Type: ParamNumber, Label: "Вес, г"},
	{Key: "cvet", Type: ParamString, Label: "Цвет",
		Options: []string{"белый", "чёрный"}},
	{Key: "posudomoika", Type: ParamBool, Label: "Можно в посудомойку"},
	// A numeric field restricted to a list — an enum that is not a string.
	{Key: "obem", Type: ParamNumber, Label: "Объём, л",
		Options: []string{"1", "1.5", "2"}},
}

// The type in a definition is a promise, and this is where it is kept: a weight
// stored as words is what makes a delivery quote wrong months later, when
// nobody remembers who wrote it.
func TestValidateParamsCatchesTypes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values ParamValues
		ok     bool
	}{
		{"all correct", ParamValues{"ves": "1200", "cvet": "белый", "posudomoika": "true"}, true},
		{"a fractional number is still a number", ParamValues{"ves": "200.5"}, true},
		{"weight in words", ParamValues{"ves": "около килограмма"}, false},
		{"colour outside the options", ParamValues{"cvet": "малиновый"}, false},
		{"boolean as a word", ParamValues{"posudomoika": "да"}, false},
		{"numeric enum, allowed value", ParamValues{"obem": "1.5"}, true},
		{"numeric enum, a number outside the list", ParamValues{"obem": "3"}, false},
		{"a field nobody declared", ParamValues{"vysota": "10"}, false},
		{"nothing at all", ParamValues{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateParams(testDefs, tc.values)
			if tc.ok && err != nil {
				t.Fatalf("refused a valid set: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("accepted what it should have refused")
			}
		})
	}
}

// Reading back is where the text becomes a number again. Int refuses a fraction
// rather than truncating it: turning 1.5 into 1 quietly is how a wrong number
// gets a second life.
func TestParamValuesRead(t *testing.T) {
	v := ParamValues{"ves": "1200", "plotnost": "200.5", "flag": "true"}
	if g, ok := v.Int("ves"); !ok || g != 1200 {
		t.Errorf("weight: %v %t", g, ok)
	}
	if _, ok := v.Int("plotnost"); ok {
		t.Error("Int accepted a fraction")
	}
	if f, ok := v.Number("plotnost"); !ok || f != 200.5 {
		t.Errorf("density: %v %t", f, ok)
	}
	if b, ok := v.Bool("flag"); !ok || !b {
		t.Errorf("flag: %v %t", b, ok)
	}
	if _, ok := v.Number("nothing"); ok {
		t.Error("a missing key read as a number")
	}
}

// Characteristics travel with the product: an empty set is stored as an object
// rather than as NULL or the string "null", and a product that was never given
// any reads back as empty rather than nil — a caller must not have to check.
func TestParamsRoundTrip(t *testing.T) {
	d, err := OpenInMemory()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()

	bare := &Product{Title: "Без характеристик", Price: 100}
	if err := d.CreateProduct(bare); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetProduct(bare.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Params == nil || len(got.Params) != 0 {
		t.Errorf("empty characteristics came back as %#v", got.Params)
	}

	got.Params = ParamValues{"cvet": "белый", "obem": "1.5"}
	if err := d.UpdateProduct(got); err != nil {
		t.Fatal(err)
	}
	back, err := d.GetProduct(bare.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Params["cvet"] != "белый" {
		t.Errorf("colour came back as %q", back.Params["cvet"])
	}
	// A number stored as text is still a number on the way out.
	if v, ok := back.Params.Number("obem"); !ok || v != 1.5 {
		t.Errorf("volume came back as %v (%t)", v, ok)
	}
}
