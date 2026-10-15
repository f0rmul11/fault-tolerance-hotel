package rtconfig

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseSchemaNormalizesScalarDefaults(t *testing.T) {
	schema, err := ParseSchema([]byte(`realtimeConfig:
  enabled:
    type: bool
    value: "false"
    writable: true
  count:
    type: int64
    value: "9007199254740993"
    writable: false
  timeout:
    type: duration
    value: 1500ms
    writable: true
  mode:
    usage: Режим работы
    group: features
    type: string
    value: full
    writable: true
    view:
      enum: [partial, full]
`))
	if err != nil {
		t.Fatal(err)
	}
	defaults := schema.defaults()
	if defaults["enabled"].Bool() || defaults["count"].Int64() != 9007199254740993 || defaults["timeout"].Duration() != 1500*time.Millisecond {
		t.Fatal("defaults were not normalized without loss")
	}
	definitions := schema.Definitions()
	if definitions[0].Default != int64(9007199254740993) || definitions[1].Default != false || definitions[3].Default != 1500*time.Millisecond {
		t.Fatalf("normalized definitions = %+v", definitions)
	}
	if err := schema.RequireType("enabled", BoolType); err != nil {
		t.Fatal(err)
	}
	if err := schema.RequireType("enabled", StringType); err == nil {
		t.Fatal("wrong consumer type must fail")
	}
	if err := schema.RequireType("missing", BoolType); !errors.Is(err, ErrVariableNotFound) {
		t.Fatalf("missing type check = %v", err)
	}
}

func TestParseSchemaRejectsAmbiguousOrInvalidYAML(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing writable",
			body: "realtimeConfig: {enabled: {type: bool, value: false}}",
		},
		{
			name: "writable typo",
			body: "realtimeConfig: {enabled: {type: bool, value: false, writeable: true}}",
		},
		{
			name: "quoted writable metadata",
			body: `realtimeConfig: {enabled: {type: bool, value: false, writable: "true"}}`,
		},
		{
			name: "missing default",
			body: "realtimeConfig: {enabled: {type: bool, writable: true}}",
		},
		{
			name: "null default",
			body: "realtimeConfig: {enabled: {type: bool, value: null, writable: true}}",
		},
		{
			name: "mapping default",
			body: "realtimeConfig: {enabled: {type: bool, value: {}, writable: true}}",
		},
		{
			name: "array default",
			body: "realtimeConfig: {enabled: {type: bool, value: [], writable: true}}",
		},
		{
			name: "numeric boolean",
			body: "realtimeConfig: {enabled: {type: bool, value: 1, writable: true}}",
		},
		{
			name: "boolean alias",
			body: "realtimeConfig: {enabled: {type: bool, value: yes, writable: true}}",
		},
		{
			name: "duplicate definition",
			body: "realtimeConfig:\n  enabled: {type: bool, value: false, writable: true}\n  enabled: {type: bool, value: true, writable: true}",
		},
		{
			name: "duplicate metadata",
			body: "realtimeConfig: {enabled: {type: bool, type: string, value: false, writable: true}}",
		},
		{
			name: "unknown metadata",
			body: "realtimeConfig: {enabled: {type: bool, value: false, writable: true, secret: 1}}",
		},
		{
			name: "unknown root",
			body: "realtimeConfig: {}\nother: true",
		},
		{
			name: "unknown view metadata",
			body: "realtimeConfig: {mode: {type: string, value: full, writable: true, view: {enmu: [full]}}}",
		},
		{
			name: "numeric enum",
			body: `realtimeConfig: {mode: {type: string, value: "1", writable: true, view: {enum: [1]}}}`,
		},
		{
			name: "empty enum",
			body: "realtimeConfig: {mode: {type: string, value: full, writable: true, view: {enum: []}}}",
		},
		{
			name: "empty view",
			body: "realtimeConfig: {mode: {type: string, value: full, writable: true, view: {}}}",
		},
		{
			name: "numeric string",
			body: "realtimeConfig: {mode: {type: string, value: 1, writable: true}}",
		},
		{
			name: "second document",
			body: "realtimeConfig: {}\n---\nrealtimeConfig: {}",
		},
		{
			name: "alias",
			body: "realtimeConfig: {a: &flag {type: bool, value: false, writable: true}, b: *flag}",
		},
		{
			name: "missing root",
			body: "{}",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseSchema([]byte(test.body)); err == nil {
				t.Fatal("invalid schema was accepted")
			}
		})
	}
}

