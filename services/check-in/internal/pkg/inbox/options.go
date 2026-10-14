package inbox

type Option func(outbox *Inbox)

func WithMetrics() Option {
	return func(_ *Inbox) {
	}
}
