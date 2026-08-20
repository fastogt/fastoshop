package database

import (
	"fmt"
	"slices"
	"strconv"
)

// A characteristic is two things, kept apart on purpose: the definition says
// what the field is, the value says what this product has. Together they would
// repeat the label, the type and the order on every one of twenty thousand rows.
//
//	definition: {"key":"ves","type":"number","label":"Вес, г"}
//	value:      {"ves":"1200"}
//
// The shape is fastometa's `MetaKeyDef` deliberately: the same contract in two
// of our services means one thing to learn, and that one has been carrying an
// operator-editable dictionary in production.

// Parameter value types. Deliberately the same three fastometa allows —
// "number" rather than "int", because a density of 200.5 g/m² is as real as a
// weight of 1200 g and both are one type to a client.
const (
	ParamString = "string"
	ParamNumber = "number"
	ParamBool   = "bool"
)

// ParamDef describes one characteristic.
type ParamDef struct {
	Key  string `json:"key"`
	Type string `json:"type"`
	// Options close the set when non-empty, and they are allowed on every type:
	// a numeric field restricted to 30/45/60 is a legitimate enum, not a string
	// one. They keep the order they were given — sorted alphabetically, an
	// ordinal scale reads as nonsense.
	Options []string `json:"options"`
	// Label is what a buyer reads instead of the raw key, unit included ("Вес,
	// г"). Empty means the client decides — a caption living in client code is
	// a caption nobody can change without a release.
	Label string `json:"label"`
}

// ParamValues is what a product carries: key to value, always as text. One
// storage form for every type — the definition says how to read it, and a
// number that made a round trip through JSON comes back the way it went in.
type ParamValues map[string]string

// Number reads a value the definition promised is a number.
func (v ParamValues) Number(key string) (float64, bool) {
	f, err := strconv.ParseFloat(v[key], 64)
	return f, err == nil
}

// Int is Number for the whole ones — a weight in grams, a length in
// millimetres. A fraction is refused rather than truncated: silently turning
// 1.5 into 1 is how a wrong number gets a second life.
func (v ParamValues) Int(key string) (int64, bool) {
	i, err := strconv.ParseInt(v[key], 10, 64)
	return i, err == nil
}

func (v ParamValues) Bool(key string) (bool, bool) {
	b, err := strconv.ParseBool(v[key])
	return b, err == nil
}

// ValidateParams is the one place a type stops being a comment and becomes a
// promise. Everything that reaches a product goes through it — an import, the
// admin, a model's draft — because the first delivery quote to read a weight of
// "около килограмма" is the one that finds out.
func ValidateParams(defs []ParamDef, values ParamValues) error {
	byKey := make(map[string]ParamDef, len(defs))
	for _, d := range defs {
		byKey[d.Key] = d
	}
	for key, raw := range values {
		def, known := byKey[key]
		if !known {
			// Neither dropped nor stored: dropping loses data nobody asked us to
			// lose, storing gives an undeclared field a permanent home.
			return fmt.Errorf("unknown parameter %q", key)
		}
		switch def.Type {
		case ParamString:
		case ParamNumber:
			if _, ok := values.Number(key); !ok {
				return fmt.Errorf("parameter %q: %q is not a number", key, raw)
			}
		case ParamBool:
			if _, ok := values.Bool(key); !ok {
				return fmt.Errorf("parameter %q: %q is not true or false", key, raw)
			}
		default:
			return fmt.Errorf("parameter %q: unknown type %q", key, def.Type)
		}
		if len(def.Options) > 0 && !slices.Contains(def.Options, raw) {
			return fmt.Errorf("parameter %q: %q is not one of %v", key, raw, def.Options)
		}
	}
	return nil
}
