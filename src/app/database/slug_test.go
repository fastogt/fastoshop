package database

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Красный чайник":      "krasnyj-chajnik",
		"iPhone 15 Pro Max":   "iphone-15-pro-max",
		"Щётка (жёсткая)!":    "schyotka-zhyostkaya",
		"  пробелы   вокруг ": "probely-vokrug",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}
