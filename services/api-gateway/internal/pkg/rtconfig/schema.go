package rtconfig

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Definition описывает локальный контракт одного параметра.
type Definition struct {
	Name     string
	Usage    string
	Group    string
	Type     Type
	Default  any
	Writable bool
	Enum     []string
}

// Schema содержит проверенный неизменяемый каталог и значения по умолчанию.
type Schema struct {
	definitions []Definition
	values      map[string]Value
	names       []string
	hash        string
}

type yamlSchema struct {
	RealtimeConfig map[string]yamlDefinition `yaml:"realtimeConfig"`
}

type yamlDefinition struct {
	Usage    string    `yaml:"usage"`
	Group    string    `yaml:"group"`
	Value    yaml.Node `yaml:"value"`
	Type     Type      `yaml:"type"`
	Writable *bool     `yaml:"writable"`
	View     *yamlView `yaml:"view"`
}

type yamlView struct {
	Enum []yaml.Node `yaml:"enum"`
}

// ParseSchema читает единственный YAML-документ и отвергает неизвестные поля и дубликаты.
func ParseSchema(data []byte) (*Schema, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("rtconfig: invalid schema YAML")
	}
	if err := validateYAMLNode(&document); err != nil {
		return nil, err
	}
	if err := validateYAMLMetadata(&document); err != nil {
		return nil, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var parsed yamlSchema
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("rtconfig: invalid schema fields")
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("rtconfig: invalid trailing schema document")
		}
		return nil, fmt.Errorf("rtconfig: schema must contain one YAML document")
	}
	if parsed.RealtimeConfig == nil {
		return nil, fmt.Errorf("rtconfig: realtimeConfig mapping is required")
	}

	definitions := make([]Definition, 0, len(parsed.RealtimeConfig))
	names := make([]string, 0, len(parsed.RealtimeConfig))
	for name := range parsed.RealtimeConfig {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		item := parsed.RealtimeConfig[name]
		if item.Writable == nil {
			return nil, fmt.Errorf("rtconfig: %q requires writable", name)
		}
		if item.Value.Kind != yaml.ScalarNode || item.Value.Tag == "!!null" {
			return nil, fmt.Errorf("rtconfig: %q requires a non-null scalar value", name)
		}

		definition := Definition{
			Name:     name,
			Usage:    item.Usage,
			Group:    item.Group,
			Type:     item.Type,
			Default:  item.Value.Value,
			Writable: *item.Writable,
		}
		if item.View != nil {
			definition.Enum = make([]string, 0, len(item.View.Enum))
			for _, option := range item.View.Enum {
				if option.Kind != yaml.ScalarNode || option.Tag != "!!str" {
					return nil, fmt.Errorf("rtconfig: %q enum values must be strings", name)
				}
				definition.Enum = append(definition.Enum, option.Value)
			}
		}
		if item.Type == StringType && item.Value.Tag != "!!str" {
			return nil, fmt.Errorf("rtconfig: %q string default must be a string", name)
		}
		definitions = append(definitions, definition)
	}
	return NewSchema(definitions...)
}

func validateYAMLNode(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("rtconfig: YAML aliases are not supported")
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return fmt.Errorf("rtconfig: YAML mapping keys must be strings")
			}
			if seen[key.Value] {
				return fmt.Errorf("rtconfig: duplicate YAML key %q", key.Value)
			}
			seen[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := validateYAMLNode(child); err != nil {
			return err
		}
	}
	return nil
}

func validateYAMLMetadata(document *yaml.Node) error {
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("rtconfig: schema must be a mapping")
	}
	root := document.Content[0]
	for index := 0; index < len(root.Content); index += 2 {
		if root.Content[index].Value != "realtimeConfig" {
			continue
		}
		catalog := root.Content[index+1]
		if catalog.Kind != yaml.MappingNode {
			return fmt.Errorf("rtconfig: realtimeConfig must be a mapping")
		}
		for entry := 0; entry < len(catalog.Content); entry += 2 {
			definition := catalog.Content[entry+1]
			if definition.Kind != yaml.MappingNode {
				return fmt.Errorf("rtconfig: definitions must be mappings")
			}
			for field := 0; field < len(definition.Content); field += 2 {
				name := definition.Content[field].Value
				value := definition.Content[field+1]
				switch name {
				case "usage", "group", "type":
					if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
						return fmt.Errorf("rtconfig: metadata field %q must be a string", name)
					}
				case "writable":
					if value.Kind != yaml.ScalarNode || value.Tag != "!!bool" {
						return fmt.Errorf("rtconfig: writable must be a boolean")
					}
				}
			}
		}
	}
	return nil
}

