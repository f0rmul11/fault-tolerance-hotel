package fault

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	faultctl "github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/fault"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/stand"
)

type fakeFactory struct {
	config       config.Config
	runtimeCalls int
	faultCalls   int
	closed       bool
	received     faultctl.Options
}

func newFakeFactory() *fakeFactory {
	return &fakeFactory{
		config: config.Config{
			Services: []config.Service{
				{Name: "api-gateway"},
				{Name: "booking"},
				{Name: "hotels"},
				{Name: "review"},
				{Name: "check-in"},
			},
		},
	}
}

func (f *fakeFactory) Config() (config.Config, error) {
	return f.config, nil
}

func (f *fakeFactory) NewStand(config.Config, iostreams.IOStreams) (stand.Interface, error) {
	f.runtimeCalls++
	return &fakeStand{
		factory: f,
	}, nil
}

type fakeStand struct {
	stand.Interface
	factory *fakeFactory
}

func (s *fakeStand) Fault(_ context.Context, options faultctl.Options) error {
	s.factory.faultCalls++
	s.factory.received = options
	return nil
}

func (s *fakeStand) Close() error {
	s.factory.closed = true
	return nil
}

func execute(t *testing.T, factory *fakeFactory, args []string) error {
	t.Helper()
	command := NewCmdFault(factory, iostreams.IOStreams{
		Out:    io.Discard,
		ErrOut: io.Discard,
	})
	command.SilenceErrors, command.SilenceUsage = true, true
	command.SetArgs(args)
	return command.Execute()
}

func TestInvalidFaultInputDoesNotOpenRuntime(t *testing.T) {
	tests := map[string][]string{
		"no_services":          {},
		"one_service":          {"hotels"},
		"three_services":       {"hotels", "review", "booking", "--unavailable", "--duration", "10s"},
		"reverse_edge":         {"review", "hotels", "--unavailable", "--duration", "10s"},
		"missing_mode":         {"hotels", "review", "--duration", "10s"},
		"missing_duration":     {"hotels", "review", "--unavailable"},
		"zero_duration":        {"hotels", "review", "--unavailable", "--duration", "0s"},
		"negative_duration":    {"hotels", "review", "--unavailable", "--duration", "-1s"},
		"malformed_duration":   {"hotels", "review", "--unavailable", "--duration", "ten"},
		"zero_slowdown":        {"hotels", "review", "--slowdown", "0s", "--duration", "10s"},
		"oversized_slowdown":   {"hotels", "review", "--slowdown", "61s", "--duration", "10s"},
		"two_modes":            {"hotels", "review", "--unavailable", "--slowdown", "8s", "--duration", "10s"},
		"response_unavailable": {"hotels", "review", "--unavailable", "--direction", "response", "--duration", "10s"},
		"invalid_direction":    {"hotels", "review", "--slowdown", "8s", "--direction", "both", "--duration", "10s"},
		"short_timeout":        {"hotels", "review", "--unavailable", "--duration", "10s", "--timeout", "1s"},
		"clear_with_mode":      {"hotels", "review", "--clear", "--unavailable"},
		"clear_with_slowdown":  {"hotels", "review", "--clear", "--slowdown", "8s"},
		"clear_with_duration":  {"hotels", "review", "--clear", "--duration", "10s"},
		"clear_invalid_edge":   {"review", "hotels", "--clear"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			factory := newFakeFactory()
			if err := execute(t, factory, args); err == nil {
				t.Fatal("invalid input was accepted")
			}
			if factory.runtimeCalls != 0 {
				t.Fatal("invalid input opened the stand runtime")
			}
		})
	}
}

func TestFaultModesReachStandWithoutLosingDirectionOrDuration(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want faultctl.Options
	}{
		{
			name: "request_delay",
			args: []string{"api-gateway", "booking", "--slowdown", "8s", "--duration", "10s"},
			want: faultctl.Options{
				From:      "api-gateway",
				To:        "booking",
				Direction: "request",
				Slowdown:  8 * time.Second,
				Duration:  10 * time.Second,
			},
		},
		{
			name: "response_delay",
			args: []string{"api-gateway", "booking", "--direction", "response", "--slowdown", "8s", "--duration", "10s"},
			want: faultctl.Options{
				From:      "api-gateway",
				To:        "booking",
				Direction: "response",
				Slowdown:  8 * time.Second,
				Duration:  10 * time.Second,
			},
		},
		{
			name: "unavailable",
			args: []string{"hotels", "review", "--unavailable", "--duration", "10s"},
			want: faultctl.Options{
				From:        "hotels",
				To:          "review",
				Direction:   "request",
				Duration:    10 * time.Second,
				Unavailable: true,
			},
		},
		{
			name: "clear_without_duration",
			args: []string{"hotels", "review", "--clear"},
			want: faultctl.Options{
				From:      "hotels",
				To:        "review",
				Direction: "request",
				Clear:     true,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			factory := newFakeFactory()
			if err := execute(t, factory, test.args); err != nil {
				t.Fatal(err)
			}
			if factory.received != test.want {
				t.Fatalf("stand received %+v, want %+v", factory.received, test.want)
			}
			if factory.runtimeCalls != 1 || factory.faultCalls != 1 || !factory.closed {
				t.Fatalf("runtime=%d fault=%d closed=%v", factory.runtimeCalls, factory.faultCalls, factory.closed)
			}
		})
	}
}

func TestFaultRequiresBothServicesInStandConfiguration(t *testing.T) {
	factory := newFakeFactory()
	factory.config.Services = []config.Service{{Name: "hotels"}}
	if err := execute(t, factory, []string{"hotels", "review", "--unavailable", "--duration", "10s"}); err == nil {
		t.Fatal("missing downstream was accepted")
	}
	if factory.runtimeCalls != 0 {
		t.Fatal("missing downstream opened the stand runtime")
	}
}
