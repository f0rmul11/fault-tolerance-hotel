package config

import (
	"testing"
)

func TestEnvironmentOverridesAndInvalidConfig(t *testing.T) {
	t.Setenv("CONFIG_PATH", "config.yaml")
	t.Setenv("HTTP_PORT", "18080")
	t.Setenv("GRPC_PORT", "19090")
	cfg, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HttpServer.Port != 18080 || cfg.GrpcServer.Port != 19090 {
		t.Fatalf("environment was not applied: %+v", cfg)
	}
	t.Setenv("GRPC_PORT", "18080")
	if _, err := Read(); err == nil {
		t.Fatal("overlapping HTTP/gRPC ports must fail")
	}
	t.Setenv("GRPC_PORT", "0")
	if _, err := Read(); err == nil {
		t.Fatal("invalid port must fail")
	}
	t.Setenv("GRPC_PORT", "19090")
	t.Setenv("GRACEFUL_TIMEOUT", "0s")
	if _, err := Read(); err == nil {
		t.Fatal("unbounded shutdown must fail")
	}
}

func TestGracefulDelay(t *testing.T) {
	t.Setenv("CONFIG_PATH", "config.yaml")
	t.Setenv("GRACEFUL_TIMEOUT", "10s")
	for _, delay := range []string{"0s", "3s", "-1s", "10s", "11s"} {
		t.Run(delay, func(t *testing.T) {
			t.Setenv("GRACEFUL_DELAY", delay)
			cfg, err := Read()
			valid := delay == "0s" || delay == "3s"
			if valid && err != nil {
				t.Fatal(err)
			}
			if !valid && err == nil {
				t.Fatal("invalid graceful delay accepted")
			}
			if valid && cfg.Graceful.Delay.String() != delay {
				t.Fatalf("delay override not applied: %v", cfg.Graceful.Delay)
			}
		})
	}
}
