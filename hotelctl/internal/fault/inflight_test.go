package fault

import (
	"context"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func inflightOptions() Options {
	return Options{
		From:             "hotels",
		To:               "review",
		Direction:        "handler",
		Slowdown:         20 * time.Millisecond,
		DelayPerInflight: 20 * time.Millisecond,
		MaxDelay:         8 * time.Second,
		Duration:         3 * time.Minute,
	}
}

func TestInflightValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Options)
	}{
		{name: "other direction", change: func(o *Options) {
			o.Direction = "response"
		}},
		{name: "other edge", change: func(o *Options) {
			o.From, o.To = "booking", "hotels"
		}},
		{name: "unavailable", change: func(o *Options) {
			o.Unavailable = true
		}},
		{name: "clear", change: func(o *Options) {
			o.Clear = true
		}},
		{name: "missing step", change: func(o *Options) {
			o.DelayPerInflight = 0
		}},
		{name: "negative step", change: func(o *Options) {
			o.DelayPerInflight = -time.Millisecond
		}},
		{name: "fractional step", change: func(o *Options) {
			o.DelayPerInflight = time.Microsecond
		}},
		{name: "missing maximum", change: func(o *Options) {
			o.MaxDelay = 0
		}},
		{name: "maximum below base", change: func(o *Options) {
			o.MaxDelay = time.Millisecond
		}},
		{name: "unbounded maximum", change: func(o *Options) {
			o.MaxDelay = 2 * time.Minute
		}},
		{name: "missing base", change: func(o *Options) {
			o.Slowdown = 0
		}},
	}
	valid := inflightOptions()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := inflightOptions()
			tt.change(&o)
			if o.Validate() == nil {
				t.Fatal("accepted invalid progressive delay")
			}
		})
	}
}

func TestInflightRuntimeLifecycle(t *testing.T) {
	for _, mode := range []string{"duration", "cancel", "lost apply"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				o := inflightOptions()
				values := o.Values()
				prefix := "demo.hotels.review.handler."
				if values[prefix+"delay_ms"] != "20" || values[prefix+"delay_per_inflight_ms"] != "20" || values[prefix+"max_delay_ms"] != "8000" || values[prefix+"delay_percent"] != "100" {
					t.Fatal(values)
				}
				r := &fakeRuntime{values: map[string]string{
					"demo.handler_delay_supported":          "1",
					"demo.handler_inflight_delay_supported": "1",
					"unrelated":                             "keep",
				}}
				if mode == "cancel" {
					r.cancel = cancel
				}
				r.fail = mode == "lost apply"
				err := RunRuntime(ctx, r, o, nil, io.Discard)
				if (err != nil) != (mode != "duration") {
					t.Fatal(err)
				}
				if r.values[prefix+"delay_percent"] != "0" || r.values[prefix+"delay_per_inflight_ms"] != "0" || r.values[prefix+"max_delay_ms"] != "0" || r.values["unrelated"] != "keep" || r.calls != 2 {
					t.Fatalf("incomplete cleanup: calls=%d values=%v", r.calls, r.values)
				}
			})
		})
	}
}

func TestInflightRequiresUpdatedEnvoy(t *testing.T) {
	r := &fakeRuntime{values: map[string]string{
		"demo.handler_delay_supported": "1",
	}}
	if RunRuntime(t.Context(), r, inflightOptions(), nil, io.Discard) == nil || r.calls != 0 {
		t.Fatal("old Envoy silently accepted inflight fault")
	}
}

func TestEnterEnablesInflightWithoutRemovingBaseline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		o := inflightOptions()
		o.InflightThreshold = 20
		o.DelayRamp = time.Minute
		o.ActivateOnEnter = true
		r := &fakeRuntime{values: map[string]string{
			"demo.handler_delay_supported":          "1",
			"demo.handler_inflight_delay_supported": "1",
			"demo.handler_inflight_ramp_supported":  "1",
		}}
		if err := RunRuntime(t.Context(), r, o, strings.NewReader("\n"), io.Discard); err != nil {
			t.Fatal(err)
		}
		if len(r.writes) != 3 {
			t.Fatalf("want baseline, activation and cleanup, got %d writes", len(r.writes))
		}
		prefix := "demo.hotels.review.handler."
		baseline, active, cleanup := r.writes[0], r.writes[1], r.writes[2]
		if baseline[prefix+"delay_ms"] != "20" || baseline[prefix+"delay_percent"] != "100" || baseline[prefix+"delay_per_inflight_ms"] != "0" {
			t.Fatalf("baseline already uses inflight or has no delay: %v", baseline)
		}
		if active[prefix+"delay_ms"] != "20" || active[prefix+"delay_percent"] != "100" ||
			active[prefix+"inflight_threshold"] != "20" || active[prefix+"delay_ramp_ms"] != "60000" ||
			active[prefix+"delay_ramp_start_ms"] == "0" || active[prefix+"delay_per_inflight_ms"] != "20" {
			t.Fatalf("incomplete activation: %v", active)
		}
		if cleanup[prefix+"delay_percent"] != "0" || cleanup[prefix+"delay_ramp_start_ms"] != "0" {
			t.Fatalf("incomplete cleanup: %v", cleanup)
		}
	})
}
