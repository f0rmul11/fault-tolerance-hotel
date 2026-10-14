package echo

import (
	"context"

	echoV1 "api-gateway/internal/pkg/pb/api-gateway/echo/v1"
)

func (i *Implementation) Echo(_ context.Context, req *echoV1.EchoRequest) (*echoV1.EchoResponse, error) {
	return &echoV1.EchoResponse{
		Message: req.GetMessage(),
	}, nil
}
