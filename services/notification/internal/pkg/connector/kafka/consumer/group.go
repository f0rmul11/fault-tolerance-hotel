package consumer

import (
	"context"

	"github.com/IBM/sarama"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"notification/internal/pkg/msgbus/subscriber/retry"
	"notification/internal/pkg/telemetry/traces"
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
			return nil
		}

		select {
		case message, ok := <-claim.Messages():
			if !ok {
				return nil
			}

			err := g.handleMessage(ctx, message)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return err
			}

			session.MarkMessage(message, "")
			// Подтверждаем offset после успешного handler; ошибки читает ConsumerGroup.Errors().
			session.Commit()
		case <-ctx.Done():
			return nil
		}
	}
}

func (g groupSubscriber) handleMessage(ctx context.Context, message *sarama.ConsumerMessage) (err error) {
	fields := retry.Read(message.Headers)
	ctx = traces.Extract(ctx, fields)
	ctx, span := traces.Start(ctx, "kafka.receive",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination.name", message.Topic),
			attribute.String("messaging.operation.type", "receive"),
			attribute.String("messaging.message.id", fields["x-correlation-id"]),
		),
	)
	defer span.End()
	defer func() {
		traces.RecordError(span, err)
	}()
	return g.messageHandler(ctx, message, fields)
}