func TestSchemaCandidateIsValidatedAsAWhole(t *testing.T) {
	schema, err := NewSchema(
		Definition{
			Name:     "enabled",
			Type:     BoolType,
			Default:  false,
			Writable: true,
		},
		Definition{
			Name:     "mode",
			Type:     StringType,
			Default:  "full",
			Writable: true,
			Enum:     []string{"full", "partial"},
		},
		Definition{
			Name:    "immutable",
			Type:    StringType,
			Default: "fixed",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	valid := map[string][]byte{
		"enabled": []byte("true"),
		"mode":    []byte(`"partial"`),
		"unknown": []byte("not JSON"),
	}
	values, err := schema.decode(valid)
	if err != nil {
		t.Fatal(err)
	}
	if !values["enabled"].Bool() || values["mode"].String() != "partial" || values["immutable"].String() != "fixed" || len(values) != 3 {
		t.Fatalf("unexpected candidate: %v", values)
	}
	valid["mode"] = []byte(`"invalid-private-value"`)
	if rejected, err := schema.decode(valid); err == nil || rejected != nil || strings.Contains(err.Error(), "invalid-private-value") {
		t.Fatalf("invalid candidate leaked values or partially succeeded: %v, %v", rejected, err)
	}
	if schema.defaults()["enabled"].Bool() || values["mode"].String() != "partial" {
		t.Fatal("candidate decoding changed existing immutable values")
	}
	if rejected, err := schema.decode(map[string][]byte{"immutable": []byte(`"fixed"`)}); err == nil || rejected != nil {
		t.Fatal("readonly override must fail, even when unchanged")
	}
	reset, err := schema.decode(nil)
	if err != nil || reset["enabled"].Bool() || reset["mode"].String() != "full" {
		t.Fatal("deleted keys did not return to defaults")
	}
}

func TestSchemaCopiesInputsAndHasStableContractHash(t *testing.T) {
	definitions := []Definition{
		{
			Name:     "mode",
			Type:     StringType,
			Default:  "full",
			Writable: true,
			Enum:     []string{"partial", "full"},
		},
		{
			Name:     "enabled",
			Type:     BoolType,
			Default:  false,
			Writable: true,
		},
	}
	schema, err := NewSchema(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	reordered, err := NewSchema(definitions[1], definitions[0])
	if err != nil {
		t.Fatal(err)
	}
	if schema.Hash() != reordered.Hash() {
		t.Fatal("definition order changed contract hash")
	}
	definitions[0].Enum[0] = "mutated"
	copied := schema.Definitions()
	copied[1].Enum[0] = "also_mutated"
	names := schema.Names()
	names[0] = "missing"
	defaults := schema.defaults()
	delete(defaults, "enabled")
	if _, err := schema.decode(map[string][]byte{"mode": []byte(`"full"`)}); err != nil {
		t.Fatal("input or returned collection mutated schema", err)
	}
	if schema.Names()[0] != "enabled" || len(schema.defaults()) != 2 {
		t.Fatal("caller changed private collections")
	}
	changed := schema.Definitions()
	changed[0].Default = true
	other, err := NewSchema(changed...)
	if err != nil {
		t.Fatal(err)
	}
	if schema.Hash() == other.Hash() {
		t.Fatal("default change must change contract hash")
	}
}

func TestSchemaRejectsInvalidDefinitionsAndBounds(t *testing.T) {
	tests := []struct {
		name       string
		definition Definition
	}{
		{
			name: "unknown type",
			definition: Definition{
				Name:    "valid",
				Type:    "object",
				Default: "value",
			},
		},
		{
			name: "mutable default",
			definition: Definition{
				Name:    "valid",
				Type:    StringType,
				Default: []string{"value"},
			},
		},
		{
			name: "nil default",
			definition: Definition{
				Name: "valid",
				Type: StringType,
			},
		},
		{
			name: "wrong enum type",
			definition: Definition{
				Name:    "valid",
				Type:    BoolType,
				Default: true,
				Enum:    []string{"true"},
			},
		},
		{
			name: "enum excludes default",
			definition: Definition{
				Name:    "valid",
				Type:    StringType,
				Default: "missing",
				Enum:    []string{"full"},
			},
		},
		{
			name: "duplicate enum",
			definition: Definition{
				Name:    "valid",
				Type:    StringType,
				Default: "full",
				Enum:    []string{"full", "full"},
			},
		},
		{
			name: "oversize enum option",
			definition: Definition{
				Name:    "valid",
				Type:    StringType,
				Default: "full",
				Enum:    []string{"full", strings.Repeat("a", maxValueBytes)},
			},
		},
		{
			name: "nonfinite default",
			definition: Definition{
				Name:    "valid",
				Type:    Float64Type,
				Default: math.Inf(1),
			},
		},
		{
			name: "oversize default",
			definition: Definition{
				Name:    "valid",
				Type:    StringType,
				Default: strings.Repeat("a", maxValueBytes),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSchema(test.definition); err == nil {
				t.Fatal("invalid definition accepted")
			}
		})
	}
	for _, name := range []string{"", "Upper", "1starts", "contains-dash", "has.dot", "key/child", "доступен", "ends_", "double__underscore", strings.Repeat("a", maxKeyBytes+1)} {
		t.Run("name="+name, func(t *testing.T) {
			if _, err := NewSchema(Definition{
				Name:    name,
				Type:    BoolType,
				Default: false,
			}); err == nil {
				t.Fatal("invalid key accepted")
			}
		})
	}
	definition := Definition{
		Name:    "enabled",
		Type:    BoolType,
		Default: false,
	}
	if _, err := NewSchema(definition, definition); err == nil {
		t.Fatal("duplicate key accepted")
	}
	definitions := make([]Definition, maxKeys+1)
	for index := range definitions {
		definitions[index] = Definition{
			Name:    fmt.Sprintf("key_%d", index),
			Type:    BoolType,
			Default: false,
		}
	}
	if _, err := NewSchema(definitions...); err == nil {
		t.Fatal("oversize catalog accepted")
	}
}

func TestWireValuesAreStrictAndBounded(t *testing.T) {
	tests := []struct {
		name string
		typ  Type
		raw  string
	}{
		{
			name: "quoted bool",
			typ:  BoolType,
			raw:  `"false"`,
		},
		{
			name: "null",
			typ:  BoolType,
			raw:  "null",
		},
		{
			name: "array",
			typ:  StringType,
			raw:  "[]",
		},
		{
			name: "object",
			typ:  StringType,
			raw:  "{}",
		},
		{
			name: "integer decimal",
			typ:  Int64Type,
			raw:  "1.0",
		},
		{
			name: "integer exponent",
			typ:  Int64Type,
			raw:  "1e2",
		},
		{
			name: "signed overflow",
			typ:  Int64Type,
			raw:  "9223372036854775808",
		},
		{
			name: "unsigned overflow",
			typ:  Uint64Type,
			raw:  "18446744073709551616",
		},
		{
			name: "negative unsigned",
			typ:  Uint64Type,
			raw:  "-1",
		},
		{
			name: "float overflow",
			typ:  Float64Type,
			raw:  "1e1000",
		},
		{
			name: "numeric duration",
			typ:  DurationType,
			raw:  "1000",
		},
		{
			name: "duration overflow",
			typ:  DurationType,
			raw:  `"10000000000h"`,
		},
		{
			name: "trailing scalar",
			typ:  BoolType,
			raw:  "true false",
		},
		{
			name: "invalid JSON",
			typ:  StringType,
			raw:  "sensitive-value-unquoted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := decodeValue(test.typ, []byte(test.raw))
			if err == nil || value != nil {
				t.Fatalf("invalid wire value accepted: %v", value)
			}
			if strings.Contains(err.Error(), test.raw) {
				t.Fatal("error exposes rejected value")
			}
		})
	}
	for _, typ := range []Type{IntType, UintType} {
		if strconv.IntSize != 32 {
			continue
		}
		if _, err := decodeValue(typ, []byte("4294967296")); err == nil {
			t.Fatal("machine integer overflow accepted")
		}
	}
	schema, err := NewSchema(Definition{
		Name:     "text",
		Type:     StringType,
		Default:  "",
		Writable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	boundary := `"` + strings.Repeat("a", maxValueBytes-2) + `"`
	if _, err := schema.decode(map[string][]byte{"text": []byte(boundary)}); err != nil {
		t.Fatal(err)
	}
	if _, err := schema.decode(map[string][]byte{"text": []byte(boundary + " ")}); err == nil {
		t.Fatal("raw wire size bound ignored")
	}
}
