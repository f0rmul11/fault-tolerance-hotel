package checkin

import (
	pb "api-gateway/internal/pkg/pb/api-gateway/checkin/v1"
	external "api-gateway/internal/pkg/pb/external/check-in/checkin/v1"
)

type Implementation struct {
	pb.UnimplementedCheckinServiceServer
	external external.CheckinServiceClient
}

func NewCheckinService(client external.CheckinServiceClient) *Implementation {
	return &Implementation{external: client}
}
