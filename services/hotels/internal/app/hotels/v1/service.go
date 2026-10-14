package hotels

import (
	"hotels/internal/application/usecase"
	pb "hotels/internal/pkg/pb/hotels/hotels/v1"
)

type Service struct {
	pb.UnimplementedHotelsServiceServer
	usecases *usecase.Registry
}

func New(usecases *usecase.Registry) *Service {
	return &Service{usecases: usecases}
}