// NewSchema проверяет defaults, типы и enum и копирует переданные определения.
func NewSchema(definitions ...Definition) (*Schema, error) {
	if len(definitions) > maxKeys {
		return nil, fmt.Errorf("rtconfig: schema exceeds %d keys", maxKeys)
	}

	schema := &Schema{
		definitions: make([]Definition, 0, len(definitions)),
		values:      make(map[string]Value, len(definitions)),
		names:       make([]string, 0, len(definitions)),
	}
	for _, definition := range definitions {
		if !validName(definition.Name) {
			return nil, fmt.Errorf("rtconfig: invalid name %q", definition.Name)
		}
		if _, exists := schema.values[definition.Name]; exists {
			return nil, fmt.Errorf("rtconfig: duplicate definition %q", definition.Name)
		}
		value, err := defaultValue(definition.Type, definition.Default)
		if err != nil {
			return nil, fmt.Errorf("rtconfig: %q default: %w", definition.Name, err)
		}
		if definition.Enum != nil && definition.Type != StringType {
			return nil, fmt.Errorf("rtconfig: %q enum requires string type", definition.Name)
		}
		if definition.Enum != nil && len(definition.Enum) == 0 {
			return nil, fmt.Errorf("rtconfig: %q enum must not be empty", definition.Name)
		}
		definition.Enum = slices.Clone(definition.Enum)
		slices.Sort(definition.Enum)
		for index, option := range definition.Enum {
			if index > 0 && option == definition.Enum[index-1] {
				return nil, fmt.Errorf("rtconfig: %q has duplicate enum value", definition.Name)
			}
			encoded, _ := json.Marshal(option)
			if len(encoded) > maxValueBytes {
				return nil, fmt.Errorf("rtconfig: %q enum value exceeds %d bytes", definition.Name, maxValueBytes)
			}
		}
		if len(definition.Enum) > 0 && !slices.Contains(definition.Enum, value.String()) {
			return nil, fmt.Errorf("rtconfig: %q default is outside enum", definition.Name)
		}
		definition.Default = nativeValue(value)
		wire, err := json.Marshal(wireValue(value))
		if err != nil {
			return nil, fmt.Errorf("rtconfig: %q default serialization: %w", definition.Name, err)
		}
		if len(wire) > maxValueBytes {
			return nil, fmt.Errorf("rtconfig: %q default exceeds %d bytes", definition.Name, maxValueBytes)
		}
		schema.definitions = append(schema.definitions, definition)
		schema.values[definition.Name] = value
		schema.names = append(schema.names, definition.Name)
	}
	slices.SortFunc(schema.definitions, func(left, right Definition) int {
		return strings.Compare(left.Name, right.Name)
	})
	slices.Sort(schema.names)
	schema.hash = schemaHash(schema.definitions, schema.values)
	return schema, nil
}

func validName(name string) bool {
	if len(name) == 0 || len(name) > maxKeyBytes || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for index := range name {
		character := name[index]
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' {
			continue
		}
		if character != '_' || index == len(name)-1 || name[index-1] == '_' {
			return false
		}
	}
	return true
}

// Definitions возвращает копию каталога, включая enum slices.
func (s *Schema) Definitions() []Definition {
	definitions := slices.Clone(s.definitions)
	for index := range definitions {
		definitions[index].Enum = slices.Clone(definitions[index].Enum)
	}
	return definitions
}

// Names возвращает отсортированную копию имён.
func (s *Schema) Names() []string {
	return slices.Clone(s.names)
}

// Hash идентифицирует контракт: имена, типы, defaults, writable и enum.
func (s *Schema) Hash() string {
	return s.hash
}

// RequireType проверяет тип потребителя до начала обслуживания запросов.
func (s *Schema) RequireType(name string, typ Type) error {
	value, exists := s.values[name]
	if !exists {
		return fmt.Errorf("rtconfig: %q: %w", name, ErrVariableNotFound)
	}
	if value.Type() != typ {
		return fmt.Errorf("rtconfig: %q has type %q, want %q", name, value.Type(), typ)
	}
	return nil
}

func (s *Schema) defaults() map[string]Value {
	values := make(map[string]Value, len(s.values))
	for name, value := range s.values {
		values[name] = value
	}
	return values
}

func (s *Schema) decode(raw map[string][]byte) (map[string]Value, error) {
	values := s.defaults()
	for _, definition := range s.definitions {
		data, exists := raw[definition.Name]
		if !exists {
			continue
		}
		if !definition.Writable {
			return nil, fmt.Errorf("rtconfig: %q is readonly", definition.Name)
		}
		if len(data) > maxValueBytes {
			return nil, fmt.Errorf("rtconfig: %q exceeds %d bytes", definition.Name, maxValueBytes)
		}
		value, err := decodeValue(definition.Type, data)
		if err != nil {
			return nil, fmt.Errorf("rtconfig: %q: %w", definition.Name, err)
		}
		if len(definition.Enum) > 0 && !slices.Contains(definition.Enum, value.String()) {
			return nil, fmt.Errorf("rtconfig: %q value is outside enum", definition.Name)
		}
		values[definition.Name] = value
	}
	return values, nil
}

