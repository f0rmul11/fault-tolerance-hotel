package event

import (
	"context"
)

// ContextKey ключ для сохранения буфера в context
type contextKey int

// Key ключ
const key contextKey = 1

// Inject внедряет буфер в контекст
func Inject(ctx context.Context, buffer *Buffer) context.Context {
	return context.WithValue(ctx, key, buffer)
}

// WithContext создает буфер и складывает его в переданный контекст
func WithContext(ctx context.Context, flusher Flusher, ctxOpts ...Option) (*Buffer, context.Context) {
	buf := New(flusher, ctxOpts...)
	return buf, context.WithValue(ctx, key, buf)
}

// Extract извлекает буфер из контекста
func Extract(ctx context.Context) *Buffer {
	if buf, ok := ctx.Value(key).(*Buffer); ok {
		return buf
	}
	return nil
}
