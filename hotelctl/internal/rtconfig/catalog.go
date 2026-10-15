// Package rtconfig changes service runtime values through the etcd wire contract.
// It reads service catalogs from disk and does not import another module's internal packages.
package rtconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const maxValueBytes = 1024

var variableName = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// Variable is the selected service declaration, not a remote value.
type Variable struct {
	Name     string
	Type     string
	Writable bool
	Default  string // JSON scalar, ready for display when no override exists.
	enum     []string
}

type catalog struct {
	RealtimeConfig map[string]definition `yaml:"realtimeConfig"`
}

type definition struct {
	Usage    string    `yaml:"usage"`
	Group    string    `yaml:"group"`
	Value    yaml.Node `yaml:"value"`
	Type     string    `yaml:"type"`
	Writable *bool     `yaml:"writable"`
	View     *view     `yaml:"view"`
}

type view struct {
	Enum []yaml.Node `yaml:"enum"`
}

// LoadVariable validates the selected declaration before any network I/O.
// The reader still validates the entire remote snapshot independently.
func LoadVariable(filename, name string) (Variable, error) {
	if len(name) > 128 || !variableName.MatchString(name) {
		return Variable{}, fmt.Errorf("invalid runtime variable name %q", name)
	}
	file, err := os.Open(filename)
	if err != nil {
		return Variable{}, fmt.Errorf("read service runtime catalog: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return Variable{}, fmt.Errorf("read service runtime catalog: %w", err)
	}
	if len(data) > 1<<20 {
		return Variable{}, fmt.Errorf("service runtime catalog exceeds 1 MiB")
	}

	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return Variable{}, fmt.Errorf("invalid service runtime YAML")
	}
	if hasAlias(&document) {
		return Variable{}, fmt.Errorf("runtime catalog must not contain YAML aliases")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var parsed catalog
	if err := decoder.Decode(&parsed); err != nil {
		return Variable{}, fmt.Errorf("invalid runtime catalog fields or duplicate keys")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return Variable{}, fmt.Errorf("runtime catalog must contain one YAML document")
	}
	if len(parsed.RealtimeConfig) == 0 || len(parsed.RealtimeConfig) > 256 {
		return Variable{}, fmt.Errorf("runtime catalog must contain 1..256 variables")
	}
	d, exists := parsed.RealtimeConfig[name]
	if !exists {
		return Variable{}, fmt.Errorf("runtime variable %q is not declared in the service catalog", name)
	}
	if d.Writable == nil || d.Value.Kind != yaml.ScalarNode || d.Value.Tag == "!!null" {
		return Variable{}, fmt.Errorf("runtime variable %q requires writable and a scalar default", name)
	}
	v := Variable{
		Name:     name,
		Type:     d.Type,
		Writable: *d.Writable,
	}
	if d.View != nil {
		if d.Type != "string" || len(d.View.Enum) == 0 {
			return Variable{}, fmt.Errorf("runtime enum requires string type and nonempty choices")
		}
		v.enum = make([]string, 0, len(d.View.Enum))
		for _, choice := range d.View.Enum {
			if choice.Kind != yaml.ScalarNode || choice.Tag != "!!str" || len(choice.Value) > maxValueBytes || slices.Contains(v.enum, choice.Value) {
				return Variable{}, fmt.Errorf("runtime enum requires unique string choices")
			}
			v.enum = append(v.enum, choice.Value)
		}
	}
	// Quoted scalar defaults are accepted by the service schema. Other YAML
	// scalars must belong to the declared type; strings stay literal strings.
	if !defaultTagMatches(d.Type, d.Value.Tag) {
		return Variable{}, fmt.Errorf("runtime variable %q: default does not match type %q", name, d.Type)
	}
	var native any
	if err := d.Value.Decode(&native); err != nil {
		return Variable{}, fmt.Errorf("runtime variable %q: invalid scalar default", name)
	}
	v.Default, err = v.Encode(fmt.Sprint(native))
	if err != nil {
		return Variable{}, fmt.Errorf("runtime variable %q: invalid default: %w", name, err)
	}
	return v, nil
}

func hasAlias(node *yaml.Node) bool {
	if node.Kind == yaml.AliasNode {
		return true
	}
	for _, child := range node.Content {
		if hasAlias(child) {
			return true
		}
	}
	return false
}

func defaultTagMatches(typ, tag string) bool {
	if tag == "!!str" {
		return true
	}
	switch typ {
	case "bool":
		return tag == "!!bool"
	case "int", "int64", "uint", "uint64":
		return tag == "!!int"
	case "float64":
		return tag == "!!int" || tag == "!!float"
	default:
		return false
	}
}

// Encode converts a CLI argument to the reader's JSON scalar representation.
// Strings and durations are quoted here, so users only need normal shell quoting.
func (v Variable) Encode(text string) (string, error) {
	if !utf8.ValidString(text) || len(text) > maxValueBytes {
		return "", fmt.Errorf("runtime value must be valid UTF-8 and at most %d bytes", maxValueBytes)
	}
	var value any
	var err error
	switch v.Type {
	case "bool":
		if text != "true" && text != "false" {
			return "", fmt.Errorf("boolean must be true or false")
		}
		value = text == "true"
	case "string":
		if v.enum != nil && !slices.Contains(v.enum, text) {
			return "", fmt.Errorf("string value is outside the declared enum")
		}
		value = text
	case "duration":
		var duration time.Duration
		duration, err = time.ParseDuration(text)
		value = duration.String()
	case "int", "int64":
		bits := 64
		if v.Type == "int" {
			bits = strconv.IntSize
		}
		value, err = strconv.ParseInt(text, 10, bits)
	case "uint", "uint64":
		bits := 64
		if v.Type == "uint" {
			bits = strconv.IntSize
		}
		value, err = strconv.ParseUint(text, 10, bits)
	case "float64":
		var number float64
		number, err = strconv.ParseFloat(text, 64)
		if math.IsInf(number, 0) || math.IsNaN(number) {
			return "", fmt.Errorf("float must be finite")
		}
		value = number
	default:
		return "", fmt.Errorf("unsupported runtime type %q", v.Type)
	}
	if err != nil {
		// strconv errors include input; do not accidentally echo a value here.
		return "", fmt.Errorf("invalid %s value", v.Type)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode runtime value: %w", err)
	}
	if len(encoded) > maxValueBytes {
		return "", fmt.Errorf("encoded runtime value exceeds %d bytes", maxValueBytes)
	}
	return string(encoded), nil
}
