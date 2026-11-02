package fault

import (
	"strconv"
	"testing"
)

func TestPercentFlag(t *testing.T) {
	for _, percent := range []int{-1, 0, 3, 100, 101} {
		t.Run(strconv.Itoa(percent), func(t *testing.T) {
			factory := newFakeFactory()
			err := execute(t, factory, []string{"api-gateway", "booking", "--slowdown", "800ms", "--duration", "1s", "--percent", strconv.Itoa(percent)})
			if percent < 0 || percent > 100 {
				if err == nil || factory.runtimeCalls != 0 {
					t.Fatalf("invalid percent reached runtime: %v", err)
				}
				return
			}
			if err != nil || factory.received.Percentage() != percent {
				t.Fatalf("percent=%d error=%v", factory.received.Percentage(), err)
			}
			values := factory.received.Values()
			if values["demo.api-gateway.booking.request.delay_percent"] != strconv.Itoa(percent) {
				t.Fatalf("wrong Envoy runtime values: %v", values)
			}
		})
	}
}

func TestClearRejectsPercent(t *testing.T) {
	factory := newFakeFactory()
	if err := execute(t, factory, []string{"api-gateway", "booking", "--clear", "--percent", "0"}); err == nil || factory.runtimeCalls != 0 {
		t.Fatalf("clear accepted percent: %v", err)
	}
}
