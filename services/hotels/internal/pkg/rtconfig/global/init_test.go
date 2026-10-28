package global

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hotels/internal/pkg/rtconfig"
)

func fixture(t *testing.T) (rtconfig.Config, string) {
	t.Helper()
	data := []byte("realtimeConfig:\n  enabled: {type: bool, value: false, writable: true}\n")
	path := filepath.Join(t.TempDir(), "rtconfig.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	schema, err := rtconfig.ParseSchema(data)
	if err != nil {
		t.Fatal(err)
	}
	return rtconfig.Config{
		Endpoints:       []string{"http://unreachable.invalid:2379"},
		Prefix:          "/test/runtime/",
		SchemaPath:      path,
		StartupTimeout:  5 * time.Millisecond,
		ShutdownTimeout: time.Second,
	}, schema.Hash()
}

func TestOfflineStartInstallsDefaultsAndCloseRemovesReadiness(t *testing.T) {
	previous := Client()
	t.Cleanup(func() {
		SetClient(previous)
	})
	cfg, hash := fixture(t)
	var uninitialized *rtconfig.Manager
	if !errors.Is(uninitialized.CheckReady(), rtconfig.ErrNotInitialized) {
		t.Fatal("uninitialized runtime is ready")
	}
	runtime, err := Init(t.Context(), cfg, hash)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	value, err := Client().Value(t.Context(), "enabled")
	if err != nil || value.Bool() || runtime.Status().Source != "defaults" {
		t.Fatalf("offline initialization did not install defaults: value=%v error=%v", value, err)
	}
	if err := runtime.CheckReady(); err != nil {
		t.Fatalf("valid defaults must be ready without etcd: %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(runtime.CheckReady(), rtconfig.ErrClosed) {
		t.Fatal("closed runtime is still ready")
	}
}

func TestStartRejectsInvalidSchemaAndPreservesGlobalClient(t *testing.T) {
	for _, name := range []string{"invalid_yaml", "stale_hash", "missing_file", "canceled_lifetime"} {
		t.Run(name, func(t *testing.T) {
			cfg, hash := fixture(t)
			ctx := t.Context()
			switch name {
			case "invalid_yaml":
				if err := os.WriteFile(cfg.SchemaPath, []byte("realtimeConfig: ["), 0o600); err != nil {
					t.Fatal(err)
				}
			case "stale_hash":
				hash = "old generated facade"
			case "missing_file":
				cfg.SchemaPath += ".missing"
			case "canceled_lifetime":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			previous := Client()
			runtime, err := Init(ctx, cfg, hash)
			if err == nil {
				_ = runtime.Close(context.Background())
				t.Fatal("invalid initialization accepted")
			}
			if name == "stale_hash" && !strings.Contains(err.Error(), "generated facade") {
				t.Fatalf("expected schema hash mismatch: %v", err)
			}
			if name == "canceled_lifetime" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost context cancellation: %v", err)
			}
			if Client() != previous {
				t.Fatal("failed startup replaced global client")
			}
		})
	}
}
