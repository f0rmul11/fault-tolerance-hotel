package loadshedding

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/platinummonkey/go-concurrency-limits/core"
	"github.com/platinummonkey/go-concurrency-limits/limit"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "load_shedding"

var (
	estimatedLimit = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "estimated_limit",
		Help:      "Current Vegas concurrency limit, published on every limit change.",
	}, []string{"handler"})
	vegasRTT = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "vegas_rtt_seconds",
		Help:      "Last RTT supplied to Vegas: minimum successful RTT in a DefaultLimiter sample window; excludes admission wait.",
	}, []string{"handler"})
	queueEstimate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "vegas_queue_estimate",
		Help:      "Vegas queue estimate reconstructed from the pre-update integer limit and RTT; may differ by one from the internal float calculation. Not the admission backlog. NaN when the baseline is refreshed.",
	}, []string{"handler"})
	admissions = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "admissions_total",
		Help:      "Completed admission attempts. Rejected includes backlog full, wait expiry and context cancellation; the library does not distinguish them.",
	}, []string{"handler", "result"})
	acquireDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "acquire_duration_seconds",
		Help:      "Time spent in QueueBlockingLimiter.Acquire across all admitted and rejected attempts, including admission overhead and any backlog wait; excludes handler execution.",
		Buckets:   []float64{0.0001, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.15, 0.19, 0.2, 0.205, 0.21, 0.25, 0.5, 1, 2, 5},
	}, []string{"handler"})
	liveMetrics = &liveCollector{}
)

func init() {
	prometheus.MustRegister(liveMetrics)
}

// The existing telemetry provider bridges the default Prometheus registry to OTLP.
// Suppliers are evaluated at collection time, so inflight/backlog also return to
// zero when traffic stops. Sample listeners alone cannot provide those gauges.
type liveCollector struct {
	states sync.Map
}

func (c *liveCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range liveDescriptors {
		ch <- desc
	}
}

func (c *liveCollector) Collect(ch chan<- prometheus.Metric) {
	c.states.Range(func(_, value any) bool {
		state := value.(*metricRegistry)
		for name, supplier := range state.suppliers {
			if value, ok := supplier(); ok {
				ch <- prometheus.MustNewConstMetric(liveDescriptors[name], prometheus.GaugeValue, value, state.handler)
			}
		}
		return true
	})
}

var liveDescriptors = map[string]*prometheus.Desc{
	"inflight":              prometheus.NewDesc(namespace+"_inflight", "Currently acquired strategy tokens, excluding admission backlog.", []string{"handler"}, nil),
	"backlog":               prometheus.NewDesc(namespace+"_backlog", "Current number of waiting entries in the library admission backlog.", []string{"handler"}, nil),
	"backlog_limit":         prometheus.NewDesc(namespace+"_backlog_limit", "Configured admission backlog capacity.", []string{"handler"}, nil),
	"vegas_min_rtt_seconds": prometheus.NewDesc(namespace+"_vegas_min_rtt_seconds", "Current Vegas no-load RTT baseline, including probe resets.", []string{"handler"}, nil),
	"vegas_alpha":           prometheus.NewDesc(namespace+"_vegas_alpha", "Alpha for the current integer limit, in estimated queued requests.", []string{"handler"}, nil),
	"vegas_beta":            prometheus.NewDesc(namespace+"_vegas_beta", "Beta for the current integer limit, in estimated queued requests.", []string{"handler"}, nil),
}

// metricRegistry adapts the public library instrumentation, without modifying
// its algorithm or queue. Suppliers are installed before publishing the state.
type metricRegistry struct {
	handler        string
	suppliers      map[string]core.MetricSupplier
	currentLimit   atomic.Int64
	sampleRTT      atomic.Int64
	sampleInflight atomic.Int64
	sampleBase     atomic.Int64
	sampleQueue    atomic.Int64
	sampleDrop     atomic.Bool
}

