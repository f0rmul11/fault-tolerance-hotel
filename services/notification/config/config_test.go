package config

import "testing"

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

func TestKafkaConfiguration(t *testing.T) {
	t.Setenv("CONFIG_PATH", "config.yaml")
	t.Setenv("KAFKA_BROKERS", "first:9092,second:9092")
	t.Setenv("KAFKA_CONSUMER_GROUP", "notification-test")
	cfg, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.KafkaBrokers) != 2 || cfg.ConsumerGroup != "notification-test" {
		t.Fatalf("Kafka environment was not applied: %+v", cfg)
	}
	cfg.ConsumerGroup = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("missing consumer group accepted")
	}
	cfg.ConsumerGroup = "notification-test"
	cfg.KafkaBrokers = nil
	if err := cfg.Validate(); err == nil {
		t.Fatal("missing brokers accepted")
	}
}
