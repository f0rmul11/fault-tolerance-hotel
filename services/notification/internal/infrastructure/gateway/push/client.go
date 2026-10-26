package push

import (
	"context"
	"log/slog"

	"notification/internal/dto/push"
	"notification/internal/pkg/telemetry/traces"
)

// Client имитирует push-провайдера структурированным сообщением в stdout.
type Client struct {
	logger *slog.Logger
}

func NewClient(logger *slog.Logger) *Client {
	return &Client{
		logger: logger,
	}
}

func (c *Client) Send(ctx context.Context, message push.Message) error {
	ctx, span := traces.Start(ctx, "gateway.push.Send")
	defer span.End()

	c.logger.InfoContext(ctx, message.Title,
		"service", "notification",
		"channel", "push",
		"provider", "stdout",
		"correlation_id", message.CorrelationID.String(),
		"booking_id", message.BookingID.String(),
		"text", message.Text,
	)
	return nil
}
