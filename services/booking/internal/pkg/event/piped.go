package event

import (
	"context"

	"booking/internal/pkg/pipe"
)

// Flush адаптер для использования в pipe
func Flush[T any](buffer *Buffer) pipe.Func[T] {
	return func(ctx context.Context, value T) (T, error) {
		return value, buffer.Flush(ctx)
	}
}
