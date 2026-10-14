package hotels

import (
	pb "api-gateway/internal/pkg/pb/api-gateway/hotels/v1"
	external "api-gateway/internal/pkg/pb/external/hotels/hotels/v1"
)

type Implementation struct {
	pb.UnimplementedHotelsServiceServer
	external external.HotelsServiceClient
}

func NewHotelsService(external external.HotelsServiceClient) *Implementation {
	return &Implementation{external: external}
}
