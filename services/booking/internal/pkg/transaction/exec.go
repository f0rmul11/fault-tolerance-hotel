package transaction

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/trace"

	"booking/internal/pkg/telemetry/traces"
	"booking/internal/pkg/transaction/abstract"
)

func Exec(ctx context.Context, callback func(context.Context) error) (err error) {
	// Polling transactions have no request parent and must not create idle traces.
	if trace.SpanContextFromContext(ctx).IsValid() {
		var span trace.Span
		ctx, span = traces.Start(ctx, "transaction.Exec")
		defer span.End()
		defer func() {
			traces.RecordError(span, err)
		}()
	}

	if abstract.TxFromContext(ctx) != nil {
		return callback(ctx)
	}
	ctx, finish := abstract.ContextWithTx(ctx, nil)
	defer func() {
		if p := recover(); p != nil {
			_ = finish(errors.New("паника в транзакции"))
			panic(p)
		}
		err = finish(err)
	}()
	return callback(ctx)
}
