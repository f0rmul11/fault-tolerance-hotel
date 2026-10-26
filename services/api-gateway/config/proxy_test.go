package config

import "testing"

func TestServiceTargetsOverrideForEnvoy(t *testing.T) {
	t.Setenv("CONFIG_PATH", "config.yaml")
	t.Setenv("SERVICE_TARGETS", "booking:envoy:10001,hotels:envoy:10002")
	cfg, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Targets) != 2 || cfg.Targets["booking"] != "envoy:10001" || cfg.Targets["hotels"] != "envoy:10002" {
		t.Fatalf("RPC targets were not replaced correctly: %v", cfg.Targets)
	}
}
