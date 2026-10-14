package checkinerror

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorThroughOuterWrapper(t *testing.T) {
	cause := errors.New("private database details")
	err := NewInternalError("Не удалось выполнить операцию", cause)
	wrapped := fmt.Errorf("operation: %w", err)
	typed, ok := As(wrapped)
	if !ok || typed != err || !IsCode(wrapped, Internal) {
		t.Fatal("typed error lost through outer wrapper")
	}
	if typed.Message() != "Не удалось выполнить операцию" || typed.Classification() != "internal" {
		t.Fatal("public message or classification lost")
	}
	if typed.BaseMessage() != cause.Error() {
		t.Fatal("diagnostic message lost")
	}
	if _, ok := As(nil); ok {
		t.Fatal("nil is not a service error")
	}
}
