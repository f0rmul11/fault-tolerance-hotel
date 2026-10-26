package transaction

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"booking/internal/pkg/transaction/abstract"
)

func TestTransactionSpanIncludesCommitFailureAndSkipsIdlePolling(t *testing.T) {
	previous := otel.GetTracerProvider()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	if err := Exec(t.Context(), func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(exporter.GetSpans()) != 0 {
		t.Fatal("idle polling generated a root transaction trace")
	}
	failure := errors.New("commit failed")
	ctx, parent := provider.Tracer("test").Start(t.Context(), "request")
	defer parent.End()
	tx := &fakeTx{commitErr: failure}
	err := Exec(ctx, func(ctx context.Context) error {
		abstract.TxFromContext(ctx).WithTx(ctx, func(context.Context) abstract.Transaction {
			return tx
		})
		return nil
	})
	if !errors.Is(err, failure) || tx.commits != 1 {
		t.Fatal("transaction commit behavior changed")
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Parent.SpanID() != parent.SpanContext().SpanID() || spans[0].Status.Code != codes.Error {
		t.Fatal("transaction span ended before commit or lost its parent/error")
	}
}
