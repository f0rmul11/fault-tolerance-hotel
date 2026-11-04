package hedge

import (
	"testing"
	"time"
)

func TestWarmupUsesCompleteLatencyWindow(t *testing.T) {
	s := NewStrategySingleD(MainConfig{WindowSize: 3, Warmup: true, Percentile: 1, HedgeBudget: 0.2, ResetPeriod: time.Second})
	for _, latency := range []time.Duration{time.Millisecond, 150 * time.Millisecond} {
		s.latencyRingBuf.Append(latency)
		if _, probability := s.computeOptimalDelay(0); probability != 0 {
			t.Fatal("hedging started before the latency window was filled")
		}
	}
	s.latencyRingBuf.Append(800 * time.Millisecond)
	delay, probability := s.computeOptimalDelay(0)
	if delay != 800*time.Millisecond || probability != 1 {
		t.Fatalf("delay=%v probability=%v", delay, probability)
	}
}

func TestWarmupDisabledPreservesEarlyHedging(t *testing.T) {
	s := NewStrategySingleD(MainConfig{WindowSize: 1000, Percentile: .99, HedgeBudget: .2, ResetPeriod: time.Second})
	s.latencyRingBuf.Append(time.Millisecond)
	if _, probability := s.computeOptimalDelay(0); probability != 1 {
		t.Fatal("default strategy unexpectedly waits for a full window")
	}
}
