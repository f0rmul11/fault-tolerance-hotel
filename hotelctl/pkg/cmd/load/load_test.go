package load

import (
	"testing"
	"time"
)

func TestValidateLoad(t *testing.T) {
	tests := []struct {
		name   string
		change func(*options)
		valid  bool
	}{
		{"valid", func(o *options) {}, true},
		{"zero rate", func(o *options) { o.rps = 0 }, false},
		{"negative duration", func(o *options) { o.duration = -time.Second }, false},
		{"zero clients", func(o *options) { o.clients = 0 }, false},
		{"invalid method", func(o *options) { o.method = "ListRooms" }, false},
		{"TLS unsupported", func(o *options) { o.addr = "https://localhost:9090" }, false},
		{"bad port", func(o *options) { o.addr = "localhost:0" }, false},
		{"bad JSON", func(o *options) { o.data = "null" }, false},
		{"reserved metadata", func(o *options) { o.headers = []string{"grpc-timeout:1S"} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := &options{addr: "localhost:9090", method: "/example.Service/Read", data: `{"id":"1"}`, rps: 20, clients: 1, duration: time.Second}
			tt.change(o)
			_, _, err := o.validate()
			if (err == nil) != tt.valid {
				t.Fatalf("validation: %v", err)
			}
		})
	}
}
func TestRepeatedMetadata(t *testing.T) {
	o := &options{addr: "localhost:9090", method: "example.Service/Read", data: "{}", rps: 1, clients: 1, duration: time.Second, headers: []string{"X-Tag: one", "x-tag: two"}}
	_, metadata, err := o.validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata["x-tag"]) != 2 {
		t.Fatal("repeated metadata lost")
	}
}
