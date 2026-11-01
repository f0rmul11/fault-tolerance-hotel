package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
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

func TestRetryYAML(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprint(enabled), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			content := fmt.Sprintf(`retry:
  __default__:
    enabled: false
    max_attempts: 3
    initial_backoff: 200ms
    max_backoff: 1s
    throttler:
      throttling_enabled: false
  handlers:
    - method: /booking.booking.v1.BookingService/CreateBooking
      enabled: %t
`, enabled)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			var cfg Config
			if err := cleanenv.ReadConfig(path, &cfg); err != nil {
				t.Fatal(err)
			}
			policy := cfg.Retry.Default
			if len(cfg.Retry.Handlers) != 1 {
				t.Fatalf("unexpected retry handlers: %+v", cfg.Retry.Handlers)
			}
			handler := cfg.Retry.Handlers[0]
			if handler.Method != "/booking.booking.v1.BookingService/CreateBooking" ||
				handler.Enabled == nil || *handler.Enabled != enabled {
				t.Fatalf("unexpected handler policy: %+v", handler)
			}
			if policy.Enabled == nil || *policy.Enabled || policy.MaxAttempts != 3 ||
				policy.InitialBackoff != 200*time.Millisecond || policy.MaxBackoff != time.Second ||
				policy.Throttler.Enabled == nil || *policy.Throttler.Enabled {
				t.Fatalf("unexpected booking retry policy: %+v", policy)
			}
		})
	}
}
