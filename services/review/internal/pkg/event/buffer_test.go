package event

import (
	"context"
	"errors"
	"testing"
)

type fakeFlusher struct {
	err    error
	events Events
}

func (f *fakeFlusher) Flush(_ context.Context, events Events) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, events...)
	return nil
}

func TestFailedFlushPreservesBuffer(t *testing.T) {
	for _, stage := range []string{
		"conversion",
		"storage",
	} {
		t.Run(stage, func(t *testing.T) {
			fail := true
			cause := errors.New("failed")
			flusher := &fakeFlusher{}
			buffer := New(flusher)
			buffer.Add(func(_ context.Context, batch Events) (Events, error) {
				if fail && stage == "conversion" {
					return nil, cause
				}
				return append(batch, Event{
					EntityID: "booking",
				}), nil
			})
			if stage == "storage" {
				flusher.err = cause
			}
			if err := buffer.Flush(context.Background()); !errors.Is(err, cause) {
				t.Fatal(err)
			}
			fail = false
			flusher.err = nil
			if err := buffer.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(flusher.events) != 1 {
				t.Fatal("lost pending event")
			}
			if err := buffer.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(flusher.events) != 1 {
				t.Fatal("successful flush was not cleared")
			}
		})
	}
}
