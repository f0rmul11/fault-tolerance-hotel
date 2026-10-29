package cmd

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/canary"
	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/iostreams"
)

func (s *fakeStand) Canary(_ context.Context, options canary.Options) error {
	s.f.canaryInput = options
	return nil
}

func TestCanaryValidationDoesNotOpenStand(t *testing.T) {
	for _, args := range [][]string{
		{"canary", "setup", "hotels"},
		{"canary", "step", "review", "30"},
		{"canary", "step", "review", "abc"},
		{"canary", "step", "review"},
		{"canary", "deploy", "review", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			f := &fakeFactory{c: commandConfig(t)}
			command := NewHotelctlCommand(f, iostreams.IOStreams{Out: io.Discard, ErrOut: io.Discard}, "test")
			command.SetArgs(args)
			if err := command.Execute(); err == nil || f.calls != 0 {
				t.Fatalf("err=%v, runtime calls=%d", err, f.calls)
			}
		})
	}
}

func TestCanaryDeployPassesRestart(t *testing.T) {
	f := &fakeFactory{c: commandConfig(t)}
	command := NewHotelctlCommand(f, iostreams.IOStreams{Out: io.Discard, ErrOut: io.Discard}, "test")
	command.SetArgs([]string{"canary", "deploy", "review", "--restart"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if f.canaryInput.Action != "deploy" || !f.canaryInput.Restart || f.calls != 1 {
		t.Fatalf("unexpected dispatch: %+v", f)
	}
}
