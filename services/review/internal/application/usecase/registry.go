package usecase

import (
	"review/internal/application/usecase/rating/get"
	"review/internal/infrastructure/storage"
)

type Registry struct {
	GetHotelRatings *get.UseCase
}

func NewRegistry(stores *storage.Registry) *Registry {
	return &Registry{GetHotelRatings: get.New(stores.Ratings)}
}
