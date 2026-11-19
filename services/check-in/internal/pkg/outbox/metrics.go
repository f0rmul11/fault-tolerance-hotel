package outbox

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// Векторные метрики, деление метрик происходит по топику
	canNotHandleMessageByOutbox *prometheus.CounterVec // Метрика количества неудачно обработанных с помощью outbox сообщений
	outboxMessagesWritten       *prometheus.CounterVec // Метрика количества записанных в outbox сообщений
	outboxMessagesProcessed     *prometheus.CounterVec // Метрика количества обработанных с помощью outbox сообщений
	outboxMessagesLag           *prometheus.GaugeVec   // Метрика измерения лага сообщений в outbox (стрелка - убывает/возрастает)
	once                        sync.Once
)

func registerMetrics() {
	once.Do(func() {
		canNotHandleMessageByOutbox = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_can_not_handler_message_by_outbox_total",
		}, []string{"topic"})
		outboxMessagesWritten = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_messages_written_total",
		}, []string{"topic"})
		outboxMessagesProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "outbox_messages_processed_total",
		}, []string{"topic"})
		outboxMessagesLag = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "outbox_pending_messages",
		}, []string{"topic"})
		prometheus.MustRegister(
			canNotHandleMessageByOutbox,
			outboxMessagesWritten,
			outboxMessagesProcessed,
			outboxMessagesLag,
		)
	})
}

// incCanNotHandleMessageByOutbox увеличивает счетчик сообщений, при обработке которых возникла ошибка
func incCanNotHandleMessageByOutbox(topic string) {
	if canNotHandleMessageByOutbox != nil {
		canNotHandleMessageByOutbox.WithLabelValues(topic).Inc()
	}
}

// incOutboxMessagesWritten увеличивает счетчик сообщений записанных в хранилище outbox
func incOutboxMessagesWritten(topic string) {
	if outboxMessagesWritten != nil {
		outboxMessagesWritten.WithLabelValues(topic).Inc()
	}
}

// incOutboxMessagesProcessed увеличивает счетчик обработанных сообщений outbox
func incOutboxMessagesProcessed(topic string, count float64) {
	if outboxMessagesProcessed != nil {
		outboxMessagesProcessed.WithLabelValues(topic).Add(count)
	}
}

// setOutboxMessagesLag устанавливает значения количества необработанных сообщений outbox в топиках
func setOutboxMessagesLag(messagesLag map[string]int64) {
	if outboxMessagesLag != nil {
		outboxMessagesLag.Reset()
		for topic, value := range messagesLag {
			outboxMessagesLag.WithLabelValues(topic).Set(float64(value))
		}
	}
}