func newMetricRegistry(handler string, initialLimit int) *metricRegistry {
	m := &metricRegistry{
		handler:   handler,
		suppliers: make(map[string]core.MetricSupplier),
	}
	m.currentLimit.Store(int64(initialLimit))
	estimatedLimit.WithLabelValues(handler).Set(float64(initialLimit))
	for _, result := range []string{"admitted", "rejected"} {
		admissions.WithLabelValues(handler, result)
	}
	return m
}

type metricSample func(float64)

func (f metricSample) AddSample(value float64, _ ...string) {
	f(value)
}

func (m *metricRegistry) RegisterDistribution(_ string, _ ...string) core.MetricSampleListener {
	// The library's inflight distribution is a historical sample, not live load.
	// Its min_rtt distribution omits probe refreshes. Both have live suppliers.
	return &core.EmptyMetricSampleListener{}
}

func (m *metricRegistry) RegisterTiming(id string, _ ...string) core.MetricSampleListener {
	if strings.HasSuffix(id, "."+core.MetricRTT) {
		return metricSample(func(value float64) {
			vegasRTT.WithLabelValues(m.handler).Set(value / float64(time.Second))
		})
	}
	return &core.EmptyMetricSampleListener{}
}

func (m *metricRegistry) RegisterCount(_ string, _ ...string) core.MetricSampleListener {
	return &core.EmptyMetricSampleListener{}
}

func (m *metricRegistry) RegisterGauge(id string, supplier core.MetricSupplier, _ ...string) {
	switch id {
	case core.MetricQueueSize:
		m.suppliers["backlog"] = supplier
	case core.MetricQueueLimit:
		m.suppliers["backlog_limit"] = supplier
	}
}

func (*metricRegistry) Start() {
}

func (*metricRegistry) Stop() {
}

func (m *metricRegistry) limitChanged(next int) {
	previous := int(m.currentLimit.Swap(int64(next)))
	estimatedLimit.WithLabelValues(m.handler).Set(float64(next))
	if previous == next {
		return
	}
	// Vegas calls this while holding its mutex: never call its getters here.
	slog.Info("Изменён лимит load shedding",
		"handler", m.handler,
		"old_limit", previous,
		"new_limit", next,
		"sample_rtt_ms", float64(m.sampleRTT.Load())/float64(time.Millisecond),
		"baseline_rtt_ms", float64(m.sampleBase.Load())/float64(time.Millisecond),
		"queue_estimate", m.sampleQueue.Load(),
		"did_drop", m.sampleDrop.Load(),
		"sample_inflight", m.sampleInflight.Load(),
	)
}

type observedVegas struct {
	*limit.VegasLimit
	metrics *metricRegistry
}

func (v *observedVegas) OnSample(startTime, rtt int64, inFlight int, didDrop bool) {
	before := v.EstimatedLimit()
	baseline := v.RTTNoLoad()
	queue := math.Ceil(float64(before) * (1 - float64(baseline)/float64(rtt)))
	v.metrics.sampleRTT.Store(rtt)
	v.metrics.sampleInflight.Store(int64(inFlight))
	v.metrics.sampleBase.Store(baseline)
	v.metrics.sampleQueue.Store(int64(queue))
	v.metrics.sampleDrop.Store(didDrop)
	v.VegasLimit.OnSample(startTime, rtt, inFlight, didDrop)
	if baseline == 0 || v.RTTNoLoad() != baseline {
		queue = math.NaN()
	}
	queueEstimate.WithLabelValues(v.metrics.handler).Set(queue)
}

func (m *metricRegistry) acquire(ctx context.Context, limiter core.Limiter) (core.Listener, bool) {
	started := time.Now()
	listener, ok := limiter.Acquire(ctx)
	acquireDuration.WithLabelValues(m.handler).Observe(time.Since(started).Seconds())
	result := "rejected"
	if ok && listener != nil {
		result = "admitted"
	}
	admissions.WithLabelValues(m.handler, result).Inc()
	return listener, ok
}
