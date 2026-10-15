package rtconfig

import (
	"errors"
	"math"
	"strconv"
	"testing"
	"time"
)

func TestValuePreservesExactIntegerRanges(t *testing.T) {
	tests := []struct {
		name string
		typ  Type
		raw  string
	}{
		{
			name: "signed above float precision",
			typ:  Int64Type,
			raw:  "9007199254740993",
		},
		{
			name: "minimum signed",
			typ:  Int64Type,
			raw:  "-9223372036854775808",
		},
		{
			name: "maximum signed",
			typ:  Int64Type,
			raw:  "9223372036854775807",
		},
		{
			name: "maximum unsigned",
			typ:  Uint64Type,
			raw:  "18446744073709551615",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := decodeValue(test.typ, []byte(test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if value.String() != test.raw || value.Err() != nil {
				t.Fatalf("value = %s, error = %v", value.String(), value.Err())
			}
			if test.typ == Int64Type {
				expected, err := strconv.ParseInt(test.raw, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				if value.Int64() != expected || value.Uint64() != 0 || value.Float64() != 0 {
					t.Fatal("signed value lost precision or converted to an unrelated type")
				}
				if strconv.IntSize == 32 && (expected > math.MaxInt32 || expected < math.MinInt32) && value.Int() != 0 {
					t.Fatal("overflowing Int must return zero")
				}
			} else {
				if value.Uint64() != math.MaxUint64 || value.Int64() != 0 || value.Float64() != 0 {
					t.Fatal("unsigned value lost precision or converted to an unrelated type")
				}
				if strconv.IntSize == 32 && value.Uint() != 0 {
					t.Fatal("overflowing Uint must return zero")
				}
			}
		})
	}
}

func TestValueExactGettersAndCanonicalRepresentation(t *testing.T) {
	tests := []struct {
		name string
		typ  Type
		raw  string
		text string
	}{
		{
			name: "boolean",
			typ:  BoolType,
			raw:  "true",
			text: "true",
		},
		{
			name: "string is not parsed as bool",
			typ:  StringType,
			raw:  `"true"`,
			text: "true",
		},
		{
			name: "duration is normalized",
			typ:  DurationType,
			raw:  `"1500ms"`,
			text: "1.5s",
		},
		{
			name: "float is normalized",
			typ:  Float64Type,
			raw:  "1.50",
			text: "1.5",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := decodeValue(test.typ, []byte(test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if value.String() != test.text || value.Type() != test.typ {
				t.Fatalf("value = %q (%q)", value.String(), value.Type())
			}
			if value.Bool() != (test.typ == BoolType) {
				t.Fatal("Bool must only read boolean values")
			}
			if test.typ == DurationType && value.Duration() != 1500*time.Millisecond {
				t.Fatal("duration was not parsed")
			}
			if test.typ != DurationType && value.Duration() != 0 {
				t.Fatal("Duration converted an unrelated type")
			}
			if test.typ == Float64Type && value.Float64() != 1.5 {
				t.Fatal("float was not parsed")
			}
			if test.typ != Float64Type && value.Float64() != 0 {
				t.Fatal("Float64 converted an unrelated type")
			}
			if value.Int() != 0 || value.Int64() != 0 || value.Uint() != 0 || value.Uint64() != 0 || value.Err() != nil {
				t.Fatal("wrong getter changed value or retrieval error")
			}
		})
	}
}

func TestNilValuePreservesErrorAndHasSafeGetters(t *testing.T) {
	cause := errors.New("missing test configuration")
	value := NewNilValue(cause)
	if !errors.Is(value.Err(), cause) {
		t.Fatal("retrieval error was lost")
	}
	if value.Bool() || value.String() != "" || value.Int() != 0 || value.Int64() != 0 || value.Uint() != 0 || value.Uint64() != 0 || value.Float64() != 0 || value.Duration() != 0 || value.Type() != "" {
		t.Fatal("error Value getters must return zero values")
	}
	if !errors.Is(value.Err(), cause) {
		t.Fatal("getter changed the retrieval error")
	}
}
