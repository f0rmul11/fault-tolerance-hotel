package fault

import "testing"

func TestHandlerDelayFlag(t *testing.T) {
	factory := newFakeFactory()
	err := execute(t, factory, []string{"hotels", "review", "--direction", "handler", "--slowdown", "8s", "--duration", "10s"})
	if err != nil {
		t.Fatal(err)
	}
	values := factory.received.Values()
	if values["demo.hotels.review.handler.delay_ms"] != "8000" || values["demo.hotels.review.handler.delay_percent"] != "100" || values["demo.hotels.review.request.delay_percent"] != "0" {
		t.Fatalf("unexpected runtime: %v", values)
	}
}

func TestHandlerDelayRejectsOtherEdges(t *testing.T) {
	factory := newFakeFactory()
	err := execute(t, factory, []string{"api-gateway", "booking", "--direction", "handler", "--slowdown", "8s", "--duration", "10s"})
	if err == nil || factory.runtimeCalls != 0 {
		t.Fatalf("unsupported handler delay reached runtime: %v", err)
	}
}
