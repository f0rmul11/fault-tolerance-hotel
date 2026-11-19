package outbox

type Option func(*Outbox)

// WithMetrics включает метрики для outbox.
func WithMetrics() Option {
	return func(_ *Outbox) {
		registerMetrics()
	}
}
