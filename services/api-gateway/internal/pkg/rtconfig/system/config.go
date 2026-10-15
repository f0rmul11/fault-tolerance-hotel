// Package system хранит платформенный каталог runtime-настроек.
// Сервисные флаги объявляются в config/rtconfig.yaml, а не в этом registry.
package system

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"api-gateway/internal/pkg/rtconfig"

	"gopkg.in/yaml.v3"
)

// WatchCallback принимает immutable значения. При начальном применении old
// содержит error Value, а не nil. Callback должен быть коротким и идемпотентным.
type WatchCallback func(old, new rtconfig.Value)

type Option func(*registrationOptions)

type registrationOptions struct {
	writable bool
	enum     []string
}

func WithWritable(writable bool) Option {
	return func(options *registrationOptions) {
		options.writable = writable
	}
}

func WithEnum(values ...string) Option {
	values = append([]string{}, values...)

	return func(options *registrationOptions) {
		options.enum = append([]string{}, values...)
	}
}

type registry struct {
	mu          sync.RWMutex
	definitions map[string]rtconfig.Definition
	callbacks   map[string]WatchCallback
	frozen      bool
}

func newRegistry() *registry {
	return &registry{
		definitions: make(map[string]rtconfig.Definition),
		callbacks:   make(map[string]WatchCallback),
	}
}

var catalog = newRegistry()

// register вызывается только платформенным package при инициализации.
// Ошибка определения является ошибкой программы и останавливает запуск.
func register(name, usage, ttype string, value any, callback WatchCallback, opts ...Option) {
	if err := catalog.add(name, usage, ttype, value, callback, opts...); err != nil {
		panic(err)
	}
}

func (r *registry) add(name, usage, ttype string, value any, callback WatchCallback, opts ...Option) error {
	options := registrationOptions{}
	for _, option := range opts {
		if option == nil {
			return fmt.Errorf("system config %q: nil option", name)
		}
		option(&options)
	}
	definition := rtconfig.Definition{
		Name:     name,
		Usage:    usage,
		Group:    "platform",
		Type:     rtconfig.Type(ttype),
		Default:  value,
		Writable: options.writable,
		Enum:     options.enum,
	}

	schema, err := rtconfig.NewSchema(definition)
	if err != nil {
		return fmt.Errorf("system config %q: %w", name, err)
	}
	definition = schema.Definitions()[0]

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.frozen {
		return fmt.Errorf("system config %q: registry is frozen", name)
	}
	if _, exists := r.definitions[name]; exists {
		return fmt.Errorf("system config %q: duplicate definition", name)
	}

	r.definitions[name] = definition
	if callback != nil {
		r.callbacks[name] = callback
	}

	return nil
}

// Freeze запрещает дальнейшую регистрацию; повторный вызов безопасен.
func Freeze() {
	catalog.freeze()
}

func (r *registry) freeze() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.frozen = true
}

// Definitions возвращает независимую копию каталога в стабильном порядке.
// Runtime и generator объединяют её с service schema через rtconfig.NewSchema.
func Definitions() []rtconfig.Definition {
	return catalog.snapshot()
}

func (r *registry) snapshot() []rtconfig.Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	definitions := make([]rtconfig.Definition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		definition.Enum = append([]string(nil), definition.Enum...)
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Name < definitions[j].Name
	})

	return definitions
}

// Callbacks возвращает копию map: вызывающий код не может изменить registry.
func Callbacks() map[string]WatchCallback {
	return catalog.callbackSnapshot()
}

func (r *registry) callbackSnapshot() map[string]WatchCallback {
	r.mu.RLock()
	defer r.mu.RUnlock()

	callbacks := make(map[string]WatchCallback, len(r.callbacks))
	for name, callback := range r.callbacks {
		callbacks[name] = callback
	}

	return callbacks
}

type variable struct {
	Usage    string `yaml:"usage"`
	Group    string `yaml:"group"`
	Value    any    `yaml:"value"`
	Type     string `yaml:"type"`
	Writable bool   `yaml:"writable"`
	View     *view  `yaml:"view,omitempty"`
}

type view struct {
	Enum []string `yaml:"enum"`
}

type valuesConfig struct {
	RealtimeConfig map[string]variable `yaml:"realtimeConfig"`
}

// ConfigYAML сохраняет форму realtimeConfig из scratch, включая нашу явную
// семантику writable. Duration сериализуется как строка Go duration.
func ConfigYAML() ([]byte, error) {
	return marshalDefinitions(Definitions())
}

func marshalDefinitions(definitions []rtconfig.Definition) ([]byte, error) {
	cfg := valuesConfig{
		RealtimeConfig: make(map[string]variable, len(definitions)),
	}
	for _, definition := range definitions {
		value := definition.Default
		if duration, ok := value.(time.Duration); ok && definition.Type == rtconfig.DurationType {
			value = duration.String()
		}

		item := variable{
			Usage:    definition.Usage,
			Group:    definition.Group,
			Value:    value,
			Type:     string(definition.Type),
			Writable: definition.Writable,
		}
		if len(definition.Enum) > 0 {
			item.View = &view{Enum: append([]string(nil), definition.Enum...)}
		}
		cfg.RealtimeConfig[definition.Name] = item
	}

	return yaml.Marshal(cfg)
}
