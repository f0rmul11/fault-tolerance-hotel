package config

import (
	"context"
	"errors"
	"testing"

	"hotels/internal/pkg/rtconfig"
)

type valueClient struct {
	rtconfig.Client
	value rtconfig.Value
	err   error
	ctx   context.Context
	key   string
}

func (c *valueClient) Value(ctx context.Context, key string) (rtconfig.Value, error) {
	c.ctx = ctx
	c.key = key

	return c.value, c.err
}

func TestInjectedFacadePreservesValueAndContext(t *testing.T) {
	t.Parallel()
	value := rtconfig.NewNilValue(errors.New("fixture value"))
	backend := &valueClient{value: value}
	client := NewClient(backend)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if actual := client.GetValue(ctx, RoomsListEnabled); actual != value {
		t.Fatal("facade replaced successful backend Value")
	}
	if backend.ctx != ctx || backend.key != "rooms_list_enabled" {
		t.Fatalf("facade changed call: ctx=%v, key=%q", backend.ctx, backend.key)
	}
}

func TestInjectedFacadeReturnsSafeErrorValue(t *testing.T) {
	t.Parallel()
	backend := &valueClient{err: rtconfig.ErrVariableNotFound}
	client := NewClient(backend)

	value := client.GetValue(context.Background(), RoomsListEnabled)
	if value == nil || !errors.Is(value.Err(), rtconfig.ErrVariableNotFound) || value.Bool() {
		t.Fatalf("expected safe missing-key value, got %v", value)
	}
}
