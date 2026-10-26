package echo

import (
	"context"

	echoV1 "check-in/internal/pkg/pb/check-in/echo/v1"
	"check-in/internal/pkg/telemetry/traces"
)

func (i *Implementation) Echo(ctx context.Context, req *echoV1.EchoRequest) (*echoV1.EchoResponse, error) {
	ctx, span := traces.Start(ctx, "handler.echo.Echo")
	defer span.End()

	return &echoV1.EchoResponse{
		Message: req.GetMessage(),
		Service: "check-in",
	}, nil
}
