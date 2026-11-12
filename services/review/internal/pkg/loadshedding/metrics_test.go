package loadshedding

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Exercise the real library callback and live suppliers: a callback that calls
// Vegas getters deadlocks, and sampled inflight metrics stay nonzero after drain.
func TestMetricsFollowVegasAndTokenRelease(t *testing.T) {
	const handler = "/metrics.test/Review"
	cfg := defaultConfig(MainConfig{})
	cfg.Vegas.InitialMaxInflightLimit = 40
	cfg.Vegas.Smoothing = 1
	cfg.Vegas.RTTNoLoadProbeMultiplier = 1000
	cfg.Vegas.AlphaFuncCoefficient = 3
	cfg.Vegas.BetaFuncCoefficient = 6
	state, err := newInternalState(cfg, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		liveMetrics.states.Delete(handler)
	})
	registry := prometheus.NewRegistry()
	registry.MustRegister(liveMetrics, estimatedLimit, vegasRTT)
	read := func(name string) float64 {
		t.Helper()
		families, err := registry.Gather()
		if err != nil {
			t.Fatal(err)
		}
		for _, family := range families {
			if family.GetName() != name {
				continue
			}
			for _, metric := range family.Metric {
				for _, label := range metric.Label {
					if label.GetName() == "handler" && label.GetValue() == handler {
						if metric.Gauge != nil {
							return metric.Gauge.GetValue()
						}
						return metric.Counter.GetValue()
					}
				}
			}
		}
		t.Fatalf("metric %s for %s is missing", name, handler)
		return 0
	}

	token, ok := state.metrics.acquire(context.Background(), state.queueLimiter)
	if !ok {
		t.Fatal("initial admission failed")
	}
	if got := read("load_shedding_inflight"); got != 1 {
		t.Fatalf("inflight after admission = %v, want 1", got)
	}
	token.OnIgnore()
	if got := read("load_shedding_inflight"); got != 0 {
		t.Fatalf("inflight after release = %v, want 0", got)
	}
	if got := read("load_shedding_backlog"); got != 0 {
		t.Fatalf("empty backlog = %v, want 0", got)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		state.limitAlg.OnSample(0, int64(100*time.Millisecond), 40, false)
		state.limitAlg.OnSample(0, int64(100*time.Millisecond), 40, false)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Vegas metrics callback deadlocked")
	}
	grown := read("load_shedding_estimated_limit")
	if grown <= 40 {
		t.Fatalf("limit did not grow: %v", grown)
	}
	state.limitAlg.OnSample(0, int64(400*time.Millisecond), int(grown), false)
	if got := read("load_shedding_estimated_limit"); got >= grown {
		t.Fatalf("limit did not decrease: before=%v, after=%v", grown, got)
	}
	if got := read("load_shedding_vegas_min_rtt_seconds"); got != 0.1 {
		t.Fatalf("baseline RTT = %v, want 0.1 seconds", got)
	}
	if got := read("load_shedding_vegas_rtt_seconds"); got != 0.4 {
		t.Fatalf("sample RTT = %v, want 0.4 seconds", got)
	}
}
