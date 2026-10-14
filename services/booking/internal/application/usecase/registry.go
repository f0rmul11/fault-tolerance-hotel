package usecase

import (
	"booking/internal/application/service"
	"booking/internal/application/usecase/booking/activate"
	"booking/internal/application/usecase/booking/available"
	"booking/internal/application/usecase/booking/cancel"
	"booking/internal/application/usecase/booking/create"
	"booking/internal/application/usecase/booking/get"
	"booking/internal/infrastructure/gateway/hotels"
	"booking/internal/infrastructure/storage/booking"
	"booking/internal/pkg/event"
)

type Registry struct {
	ActivateBooking   *activate.UseCase
	CancelBooking     *cancel.UseCase
	CreateBooking     *create.UseCase
	GetBooking        *get.UseCase
	CheckAvailability *available.UseCase
}

func NewRegistry(storage *booking.Storage, rooms *hotels.Client, services *service.Registry, flusher event.Flusher) *Registry {
	return &Registry{
		ActivateBooking:   activate.New(storage, flusher),
		CancelBooking:     cancel.New(storage, flusher),
		CreateBooking:     create.New(storage, rooms, services.BookingConfirm),
		GetBooking:        get.New(storage),
		CheckAvailability: available.New(rooms, storage),
	}
}
