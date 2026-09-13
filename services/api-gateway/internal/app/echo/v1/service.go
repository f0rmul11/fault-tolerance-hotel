package echo

import echoV1 "api-gateway/internal/pkg/pb/api-gateway/echo/v1"

type Implementation struct {
	echoV1.UnimplementedEchoServiceServer
}

func NewEchoService() *Implementation {
	return &Implementation{}
}
