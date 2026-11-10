package ratelimit

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

// Labels ограничены номером правила и исходом; произвольные client IDs или
// method names из запросов не создают неограниченную кардинальность.
type ruleMetrics struct {
	allowed  prometheus.Counter
	rejected prometheus.Counter
	canceled prometheus.Counter
	wait     prometheus.Histogram
	tokens   *prometheus.Desc
	limit    *prometheus.Desc
	burst    *prometheus.Desc
}

func newRuleMetrics(index int) *ruleMetrics {
	rule := strconv.Itoa(index)
	counter := func(outcome string) prometheus.Counter {
		return prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "ratelimit_decisions_total",
			Help:        "Matched requests admitted, rejected or canceled before the handler; allowed does not imply handler success.",
			ConstLabels: prometheus.Labels{"rule": rule, "outcome": outcome},
		})
	}
	labels := prometheus.Labels{"rule": rule}
	return &ruleMetrics{
		allowed:  counter("allowed"),
		rejected: counter("rejected"),
		canceled: counter("canceled"),
		wait: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:        "ratelimit_wait_duration_seconds",
			Help:        "Time spent acquiring a token when waiting is configured, including failed waits.",
			Buckets:     []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
			ConstLabels: labels,
		}),
		tokens: prometheus.NewDesc("ratelimit_tokens", "Current token balance at collection time; negative means queued reservations.", nil, labels),
		limit:  prometheus.NewDesc("ratelimit_limit", "Token refill rate per second for this process.", nil, labels),
		burst:  prometheus.NewDesc("ratelimit_burst", "Token bucket capacity for this process.", nil, labels),
	}
}

// RegisterMetrics подключает метрики к существующему Prometheus -> OTLP bridge.
// Отдельный registry в тестах позволяет создавать независимые экземпляры.
func (l *Limiter) RegisterMetrics(registry prometheus.Registerer) error {
	if len(l.state.Load().rules) == 0 {
		return nil
	}
	return registry.Register(l)
}

func (l *Limiter) Describe(ch chan<- *prometheus.Desc) {
	for _, rule := range l.state.Load().rules {
		m := rule.metrics
		m.allowed.Describe(ch)
		m.rejected.Describe(ch)
		m.canceled.Describe(ch)
		m.wait.Describe(ch)
		ch <- m.tokens
		ch <- m.limit
		ch <- m.burst
	}
}

func (l *Limiter) Collect(ch chan<- prometheus.Metric) {
	for _, rule := range l.state.Load().rules {
		m := rule.metrics
		m.allowed.Collect(ch)
		m.rejected.Collect(ch)
		m.canceled.Collect(ch)
		m.wait.Collect(ch)
		ch <- prometheus.MustNewConstMetric(m.tokens, prometheus.GaugeValue, rule.limiter.Tokens())
		ch <- prometheus.MustNewConstMetric(m.limit, prometheus.GaugeValue, float64(rule.limiter.Limit()))
		ch <- prometheus.MustNewConstMetric(m.burst, prometheus.GaugeValue, float64(rule.limiter.Burst()))
	}
}
