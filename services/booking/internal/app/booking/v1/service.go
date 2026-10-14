package booking

import (
	"booking/internal/application/usecase"
	pb "booking/internal/pkg/pb/booking/booking/v1"
)

type Server struct {
	pb.UnimplementedBookingServiceServer
	usecases *usecase.Registry
}

func New(usecases *usecase.Registry) *Server {
	return &Server{usecases: usecases}
}
