// Package codegen создаёт Go facade runtime-конфигурации из YAML-каталога.
package codegen

import (
	"fmt"
	"os"
	"path/filepath"

	"api-gateway/internal/pkg/rtconfig"
)

// Options задаёт входной каталог и расположение generated facade пакета config.
// Относительные пути разрешаются от рабочей директории при вызове New.
type Options struct {
	// SchemaPath — обязательный путь к сервисному YAML с realtimeConfig.
	SchemaPath string
	// OutputPath — обязательный путь к генерируемому Go-файлу.
	OutputPath string
	// SystemDefinitions дополняет YAML платформенными ключами; может быть пустым.
	// Одноимённые сервисные и системные ключи считаются ошибкой.
	SystemDefinitions []rtconfig.Definition
}

// Generator хранит параметры генерации. Создавайте его через New.
type Generator struct {
	schemaPath        string
	outputPath        string
	systemDefinitions []rtconfig.Definition
}

// New проверяет параметры и копирует системный каталог, не читая и не создавая файлы.
func New(opts Options) (*Generator, error) {
	if opts.SchemaPath == "" {
		return nil, fmt.Errorf("codegen: SchemaPath is required")
	}
	if opts.OutputPath == "" {
		return nil, fmt.Errorf("codegen: OutputPath is required")
	}
	schemaPath, err := filepath.Abs(opts.SchemaPath)
	if err != nil {
		return nil, fmt.Errorf("codegen: resolve schema path: %w", err)
	}
	outputPath, err := filepath.Abs(opts.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("codegen: resolve output path: %w", err)
	}
	if schemaPath == outputPath {
		return nil, fmt.Errorf("codegen: schema and output paths must differ")
	}
	system, err := rtconfig.NewSchema(opts.SystemDefinitions...)
	if err != nil {
		return nil, fmt.Errorf("codegen: invalid system definitions: %w", err)
	}

	return &Generator{
		schemaPath:        schemaPath,
		outputPath:        outputPath,
		systemDefinitions: system.Definitions(),
	}, nil
}

// Generate читает актуальный YAML, проверяет объединённый каталог и создаёт facade.
// Результат заменяется атомарно только после успешной валидации и форматирования.
// Повторный вызов с тем же каталогом создаёт идентичный файл.
func (g *Generator) Generate() error {
	data, err := os.ReadFile(g.schemaPath)
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	schema, err := mergedSchema(data, g.systemDefinitions)
	if err != nil {
		return err
	}
	source, err := render(schema)
	if err != nil {
		return err
	}

	return writeGenerated(g.outputPath, source)
}

func mergedSchema(data []byte, platform []rtconfig.Definition) (*rtconfig.Schema, error) {
	service, err := rtconfig.ParseSchema(data)
	if err != nil {
		return nil, fmt.Errorf("parse service schema: %w", err)
	}

	definitions := append(service.Definitions(), platform...)
	schema, err := rtconfig.NewSchema(definitions...)
	if err != nil {
		return nil, fmt.Errorf("merge service and platform schema: %w", err)
	}

	return schema, nil
}

func writeGenerated(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	file, err := os.CreateTemp(directory, ".rtconfig-*.go")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)

	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write generated config: %w", err)
	}
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return fmt.Errorf("set generated config mode: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close generated config: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace generated config: %w", err)
	}

	return nil
}
