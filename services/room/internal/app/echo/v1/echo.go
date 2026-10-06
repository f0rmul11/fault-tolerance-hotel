package echo

import (
	"context"

	echoV1 "room/internal/pkg/pb/room/echo/v1"
)

func (i *Implementation) Echo(_ context.Context, req *echoV1.EchoRequest) (*echoV1.EchoResponse, error) {
	return &echoV1.EchoResponse{Message: req.GetMessage(), Service: "room"}, nil
}
