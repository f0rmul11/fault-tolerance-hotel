package fault

import (
	"context"
	"errors"
	"io"
	"maps"
	"testing"
	"testing/synctest"
	"time"
)

type fakeRuntime struct {
	values map[string]string
	fail   bool
	calls  int
	cancel context.CancelFunc
}

func (r *fakeRuntime) Values(context.Context) (map[string]string, error) {
	return maps.Clone(r.values), nil
}

func (r *fakeRuntime) Modify(ctx context.Context, values map[string]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.calls++
	maps.Copy(r.values, values)
	if r.calls == 1 && r.cancel != nil {
		r.cancel()
	}
	if r.calls == 1 && r.fail {
		return errors.New("lost apply response")
	}
	return nil
}

func TestRuntimeFaultLifecycle(t *testing.T) {
	for _, mode := range []string{"request", "response", "unavailable", "cancel", "lost apply"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				o := Options{
					From:      "hotels",
					To:        "review",
					Direction: "request",
					Slowdown:  8 * time.Second,
					Duration:  10 * time.Second,
				}
				r := &fakeRuntime{values: map[string]string{"unrelated": "keep"}}
				switch mode {
				case "response":
					o.Direction = "response"
				case "unavailable":
					o.Unavailable, o.Slowdown = true, 0
				case "cancel":
					r.cancel = cancel
				case "lost apply":
					r.fail = true
				}
				started := time.Now()
				err := RunRuntime(ctx, r, o, io.Discard)
				wantErr := mode == "cancel" || mode == "lost apply"
				if (err != nil) != wantErr {
					t.Fatalf("error=%v", err)
				}
				if !wantErr && time.Since(started) != o.Duration {
					t.Fatal("fault window differs from duration")
				}
				o.Clear, o.Unavailable = true, false
				for key, expected := range o.Values() {
					if r.values[key] != expected {
						t.Fatalf("not cleaned: %s=%s", key, r.values[key])
					}
				}
				if r.calls != 2 || r.values["unrelated"] != "keep" {
					t.Fatal("cleanup touched unrelated state or did not run")
				}
			})
		})
	}
}

func TestActiveFaultIsNotOverwritten(t *testing.T) {
	o := Options{
		From:        "hotels",
		To:          "review",
		Direction:   "request",
		Unavailable: true,
		Duration:    time.Second,
	}
	r := &fakeRuntime{values: o.Values()}
	if err := RunRuntime(context.Background(), r, o, io.Discard); err == nil || r.calls != 0 {
		t.Fatal("existing fault was overwritten")
	}
	o.Clear, o.Unavailable, o.Duration = true, false, 0
	if err := RunRuntime(context.Background(), r, o, io.Discard); err != nil {
		t.Fatal(err)
	}
	if r.values["demo.hotels.review.request.abort_percent"] != "0" {
		t.Fatal("manual clear did not remove abort")
	}
}
