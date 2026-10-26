package echo

import (
	"context"

	echoV1 "notification/internal/pkg/pb/notification/echo/v1"
	"notification/internal/pkg/telemetry/traces"
)

func (i *Implementation) Echo(ctx context.Context, req *echoV1.EchoRequest) (*echoV1.EchoResponse, error) {
	ctx, span := traces.Start(ctx, "handler.echo.Echo")
	defer span.End()

	return &echoV1.EchoResponse{
		Message: req.GetMessage(),
		Service: "notification",
	}, nil
}
