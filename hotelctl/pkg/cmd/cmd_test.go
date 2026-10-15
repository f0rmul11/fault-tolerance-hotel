package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/stand"
)

type fakeFactory struct {
	c        config.Config
	calls    int
	built    []config.Service
	block    bool
	smoke    bool
	business bool
	reset    bool
}

func (f *fakeFactory) Config() (config.Config, error) {
	return f.c, nil
}

func (f *fakeFactory) NewStand(_ config.Config, _ iostreams.IOStreams) (stand.Interface, error) {
	f.calls++
	return &fakeStand{
		f: f,
	}, nil
}

type fakeStand struct {
	stand.Interface
	f *fakeFactory
}

func (s *fakeStand) Close() error {
	return nil
}

func (s *fakeStand) Build(ctx context.Context, services []config.Service) error {
	s.f.built = services
	if s.f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func commandConfig(t *testing.T) config.Config {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(root, "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestInvalidInputDoesNotCreateRuntime(t *testing.T) {
	for _, args := range [][]string{
		{
			"up",
			"--agents",
			"2",
		},
		{
			"build",
			"missing",
		},
		{
			"build",
			"api-gateway",
			"api-gateway",
		},
		{
			"stop",
			"extra",
		},
		{"logs"},
		{"reset"},
		{"reset", "--yes", "extra"},
		{
			"logs",
			"api-gateway",
			"--tail",
			"-1",
		},
		{
			"status",
			"-o",
			"yaml",
		},
		{
			"up",
			"--timeout",
			"0s",
		},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := &fakeFactory{
				c: commandConfig(t),
			}
			cmd := NewHotelctlCommand(f, iostreams.IOStreams{
				Out:    io.Discard,
				ErrOut: io.Discard,
			}, "test")
			cmd.SetArgs(args)
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected input error")
			}
			if f.calls != 0 {
				t.Fatalf("invalid input created %d runtimes", f.calls)
			}
		})
	}
}

type unavailableFactory struct{}

func (unavailableFactory) Config() (config.Config, error) {
	return config.Config{}, fmt.Errorf("project unavailable")
}

func (unavailableFactory) NewStand(config.Config, iostreams.IOStreams) (stand.Interface, error) {
	panic("unexpected runtime")
}

func TestHelpAndVersionNeedNoProjectOrDocker(t *testing.T) {
	for _, args := range [][]string{
		{"--help"},
		{"--version"},
		{"config", "--help"},
		{"config", "set", "--help"},
		{"config", "get", "--help"},
		{
			"build",
			"--help",
		},
		{
			"kubectl",
			"--help",
		},
		{"kubectl"},
	} {
		var output bytes.Buffer
		cmd := NewHotelctlCommand(unavailableFactory{}, iostreams.IOStreams{
			Out:    &output,
			ErrOut: io.Discard,
		}, "1.2.3")
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if output.Len() == 0 {
			t.Fatal("empty help/version")
		}
	}
}

func TestBuildSelectionAndDeadline(t *testing.T) {
	f := &fakeFactory{
		c:     commandConfig(t),
		block: true,
	}
	cmd := NewHotelctlCommand(f, iostreams.IOStreams{
		Out:    io.Discard,
		ErrOut: io.Discard,
	}, "test")
	cmd.SetArgs([]string{
		"build",
		"api-gateway",
		"--timeout",
		"1ms",
	})
	if err := cmd.Execute(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if len(f.built) != 1 || f.built[0].Name != "api-gateway" {
		t.Fatalf("wrong selection: %v", f.built)
	}
}

func (s *fakeStand) Smoke(context.Context) error {
	s.f.smoke = true
	return nil
}

func (s *fakeStand) BusinessSmoke(context.Context) error {
	s.f.business = true
	return nil
}

func TestBusinessSmokeRequiresExplicitFlag(t *testing.T) {
	for _, business := range []bool{false, true} {
		f := &fakeFactory{
			c: commandConfig(t),
		}
		cmd := NewHotelctlCommand(f, iostreams.IOStreams{
			Out:    io.Discard,
			ErrOut: io.Discard,
		}, "test")
		args := []string{"smoke"}
		if business {
			args = append(args, "--business")
		}
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		if !f.smoke || f.business != business {
			t.Fatalf("smoke=%t business=%t", f.smoke, f.business)
		}
	}
}

func (s *fakeStand) Reset(context.Context) error {
	s.f.reset = true
	return nil
}

func TestResetWithExplicitFlag(t *testing.T) {
	f := &fakeFactory{c: commandConfig(t)}
	cmd := NewHotelctlCommand(f, iostreams.IOStreams{
		Out:    io.Discard,
		ErrOut: io.Discard,
	}, "test")
	cmd.SetArgs([]string{"reset", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !f.reset {
		t.Fatal("сброс не вызван")
	}
}
