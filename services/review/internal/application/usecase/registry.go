package usecase

import (
	"review/internal/application/usecase/rating/add"
	"review/internal/application/usecase/rating/get"
	"review/internal/infrastructure/storage"
	"review/internal/pkg/event"
)

type Registry struct {
	GetHotelRatings *get.UseCase
	AddHotelRating  *add.UseCase
}

func NewRegistry(stores *storage.Registry, flusher event.Flusher) *Registry {
	return &Registry{
		GetHotelRatings: get.New(stores.Ratings),
		AddHotelRating:  add.New(stores.Ratings, flusher),
	}
}
