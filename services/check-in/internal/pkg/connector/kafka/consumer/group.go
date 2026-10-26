package consumer

import (
	"context"
	"log/slog"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"check-in/internal/pkg/telemetry/traces"
)

type groupSubscriber struct {
	messageHandler MessageHandler
	setup          func(session sarama.ConsumerGroupSession) error
	cleanup        func(session sarama.ConsumerGroupSession) error
}

func (g groupSubscriber) Setup(session sarama.ConsumerGroupSession) error {
	if g.setup != nil {
		return g.setup(session)
	}
	return nil
}

func (g groupSubscriber) Cleanup(session sarama.ConsumerGroupSession) error {
	if g.cleanup != nil {
		return g.cleanup(session)
	}
	return nil
}

func (g groupSubscriber) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	ctx := session.Context()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		select {
		case message, ok := <-claim.Messages():
			if !ok {
				return nil
			}

			err := g.handleMessage(ctx, session, message)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				slog.Error("handle kafka message", "error", err.Error())
				return err
			}

			session.MarkMessage(message, "")
			// Синхронно делаем commit нашего offset'а
			// Если не вызвать session.Commit() -> будет срабатывать асинхронный коммит раз в интервал
			session.Commit()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (g groupSubscriber) handleMessage(ctx context.Context, session sarama.ConsumerGroupSession, message *sarama.ConsumerMessage) (err error) {
	headers := make(map[string]string, len(message.Headers))
	for _, header := range message.Headers {
		if header != nil {
			headers[string(header.Key)] = string(header.Value)
		}
	}
	ctx = traces.Extract(ctx, headers)
	ctx, span := traces.Start(ctx, "kafka.receive",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination.name", message.Topic),
			attribute.String("messaging.operation.type", "receive"),
			attribute.String("messaging.message.id", headers["x-correlation-id"]),
		),
	)
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()
	return g.messageHandler(ctx, session, message)
}
