package service

import (
	"booking/internal/application/service/booking/confirm"
	"booking/internal/infrastructure/storage/booking"
	"booking/internal/pkg/event"
)

type Registry struct {
	BookingConfirm *confirm.Service
}

func NewRegistry(storage *booking.Storage, flusher event.Flusher) *Registry {
	return &Registry{
		BookingConfirm: confirm.New(storage, flusher),
	}
}
