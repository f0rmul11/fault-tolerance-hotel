package echo

import echoV1 "check-in/internal/pkg/pb/check-in/echo/v1"

type Implementation struct {
	echoV1.UnimplementedEchoServiceServer
}

func NewEchoService() *Implementation {
	return &Implementation{}
}
