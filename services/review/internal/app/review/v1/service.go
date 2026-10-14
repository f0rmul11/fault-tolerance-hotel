package review

import (
	"review/internal/application/usecase"
	pb "review/internal/pkg/pb/review/review/v1"
)

type Service struct {
	pb.UnimplementedReviewServiceServer
	usecases *usecase.Registry
}

func New(usecases *usecase.Registry) *Service {
	return &Service{usecases: usecases}
}
