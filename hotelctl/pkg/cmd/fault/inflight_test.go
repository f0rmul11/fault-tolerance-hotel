package fault

import "testing"

func TestInflightFlags(t *testing.T) {
	factory := newFakeFactory()
	err := execute(t, factory, []string{
		"hotels", "review", "--direction", "handler",
		"--slowdown", "20ms", "--delay-per-inflight", "20ms",
		"--max-delay", "8s", "--duration", "3m",
	})
	if err != nil {
		t.Fatal(err)
	}
	values := factory.received.Values()
	if values["demo.hotels.review.handler.delay_per_inflight_ms"] != "20" || values["demo.hotels.review.handler.max_delay_ms"] != "8000" {
		t.Fatal(values)
	}
}

func TestInflightFlagsRejectMissingMaximum(t *testing.T) {
	factory := newFakeFactory()
	err := execute(t, factory, []string{
		"hotels", "review", "--direction", "handler",
		"--slowdown", "20ms", "--delay-per-inflight", "20ms", "--duration", "3m",
	})
	if err == nil || factory.runtimeCalls != 0 {
		t.Fatalf("missing maximum reached runtime: %v", err)
	}
}
