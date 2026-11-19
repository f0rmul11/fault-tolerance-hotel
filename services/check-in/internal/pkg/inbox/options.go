package inbox

type Option func(*Inbox)

// WithMetrics включает метрики для inbox.
func WithMetrics() Option {
	return func(_ *Inbox) {
		registerMetrics()
	}
}
