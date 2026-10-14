package confirm

import (
	"context"

	"github.com/gofrs/uuid"

	"booking/internal/domain/entity"
	"booking/internal/domain/vo"
	"booking/internal/events/booking_confirmed"
	app "booking/internal/pkg/bookingerror"
	"booking/internal/pkg/event"
	"booking/internal/pkg/transaction"
)

type Storage interface {
	Create(context.Context, *entity.Booking, uuid.UUID) error
}

type Service struct {
	storage Storage
	flusher event.Flusher
}

func New(storage Storage, flusher event.Flusher) *Service {
	return &Service{
		storage: storage,
		flusher: flusher,
	}
}

// Confirm сохраняет бронь и событие одной транзакцией; пересечения запрещает БД.
func (s *Service) Confirm(ctx context.Context, booking *entity.Booking, key uuid.UUID, limits vo.Limits) (*entity.Booking, error) {
	err := transaction.Exec(ctx, func(txCtx context.Context) error {
		if !limits.Allows(booking.Period()) {
			return app.NewBusinessErr("Длительность бронирования не соответствует условиям номера")
		}

		if err := s.storage.Create(txCtx, booking, key); err != nil {
			return err
		}

		buffer := event.New(s.flusher)
		buffer.Add(booking_confirmed.New(booking))
		return buffer.Flush(txCtx)
	})
	if err != nil {
		return nil, err
	}

	return booking, nil
}
