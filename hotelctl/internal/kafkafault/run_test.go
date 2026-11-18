package kafkafault

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

type fakeControl struct {
	calls           []string
	blockErr        error
	disconnectErr   error
	restoreErr      error
	restoreCanceled bool
}

func (c *fakeControl) BlockResponses(context.Context) error {
	c.calls = append(c.calls, "block")
	return c.blockErr
}

func (c *fakeControl) Disconnect(context.Context) error {
	c.calls = append(c.calls, "disconnect")
	return c.disconnectErr
}

func (c *fakeControl) Restore(ctx context.Context) error {
	c.calls = append(c.calls, "restore")
	c.restoreCanceled = ctx.Err() != nil
	return c.restoreErr
}

func TestRunDisconnectsOnlyAfterAppend(t *testing.T) {
	control := &fakeControl{}
	calls := 0
	var output bytes.Buffer
	err := Run(context.Background(), control, func(context.Context) (map[int]int64, error) {
		calls++
		if calls == 1 {
			return map[int]int64{
				0: 7,
				1: 4,
			}, nil
		}
		if !reflect.DeepEqual(control.calls, []string{"block"}) {
			t.Fatalf("response must be blocked before observing append: %v", control.calls)
		}
		return map[int]int64{
			0: 8,
			1: 4,
		}, nil
	}, Options{
		Topic:    "booking.events.v1",
		Duration: time.Millisecond,
	}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(control.calls, []string{"block", "disconnect", "restore"}) {
		t.Fatal(control.calls)
	}
	if !strings.Contains(output.String(), "P0 end 7 → 8") {
		t.Fatal(output.String())
	}
}

func TestRunAlwaysRestoresAfterArming(t *testing.T) {
	failure := errors.New("test failure")
	tests := []struct {
		name          string
		blockErr      error
		disconnectErr error
		restoreErr    error
		after         map[int]int64
		expected      []string
	}{
		{
			name:     "partial block",
			blockErr: failure,
			after:    map[int]int64{0: 8},
			expected: []string{"block", "restore"},
		},
		{
			name:          "partial disconnect",
			disconnectErr: failure,
			after:         map[int]int64{0: 8},
			expected:      []string{"block", "disconnect", "restore"},
		},
		{
			name:     "reset topic",
			after:    map[int]int64{0: 0},
			expected: []string{"block", "restore"},
		},
		{
			name:     "partition change",
			after:    map[int]int64{1: 8},
			expected: []string{"block", "restore"},
		},
		{
			name:       "restore failure",
			restoreErr: failure,
			after:      map[int]int64{0: 8},
			expected:   []string{"block", "disconnect", "restore"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			control := &fakeControl{
				blockErr:      tt.blockErr,
				disconnectErr: tt.disconnectErr,
				restoreErr:    tt.restoreErr,
			}
			calls := 0
			err := Run(context.Background(), control, func(context.Context) (map[int]int64, error) {
				calls++
				if calls == 1 {
					return map[int]int64{0: 7}, nil
				}
				return tt.after, nil
			}, Options{
				Topic:    "booking.events.v1",
				Duration: time.Millisecond,
			}, &bytes.Buffer{})
			if err == nil || !reflect.DeepEqual(control.calls, tt.expected) {
				t.Fatalf("err=%v calls=%v", err, control.calls)
			}
		})
	}
}

func TestCanceledObservationDoesNotDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	control := &fakeControl{}
	calls := 0
	err := Run(ctx, control, func(context.Context) (map[int]int64, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return map[int]int64{0: 7}, nil
	}, Options{
		Topic:    "booking.events.v1",
		Duration: time.Millisecond,
	}, &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) || control.restoreCanceled {
		t.Fatalf("err=%v cleanup canceled=%v", err, control.restoreCanceled)
	}
	if !reflect.DeepEqual(control.calls, []string{"block", "restore"}) {
		t.Fatal(control.calls)
	}
}
