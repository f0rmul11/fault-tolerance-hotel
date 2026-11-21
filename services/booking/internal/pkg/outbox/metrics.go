package outbox

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const pendingMetricLimit = 10_000

var (
	messagesWritten = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "outbox_messages_written_total",
	}, []string{"topic"})

	messagesProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "outbox_messages_processed_total",
	}, []string{"topic"})

	failedBatches = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "outbox_can_not_handler_message_by_outbox_total",
		Help: "Число неудачных попыток отправить пачку сообщений.",
	}, []string{"topic"})

	pendingMessages = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "outbox_pending_messages",
		Help: "Число сообщений за курсорами топика, ограниченное 10000.",
	}, []string{"topic"})

	registerMetrics = sync.OnceFunc(func() {
		prometheus.MustRegister(messagesWritten, messagesProcessed, failedBatches, pendingMessages)
	})
)

// SendMetrics опрашивает очередь отдельно от отправки. Один опрос читает
// не более pendingMetricLimit ожидающих сообщений на топик.
func (s *Outbox) SendMetrics(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	for topic, handler := range s.handlers {
		pending := int64(0)
		partitions := handler.PartitionsCount()
		for partitionID := range partitions {
			count, err := s.storage.SamplePending(ctx, topic, partitionID, pendingMetricLimit-pending)
			if err != nil {
				pendingMessages.DeleteLabelValues(topic)
				slog.WarnContext(ctx, "Не удалось получить лаг outbox",
					"topic", topic, "partition", partitionID, "error", err,
				)
				break
			}

			pending += count
			if pending == pendingMetricLimit || partitionID == partitions-1 {
				pendingMessages.WithLabelValues(topic).Set(float64(pending))
				break
			}
		}
	}
}