func schemaHash(definitions []Definition, values map[string]Value) string {
	type contract struct {
		Name     string
		Type     Type
		Default  string
		Writable bool
		Enum     []string
	}
	contracts := make([]contract, 0, len(definitions))
	for _, definition := range definitions {
		contracts = append(contracts, contract{
			Name:     definition.Name,
			Type:     definition.Type,
			Default:  values[definition.Name].String(),
			Writable: definition.Writable,
			Enum:     definition.Enum,
		})
	}
	data, _ := json.Marshal(contracts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func defaultValue(typ Type, raw any) (*scalarValue, error) {
	if typ == DurationType {
		if duration, ok := raw.(time.Duration); ok {
			return parseScalar(typ, duration.String())
		}
	}
	var text string
	switch value := raw.(type) {
	case string:
		if len(value) > maxValueBytes {
			return nil, fmt.Errorf("default exceeds %d bytes", maxValueBytes)
		}
		text = value
	case bool:
		if typ != BoolType {
			return nil, fmt.Errorf("bool default does not match %q", typ)
		}
		text = strconv.FormatBool(value)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		if typ != IntType && typ != Int64Type && typ != UintType && typ != Uint64Type && typ != Float64Type {
			return nil, fmt.Errorf("integer default does not match %q", typ)
		}
		text = fmt.Sprint(value)
	case float64:
		if typ != Float64Type {
			return nil, fmt.Errorf("float default does not match %q", typ)
		}
		text = strconv.FormatFloat(value, 'g', -1, 64)
	default:
		return nil, fmt.Errorf("default must be a supported scalar")
	}
	return parseScalar(typ, text)
}

func decodeValue(typ Type, data []byte) (*scalarValue, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("invalid JSON scalar")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("value must contain one JSON scalar")
	}

	switch typ {
	case BoolType:
		value, ok := raw.(bool)
		if !ok {
			return nil, fmt.Errorf("expected JSON boolean")
		}
		return parseScalar(typ, strconv.FormatBool(value))
	case StringType, DurationType:
		value, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("expected JSON string")
		}
		return parseScalar(typ, value)
	case IntType, Int64Type, UintType, Uint64Type, Float64Type:
		value, ok := raw.(json.Number)
		if !ok {
			return nil, fmt.Errorf("expected JSON number")
		}
		return parseScalar(typ, value.String())
	default:
		return nil, fmt.Errorf("unsupported type %q", typ)
	}
}

func parseScalar(typ Type, text string) (*scalarValue, error) {
	value := &scalarValue{
		typ: typ,
	}
	var err error
	switch typ {
	case BoolType:
		if text != "true" && text != "false" {
			return nil, fmt.Errorf("boolean must be true or false")
		}
		value.boolean = text == "true"
		value.text = text
	case StringType:
		value.text = text
	case IntType, Int64Type:
		bits := 64
		if typ == IntType {
			bits = strconv.IntSize
		}
		value.signed, err = strconv.ParseInt(text, 10, bits)
		value.text = strconv.FormatInt(value.signed, 10)
	case UintType, Uint64Type:
		bits := 64
		if typ == UintType {
			bits = strconv.IntSize
		}
		value.unsigned, err = strconv.ParseUint(text, 10, bits)
		value.text = strconv.FormatUint(value.unsigned, 10)
	case Float64Type:
		value.floating, err = strconv.ParseFloat(text, 64)
		if math.IsNaN(value.floating) || math.IsInf(value.floating, 0) {
			return nil, fmt.Errorf("float must be finite")
		}
		value.text = strconv.FormatFloat(value.floating, 'g', -1, 64)
	case DurationType:
		value.duration, err = time.ParseDuration(text)
		value.text = value.duration.String()
	default:
		return nil, fmt.Errorf("unsupported type %q", typ)
	}
	if err != nil {
		return nil, fmt.Errorf("invalid %s value", typ)
	}
	return value, nil
}

func nativeValue(value *scalarValue) any {
	switch value.typ {
	case BoolType:
		return value.boolean
	case StringType:
		return value.text
	case IntType:
		return int(value.signed)
	case Int64Type:
		return value.signed
	case UintType:
		return uint(value.unsigned)
	case Uint64Type:
		return value.unsigned
	case Float64Type:
		return value.floating
	case DurationType:
		return value.duration
	default:
		return nil
	}
}

func wireValue(value *scalarValue) any {
	if value.typ == DurationType {
		return value.text
	}
	return nativeValue(value)
}
