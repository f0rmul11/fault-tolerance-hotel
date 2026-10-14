package checkin

import (
	"check-in/internal/application/usecase"
	pb "check-in/internal/pkg/pb/check-in/checkin/v1"
)

type Server struct {
	pb.UnimplementedCheckinServiceServer
	usecases *usecase.Registry
}

func New(usecases *usecase.Registry) *Server {
	return &Server{usecases: usecases}
}
