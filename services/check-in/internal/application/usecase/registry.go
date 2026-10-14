package usecase

import (
	"time"

	"check-in/internal/application/usecase/checkin/activate"
	"check-in/internal/application/usecase/checkin/arrivals"
	"check-in/internal/application/usecase/checkin/cancel"
	"check-in/internal/application/usecase/checkin/confirm_arrival"
	"check-in/internal/application/usecase/checkin/confirm_cancellation"
	"check-in/internal/application/usecase/checkin/register"
	"check-in/internal/application/usecase/checkin/reject"
	"check-in/internal/infrastructure/storage/checkin"
	"check-in/internal/pkg/event"
)

type Registry struct {
	ListArrivals        *arrivals.UseCase
	ActivateCheckIn     *activate.UseCase
	CancelCheckIn       *cancel.UseCase
	RegisterCheckIn     *register.UseCase
	ConfirmArrival      *confirm_arrival.UseCase
	ConfirmCancellation *confirm_cancellation.UseCase
	RejectCheckIn       *reject.UseCase
}

func NewRegistry(storage *checkin.Storage, flusher event.Flusher, location *time.Location) *Registry {
	return &Registry{
		ListArrivals:        arrivals.New(storage, location),
		ActivateCheckIn:     activate.New(storage, flusher),
		CancelCheckIn:       cancel.New(storage, flusher),
		RegisterCheckIn:     register.New(storage),
		ConfirmArrival:      confirm_arrival.New(storage),
		ConfirmCancellation: confirm_cancellation.New(storage),
		RejectCheckIn:       reject.New(storage),
	}
}
