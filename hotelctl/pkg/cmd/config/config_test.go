package config

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	projectconfig "github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/stand"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/pkg/cmd/util"
)

type localFactory struct {
	config projectconfig.Config
}

func (f localFactory) Config() (projectconfig.Config, error) {
	return f.config, nil
}

func (localFactory) NewStand(projectconfig.Config, iostreams.IOStreams) (stand.Interface, error) {
	panic("runtime configuration must not create Docker or a stand")
}

func fixture(t *testing.T) localFactory {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hotel.yaml"), []byte("services: [hotels, review, api-gateway]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "services", "hotels", "config")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "rtconfig.yaml"), []byte(`realtimeConfig:
  flag: {type: bool, value: false, writable: true}
  readonly: {type: bool, value: true, writable: false}
  mode: {type: string, value: normal, writable: true, view: {enum: [normal, degraded]}}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := projectconfig.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return localFactory{config: cfg}
}

func TestInvalidConfigInputFailsBeforeConnection(t *testing.T) {
	f := fixture(t)
	for _, args := range [][]string{
		{"set", "hotels", "flag"},
		{"set", "missing", "flag", "true"},
		{"set", "review", "flag", "true"},
		{"set", "api-gateway", "flag", "true"},
		{"get", "api-gateway", "flag"},
		{"set", "hotels", "unknown", "true"},
		{"set", "hotels", "readonly", "false"},
		{"set", "hotels", "flag", "yes"},
		{"set", "hotels", "mode", "bad"},
		{"set", "hotels", "flag", "true", "--expect-revision=-1"},
		{"set", "hotels", "flag", "true", "--env=../prod"},
		{"set", "hotels", "flag", "true", "--prefix=/wrong/../prefix/"},
		{"set", "hotels", "flag", "true", "--timeout=0"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var output bytes.Buffer
			command := NewCmdConfig(f, iostreams.IOStreams{
				Out:    &output,
				ErrOut: io.Discard,
			})
			// A cancelled context also makes an accidental network attempt visible
			// as a different error instead of waiting on a real endpoint.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			command.SetArgs(args)
			err := command.ExecuteContext(ctx)
			if err == nil || strings.Contains(err.Error(), "context canceled") {
				t.Fatalf("expected local validation failure: %v", err)
			}
			if output.Len() != 0 {
				t.Fatalf("invalid input reported success: %s", output.String())
			}
		})
	}
}

func TestDefaultKeyAndCustomServiceContext(t *testing.T) {
	f := fixture(t)
	o := &Options{
		Options:     util.NewOptions(iostreams.IOStreams{}),
		Environment: "dev",
		set:         true,
	}
	o.Connection.Endpoints = []string{"http://127.0.0.1:2379"}
	if err := o.Complete(f, []string{"hotels", "flag", "true"}); err != nil {
		t.Fatal(err)
	}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	if o.Key != "/hotelflow/dev/services/hotels/rtconfig/v1/values/flag" || o.Value != "true" {
		t.Fatalf("wrong target: %s %s", o.Key, o.Value)
	}

	old := filepath.Join(f.config.Root, "services/hotels")
	newPath := filepath.Join(f.config.Root, "alternate")
	if err := os.Rename(old, newPath); err != nil {
		t.Fatal(err)
	}
	f.config.Services[0].Context = "alternate"
	o.Prefix = "/custom/service/"
	if err := o.Complete(f, []string{"hotels", "flag", "false"}); err != nil {
		t.Fatal(err)
	}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	if o.Key != "/custom/service/flag" {
		t.Fatalf("custom prefix ignored: %s", o.Key)
	}
}
