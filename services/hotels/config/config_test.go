package config

import (
	"path/filepath"
	"testing"
	"time"
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

func TestRuntimeConfigEnvironmentAndSchemaLocation(t *testing.T) {
	path, err := filepath.Abs("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_PATH", path)
	t.Setenv("RTCONFIG_ENDPOINTS", "http://one:2379,http://two:2379")
	t.Setenv("RTCONFIG_PREFIX", "/test/runtime/")
	t.Setenv("RTCONFIG_STARTUP_TIMEOUT", "250ms")
	cfg, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	runtime := cfg.RuntimeConfig
	if len(runtime.Endpoints) != 2 || runtime.Endpoints[1] != "http://two:2379" || runtime.Prefix != "/test/runtime/" || runtime.StartupTimeout != 250*time.Millisecond {
		t.Fatalf("runtime environment overrides: %+v", runtime)
	}
	if runtime.SchemaPath != filepath.Join(filepath.Dir(path), "rtconfig.yaml") {
		t.Fatalf("schema must be relative to static YAML: %s", runtime.SchemaPath)
	}
	t.Setenv("RTCONFIG_SCHEMA_PATH", filepath.Join(t.TempDir(), "custom.yaml"))
	cfg, err = Read()
	if err != nil || !filepath.IsAbs(cfg.RuntimeConfig.SchemaPath) {
		t.Fatalf("absolute schema override: %v", err)
	}
	t.Setenv("RTCONFIG_SHUTDOWN_TIMEOUT", "0s")
	if _, err := Read(); err == nil {
		t.Fatal("unbounded runtime shutdown accepted")
	}
}
