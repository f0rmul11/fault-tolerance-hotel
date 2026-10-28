package codegen

import (
	"bytes"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hotels/internal/pkg/rtconfig"
)

func TestGenerateDeterministicAndCompatible(t *testing.T) {
	schema, err := mergedSchema([]byte(`realtimeConfig:
  rooms_list_enabled:
    usage: Разрешает получение списка отелей
    value: "false"
    type: bool
    writable: true
`), nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := render(schema)
	if err != nil {
		t.Fatal(err)
	}
	second, err := render(schema)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("generation not reproducible: %v", err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "config.go", first, parser.AllErrors); err != nil {
		t.Fatalf("generated invalid Go: %v", err)
	}
	for _, fragment := range []string{
		`RoomsListEnabled = configKey("rooms_list_enabled")`,
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
			if _, err := render(schema); err == nil {
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
			name: "go_identifier_collision",
			yaml: "realtimeConfig:\n  client: {type: bool, value: false, writable: true}\n",
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
			generator, err := New(Options{
				SchemaPath: schemaPath,
				OutputPath: outputPath,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := generator.Generate(); err == nil {
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
	generated, err := render(schema)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generated), "// package other") {
		t.Fatalf("multiline usage escaped comment: %s", generated)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{
			name: "missing_schema",
			opts: Options{OutputPath: "config.go"},
		},
		{
			name: "missing_output",
			opts: Options{SchemaPath: "values.yaml"},
		},
		{
			name: "same_path",
			opts: Options{
				SchemaPath: "values.yaml",
				OutputPath: filepath.Join("unused", "..", "values.yaml"),
			},
		},
		{
			name: "invalid_system_definition",
			opts: Options{
				SchemaPath: "values.yaml",
				OutputPath: "config.go",
				SystemDefinitions: []rtconfig.Definition{
					{
						Name:    "debug",
						Type:    rtconfig.BoolType,
						Default: "invalid bool",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(tt.opts); err == nil {
				t.Fatal("invalid options accepted")
			}
		})
	}
}

func TestGeneratorResolvesPathsAndCopiesSystemDefinitions(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	definitions := []rtconfig.Definition{
		{
			Name:    "log_level",
			Type:    rtconfig.StringType,
			Default: "info",
			Enum:    []string{"info", "debug"},
		},
	}
	data := []byte("realtimeConfig:\n  enabled: {type: bool, value: false, writable: true}\n")
	schema, err := mergedSchema(data, definitions)
	if err != nil {
		t.Fatal(err)
	}
	generator, err := New(Options{
		SchemaPath:        "values.yaml",
		OutputPath:        "generated/config.go",
		SystemDefinitions: definitions,
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 0 {
		t.Fatalf("constructor created files: %v, %v", entries, err)
	}
	// Конструктор сохраняет собственный каталог, включая вложенные enum slices.
	definitions[0].Name = "changed_name"
	definitions[0].Enum[0] = "changed_value"
	if err := os.WriteFile("values.yaml", data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Смена cwd после New не должна перенаправить ввод или вывод.
	t.Chdir(t.TempDir())
	if err := generator.Generate(); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(directory, "generated", "config.go")
	first, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`LogLevel = configKey("log_level")`,
		`Enabled = configKey("enabled")`,
		schema.Hash(),
	} {
		if !strings.Contains(strings.Join(strings.Fields(string(first)), " "), fragment) {
			t.Errorf("missing merged contract %q", fragment)
		}
	}
	if err := generator.Generate(); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(outputPath)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("generation not reproducible: %v", err)
	}
}
