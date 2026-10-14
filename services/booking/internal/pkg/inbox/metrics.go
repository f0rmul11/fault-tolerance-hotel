package inbox

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

const namespace = "inbox"

var (
	// Векторные метрики, деление метрик происходит по топику
	canNotHandleMessageByInbox *prometheus.CounterVec // Метрика количества неудачно обработанных с помощью inbox сообщений
	panicHandleMessageByInbox  *prometheus.CounterVec // Метрика количества panic при обработке сообщений с помощью inbox
	inboxMessagesWritten       *prometheus.CounterVec // Метрика количества записанных в inbox сообщений
	inboxMessagesProcessed     *prometheus.CounterVec // Метрика количества обработанных с помощью inbox сообщений

	once sync.Once
)

// registerMetrics ...
func registerMetrics() {
	once.Do(func() {
		canNotHandleMessageByInbox = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "can_not_handler_message_by_inbox_total",
		}, []string{"topic"})

		panicHandleMessageByInbox = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "panic_handler_message_by_inbox",
		}, []string{"topic"})

		inboxMessagesWritten = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "inbox_messages_written_total",
		}, []string{"topic"})

		inboxMessagesProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "inbox_messages_processed_total",
		}, []string{"topic"})

		prometheus.MustRegister(
			canNotHandleMessageByInbox,
			panicHandleMessageByInbox,
			inboxMessagesWritten,
			inboxMessagesProcessed,
		)
	})
}

// incCanNotHandleMessageByInbox увеличивает счетчик сообщений, при обработке которых возникла ошибка
func incCanNotHandleMessageByInbox(topic string) {
	if canNotHandleMessageByInbox != nil {
		canNotHandleMessageByInbox.WithLabelValues(topic).Inc()
	}
}

// incInboxMessagesWritten увеличивает счетчик сообщений записанных в хранилище inbox
func incInboxMessagesWritten(topic string) {
	if inboxMessagesWritten != nil {
		inboxMessagesWritten.WithLabelValues(topic).Inc()
	}
}

// incInboxMessagesProcessed увеличивает счетчик обработанных сообщений inbox
func incInboxMessagesProcessed(topic string, count float64) {
	if inboxMessagesProcessed != nil {
		inboxMessagesProcessed.WithLabelValues(topic).Add(count)
	}
}

// incCanNotHandleMessageByInbox увеличивает счетчик сообщений, при обработке которых возникла паника
func incPanicHandleMessageByInbox(topic string) {
	if panicHandleMessageByInbox != nil {
		panicHandleMessageByInbox.WithLabelValues(topic).Inc()
	}
}
