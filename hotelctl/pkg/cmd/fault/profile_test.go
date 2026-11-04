package fault

import "testing"

func TestDelayProfileFlag(t *testing.T) {
	f := newFakeFactory()
	args := []string{"api-gateway", "booking", "--delay-profile", "8:150ms,2:800ms", "--duration", "1s"}
	if err := execute(t, f, args); err != nil {
		t.Fatal(err)
	}
	if len(f.received.DelayProfile) != 2 {
		t.Fatalf("profile not passed to runtime: %+v", f.received)
	}
	for _, extra := range [][]string{{"--percent", "8"}, {"--slowdown", "1s"}, {"--unavailable"}, {"--clear"}, {"--direction", "response"}} {
		f := newFakeFactory()
		if err := execute(t, f, append(append([]string{}, args...), extra...)); err == nil || f.runtimeCalls != 0 {
			t.Fatalf("invalid combination %v reached runtime: %v", extra, err)
		}
	}
}
