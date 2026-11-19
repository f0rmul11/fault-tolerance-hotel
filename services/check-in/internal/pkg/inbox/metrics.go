package inbox

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// Векторные метрики, деление метрик происходит по топику
	canNotHandleMessageByInbox *prometheus.CounterVec // Метрика количества неудачно обработанных с помощью inbox сообщений
	panicHandleMessageByInbox  *prometheus.CounterVec // Метрика количества panic при обработке сообщений с помощью inbox
	inboxMessagesWritten       *prometheus.CounterVec // Метрика количества записанных в inbox сообщений
	inboxMessagesProcessed     *prometheus.CounterVec // Метрика количества обработанных с помощью inbox сообщений
	inboxMessagesLag           *prometheus.GaugeVec   // Метрика измерения лага сообщений в inbox (стрелка - убывает/возрастает)

	once sync.Once
)

func registerMetrics() {
	once.Do(func() {
		canNotHandleMessageByInbox = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "inbox_can_not_handler_message_by_inbox_total",
		}, []string{"topic"})
		panicHandleMessageByInbox = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "inbox_panic_handler_message_by_inbox",
		}, []string{"topic"})
		inboxMessagesWritten = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "inbox_inbox_messages_written_total",
		}, []string{"topic"})
		inboxMessagesProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "inbox_inbox_messages_processed_total",
		}, []string{"topic"})
		inboxMessagesLag = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "inbox_pending_messages",
		}, []string{"topic"})

		prometheus.MustRegister(
			canNotHandleMessageByInbox,
			panicHandleMessageByInbox,
			inboxMessagesWritten,
			inboxMessagesProcessed,
			inboxMessagesLag,
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

// setInboxMessagesLag устанавливает значения количества необработанных сообщений inbox в топиках
func setInboxMessagesLag(messagesLag map[string]int64) {
	if inboxMessagesLag != nil {
		inboxMessagesLag.Reset()
		for topic, value := range messagesLag {
			inboxMessagesLag.WithLabelValues(topic).Set(float64(value))
		}
	}
}

// incPanicHandleMessageByInbox увеличивает счетчик паник обработчика
func incPanicHandleMessageByInbox(topic string) {
	if panicHandleMessageByInbox != nil {
		panicHandleMessageByInbox.WithLabelValues(topic).Inc()
	}
}
