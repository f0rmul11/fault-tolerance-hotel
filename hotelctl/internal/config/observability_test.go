package config

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestObservabilitySelectionAndReservedPorts(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	originalRuntime := c.Runtime
	c.Observability = Observability{
		Enabled: true,
		Stage:   "traces",
	}
	components, err := c.SelectComponents([]string{"otel-collector", "jaeger"})
	if err != nil || len(components) != 2 {
		t.Fatalf("tracing components: %+v, %v", components, err)
	}
	if _, err := c.SelectComponents([]string{"grafana"}); err == nil {
		t.Fatal("reserved Grafana address must not become an active deployment")
	}
	for _, host := range []int{16686, 3000} {
		if !slices.ContainsFunc(c.PortBindings(), func(port PortBinding) bool {
			return port.Host == host && port.LocalOnly
		}) {
			t.Fatalf("missing local-only UI binding %d", host)
		}
	}
	if c.Runtime != originalRuntime {
		t.Fatal("enabling tracing changed persisted Runtime compatibility")
	}
	c.Observability.Enabled = false
	if len(c.ObservabilityComponents()) != 0 || len(c.ObservabilityPortBindings()) != 0 {
		t.Fatal("disabled tracing exposed components or ports")
	}
}

func TestObservabilityRejectsUnsupportedStagesAndPortCollisions(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{
			name: "future stage",
			change: func(c *Config) {
				c.Observability.Stage = "unknown"
			},
		},
		{
			name: "host port",
			change: func(c *Config) {
				c.Services[1].GRPC.Host = 16686
			},
		},
		{
			name: "reserved Grafana node port",
			change: func(c *Config) {
				c.Services[1].GRPC.Node = 30300
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := Load(root, "")
			if err != nil {
				t.Fatal(err)
			}
			test.change(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("accepted invalid observability configuration")
			}
		})
	}
}
