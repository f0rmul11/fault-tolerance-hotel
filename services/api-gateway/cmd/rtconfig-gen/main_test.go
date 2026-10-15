package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-gateway/internal/pkg/rtconfig"
)

func TestGenerateDeterministicAndCompatible(t *testing.T) {
	schema, err := mergedSchema([]byte(`realtimeConfig:
  hotels_list_enabled:
    usage: Разрешает получение списка отелей
    value: "false"
    type: bool
    writable: true
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := generate(schema)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generate(schema)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("generation not reproducible: %v", err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "config.go", first, parser.AllErrors); err != nil {
		t.Fatalf("generated invalid Go: %v", err)
	}
	for _, fragment := range []string{
		`HotelsListEnabled = configKey("hotels_list_enabled")`,
		`func GetValue(ctx context.Context, key configKey) rtconfig.Value`,
		`func NewClient(c rtconfig.Client) *Client`,
		`func (c *Client) GetValue(ctx context.Context, key configKey) rtconfig.Value`,
		schema.Hash(),
	} {
		if !bytes.Contains(first, []byte(fragment)) {
			t.Errorf("missing consumer contract %q", fragment)
		}
	}
}

func TestGenerateRejectsGoNameCollision(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{
			name: "normalized_collision",
			keys: []string{"http_id", "http_i_d"},
		},
		{
			name: "facade_name",
			keys: []string{"client"},
		},
		{
			name: "accessor_name",
			keys: []string{"get_value"},
		},
		{
			name: "schema_hash_name",
			keys: []string{"schema_hash"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var definitions []rtconfig.Definition
			for _, key := range tt.keys {
				definitions = append(definitions, rtconfig.Definition{
					Name:    key,
					Type:    rtconfig.BoolType,
					Default: false,
				})
			}
			schema, err := rtconfig.NewSchema(definitions...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := generate(schema); err == nil {
				t.Fatal("Go name collision accepted")
			}
		})
	}
}

func TestMergeRejectsServicePlatformCollision(t *testing.T) {
	data := []byte(`realtimeConfig:
  debug:
    value: false
    type: bool
    writable: true
`)
	platform := []rtconfig.Definition{
		{
			Name:    "debug",
			Type:    rtconfig.BoolType,
			Default: false,
		},
	}
	if _, err := mergedSchema(data, platform); err == nil {
		t.Fatal("service silently replaced platform key")
	}
}

func TestInvalidInputPreservesGeneratedFile(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "unknown_field",
			yaml: "realtimeConfig:\n  debug:\n    type: bool\n    value: false\n    writeable: true\n",
		},
		{
			name: "duplicate_key",
			yaml: "realtimeConfig:\n  debug: {type: bool, value: false, writable: true}\n  debug: {type: bool, value: true, writable: true}\n",
		},
		{
			name: "wrong_default",
			yaml: "realtimeConfig:\n  debug: {type: bool, value: yes, writable: true}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			schemaPath := filepath.Join(directory, "values.yaml")
			outputPath := filepath.Join(directory, "config.go")
			if err := os.WriteFile(schemaPath, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(outputPath, []byte("existing artifact"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := run([]string{"-schema", schemaPath, "-out", outputPath}, io.Discard); err == nil {
				t.Fatal("invalid schema accepted")
			}
			output, err := os.ReadFile(outputPath)
			if err != nil || string(output) != "existing artifact" {
				t.Fatalf("invalid input overwrote artifact: %q, %v", output, err)
			}
		})
	}
}

func TestGenerateKeepsUsageAsComment(t *testing.T) {
	schema, err := rtconfig.NewSchema(rtconfig.Definition{
		Name:    "debug",
		Type:    rtconfig.BoolType,
		Default: false,
		Usage:   "First line\npackage other\n/* not code */",
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := generate(schema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "// package other") {
		t.Fatalf("multiline usage escaped comment: %s", generated)
	}
}
