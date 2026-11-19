package retry

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var attemptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "notification_retry_attempts_total",
	Help: "Completed retry handler invocations by outcome.",
}, []string{"topic", "outcome"})

var publicationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "notification_retry_publications_total",
	Help: "Retry and DLQ publication attempts by destination, reason and broker acknowledgement.",
}, []string{"topic", "reason", "outcome"})

func (r *Router) InitTopicMetrics(topic string) {
	if r.attempt > 0 {
		attemptsTotal.WithLabelValues(topic, "succeeded")
		attemptsTotal.WithLabelValues(topic, "failed")
	}
}

func (r *Router) ObserveAttempt(topic string, err error) {
	if r.attempt == 0 {
		return
	}
	outcome := "succeeded"
	if err != nil {
		outcome = "failed"
	}
	attemptsTotal.WithLabelValues(topic, outcome).Inc()
}

func observePublication(topic, reason string, err error) {
	outcome := "published"
	if err != nil {
		outcome = "failed"
	}
	publicationsTotal.WithLabelValues(topic, reason, outcome).Inc()
}
