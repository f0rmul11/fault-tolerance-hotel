package system

import (
	"testing"
	"time"

	"api-gateway/internal/pkg/rtconfig"
)

func TestRegistryCopiesAndFreeze(t *testing.T) {
	r := newRegistry()
	enum := []string{"INFO", "DEBUG"}
	var calls int
	callback := func(old, new rtconfig.Value) {
		calls++
	}
	if err := r.add("log_level", "Уровень логирования", "string", "INFO", callback, WithWritable(true), WithEnum(enum...)); err != nil {
		t.Fatal(err)
	}
	enum[0] = "BROKEN"

	definitions := r.snapshot()
	definitions[0].Enum[0] = "MUTATED"
	definitions[0].Default = "MUTATED"
	callbacks := r.callbackSnapshot()
	delete(callbacks, "log_level")

	current := r.snapshot()[0]
	if current.Default != "INFO" || current.Enum[0] != "DEBUG" || current.Enum[1] != "INFO" || !current.Writable {
		t.Fatalf("external mutation changed definition: %+v", current)
	}
	r.callbackSnapshot()["log_level"](nil, nil)
	if calls != 1 {
		t.Fatal("external map mutation removed callback")
	}
	if err := r.add("log_level", "duplicate", "string", "DEBUG", nil); err == nil {
		t.Fatal("duplicate system key accepted")
	}
	r.freeze()
	r.freeze()
	if err := r.add("other_flag", "", "bool", false, nil); err == nil {
		t.Fatal("registration after Freeze accepted")
	}
}

func TestRegistryYAMLRoundTrip(t *testing.T) {
	r := newRegistry()
	if err := r.add("timeout", "Таймаут", "duration", 250*time.Millisecond, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.add("debug", "Отладка", "bool", "false", nil, WithWritable(true)); err != nil {
		t.Fatal(err)
	}
	data, err := marshalDefinitions(r.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	schema, err := rtconfig.ParseSchema(data)
	if err != nil {
		t.Fatalf("parse generated YAML: %v\n%s", err, data)
	}
	definitions := schema.Definitions()
	if len(definitions) != 2 || definitions[0].Name != "debug" || definitions[0].Default != false || !definitions[0].Writable {
		t.Fatalf("unexpected bool definition: %+v", definitions)
	}
	if definitions[1].Default != 250*time.Millisecond || definitions[1].Writable {
		t.Fatalf("duration/default writable changed: %+v", definitions[1])
	}
}

func TestRegistryRejectsInvalidDefinition(t *testing.T) {
	tests := []struct {
		name  string
		ttype string
		value any
		opts  []Option
	}{
		{
			name:  "wrong_type",
			ttype: "bool",
			value: "yes",
		},
		{
			name:  "invalid_enum_default",
			ttype: "string",
			value: "TRACE",
			opts:  []Option{WithEnum("INFO", "DEBUG")},
		},
		{
			name:  "nil_option",
			ttype: "bool",
			value: false,
			opts:  []Option{nil},
		},
		{
			name:  "empty_enum",
			ttype: "string",
			value: "INFO",
			opts:  []Option{WithEnum()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRegistry()
			if err := r.add(tt.name, "", tt.ttype, tt.value, nil, tt.opts...); err == nil {
				t.Fatal("invalid definition accepted")
			}
			if len(r.snapshot()) != 0 {
				t.Fatal("invalid definition left partial registry state")
			}
		})
	}
}
