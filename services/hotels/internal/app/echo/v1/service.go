package echo

import echoV1 "hotels/internal/pkg/pb/hotels/echo/v1"

type Implementation struct {
	echoV1.UnimplementedEchoServiceServer
}

func NewEchoService() *Implementation {
	return &Implementation{}
}
