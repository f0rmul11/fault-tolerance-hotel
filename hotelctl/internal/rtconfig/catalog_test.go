package rtconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodeWireScalars(t *testing.T) {
	tests := []struct {
		typ   string
		input string
		want  string
	}{
		{
			typ:   "bool",
			input: "false",
			want:  "false",
		},
		{
			typ:   "string",
			input: "a\n\"b",
			want:  `"a\n\"b"`,
		},
		{
			typ:   "duration",
			input: "1500ms",
			want:  `"1.5s"`,
		},
		{
			typ:   "int64",
			input: "9223372036854775807",
			want:  "9223372036854775807",
		},
		{
			typ:   "uint64",
			input: "18446744073709551615",
			want:  "18446744073709551615",
		},
		{
			typ:   "int",
			input: "-42",
			want:  "-42",
		},
		{
			typ:   "uint",
			input: "42",
			want:  "42",
		},
		{
			typ:   "float64",
			input: "1e2",
			want:  "100",
		},
	}
	for _, tt := range tests {
		t.Run(tt.typ, func(t *testing.T) {
			t.Parallel()
			got, err := (Variable{Type: tt.typ}).Encode(tt.input)
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestEncodeRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		typ   string
		input string
	}{
		{
			typ:   "bool",
			input: "1",
		},
		{
			typ:   "bool",
			input: `"false"`,
		},
		{
			typ:   "int64",
			input: "9223372036854775808",
		},
		{
			typ:   "uint64",
			input: "18446744073709551616",
		},
		{
			typ:   "uint",
			input: "-1",
		},
		{
			typ:   "int",
			input: "1e2",
		},
		{
			typ:   "float64",
			input: "NaN",
		},
		{
			typ:   "float64",
			input: "+Inf",
		},
		{
			typ:   "duration",
			input: "tomorrow",
		},
		{
			typ:   "object",
			input: "{}",
		},
		{
			typ:   "string",
			input: strings.Repeat("x", 1025),
		},
		{
			typ:   "string",
			input: strings.Repeat("\n", 600), // JSON escapes count toward the wire limit.
		},
		{
			typ:   "string",
			input: string([]byte{0xff}),
		},
	}
	for _, tt := range tests {
		if _, err := (Variable{Type: tt.typ}).Encode(tt.input); err == nil {
			t.Fatalf("accepted invalid %s value", tt.typ)
		}
	}
}

func writeCatalog(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rtconfig.yaml")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadVariableDefaultsAndEnum(t *testing.T) {
	path := writeCatalog(t, `realtimeConfig:
  enabled: {type: bool, value: "false", writable: true}
  limit: {type: uint64, value: 18446744073709551615, writable: true}
  hex: {type: int, value: 0x10, writable: true}
  mode:
    type: string
    value: normal
    writable: false
    view:
      enum: [normal, degraded]
`)
	for name, want := range map[string]string{
		"enabled": "false",
		"limit":   "18446744073709551615",
		"hex":     "16",
		"mode":    `"normal"`,
	} {
		v, err := LoadVariable(path, name)
		if err != nil || v.Default != want {
			t.Fatalf("%s: default=%s error=%v", name, v.Default, err)
		}
	}
	v, err := LoadVariable(path, "mode")
	if err != nil {
		t.Fatal(err)
	}
	if v.Writable {
		t.Fatal("readonly flag lost")
	}
	if _, err := v.Encode("other"); err == nil {
		t.Fatal("value outside enum accepted")
	}
	if got, err := v.Encode("degraded"); err != nil || got != `"degraded"` {
		t.Fatalf("valid enum: %s %v", got, err)
	}
}

func TestLoadVariableRejectsInvalidCatalog(t *testing.T) {
	for name, data := range map[string]string{
		"missing":       `realtimeConfig: {other: {type: bool, value: true, writable: true}}`,
		"unknown_field": `realtimeConfig: {flag: {type: bool, value: true, writable: true, typo: 1}}`,
		"duplicate":     `realtimeConfig: {flag: {type: bool, type: string, value: true, writable: true}}`,
		"no_writable":   `realtimeConfig: {flag: {type: bool, value: true}}`,
		"null_default":  `realtimeConfig: {flag: {type: string, value: null, writable: true}}`,
		"type_mismatch": `realtimeConfig: {flag: {type: string, value: true, writable: true}}`,
		"enum_type":     `realtimeConfig: {flag: {type: bool, value: true, writable: true, view: {enum: [yes]}}}`,
		"enum_choices":  `realtimeConfig: {flag: {type: string, value: yes, writable: true, view: {enum: [1]}}}`,
		"enum_default":  `realtimeConfig: {flag: {type: string, value: invalid, writable: true, view: {enum: [valid]}}}`,
		"alias":         "realtimeConfig:\n  flag: &f {type: bool, value: true, writable: true}\n  other: *f\n",
		"documents":     "realtimeConfig: {}\n---\nrealtimeConfig: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := LoadVariable(writeCatalog(t, data), "flag"); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}
