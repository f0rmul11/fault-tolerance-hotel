package fault

import (
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"
)

func TestDelayProfile(t *testing.T) {
	for _, value := range []string{"", "8", "8:bad", "0:1s", "-1:1s", "101:1s", "60:1s,41:2s", "8:0s", "8:61s", "8:1us"} {
		if _, err := ParseDelayProfile(value); err == nil {
			t.Errorf("accepted invalid profile %q", value)
		}
	}
	profile, err := ParseDelayProfile("8:150ms,2:800ms")
	if err != nil {
		t.Fatal(err)
	}
	o := Options{From: "api-gateway", To: "booking", Direction: "request", Duration: time.Second, DelayProfile: profile}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := o.Values()["demo.api-gateway.booking.request.delay_profile"]; got != "8:150,2:800" {
		t.Fatalf("profile = %s", got)
	}
	for _, direction := range []string{"response", "handler"} {
		o.Direction = direction
		if o.Validate() == nil {
			t.Fatalf("accepted profile direction %s", direction)
		}
	}
}

func TestProfileRuntimeLifecycle(t *testing.T) {
	for _, mode := range []string{"duration", "cancel", "lost apply"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				o := Options{From: "api-gateway", To: "booking", Direction: "request", Duration: time.Second, DelayProfile: []DelayStep{{Percent: 8, Delay: 150 * time.Millisecond}, {Percent: 2, Delay: 800 * time.Millisecond}}}
				r := &fakeRuntime{values: map[string]string{"demo.delay_profile_supported": "1", "unrelated": "keep"}}
				if mode == "cancel" {
					r.cancel = cancel
				}
				r.fail = mode == "lost apply"
				err := RunRuntime(ctx, r, o, io.Discard)
				if (err != nil) != (mode != "duration") {
					t.Fatalf("error = %v", err)
				}
				if r.values["demo.api-gateway.booking.request.profile_percent"] != "0" || r.values["demo.api-gateway.booking.request.delay_profile"] != "disabled" || r.values["unrelated"] != "keep" {
					t.Fatalf("profile cleanup failed: %v", r.values)
				}
			})
		})
	}
}

func TestProfileRequiresCapabilityAndBlocksOtherFaults(t *testing.T) {
	o := Options{From: "api-gateway", To: "booking", Direction: "request", Duration: time.Second, DelayProfile: []DelayStep{{Percent: 8, Delay: time.Second}}}
	r := &fakeRuntime{values: map[string]string{}}
	if RunRuntime(t.Context(), r, o, io.Discard) == nil || r.calls != 0 {
		t.Fatal("profile applied to an unsupported proxy")
	}
	r.values = o.Values()
	o.DelayProfile = nil
	o.Unavailable = true
	if RunRuntime(t.Context(), r, o, io.Discard) == nil || r.calls != 0 {
		t.Fatal("active profile overwritten by abort")
	}
	if err := ClearRuntime(t.Context(), r, o); err != nil {
		t.Fatal(err)
	}
	if r.values["demo.api-gateway.booking.request.profile_percent"] != "0" {
		t.Fatal("clear left active profile")
	}
}
