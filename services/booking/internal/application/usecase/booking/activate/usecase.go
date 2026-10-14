package activate

import (
	"context"

	"github.com/gofrs/uuid"

	"booking/internal/domain/entity"
	"booking/internal/events/booking_activated"
	"booking/internal/events/checkin_rejected"
	app "booking/internal/pkg/bookingerror"
	"booking/internal/pkg/event"
	"booking/internal/pkg/transaction"
)

type Storage interface {
	AcceptEvent(context.Context, uuid.UUID) (bool, error)
	Get(context.Context, uuid.UUID) (*entity.Booking, error)
	Save(context.Context, ...*entity.Booking) error
}

type UseCase struct {
	storage Storage
	flusher event.Flusher
}

func New(storage Storage, flusher event.Flusher) *UseCase {
	return &UseCase{
		storage: storage,
		flusher: flusher,
	}
}

func (s *UseCase) Execute(ctx context.Context, input Input) error {
	request := input.Request
	if err := request.Validate(); err != nil {
		return err
	}
	return transaction.Exec(ctx, func(txCtx context.Context) error {
		fresh, err := s.storage.AcceptEvent(txCtx, request.CorrelationID)
		if err != nil || !fresh {
			return err
		}
		booking, err := s.storage.Get(txCtx, request.BookingID)
		if err != nil {
			return err
		}
		buffer := event.New(s.flusher)
		changed, err := booking.Activate(request.RequestedAt)
		if err != nil {
			reason, _ := app.As(err)
			buffer.Add(checkin_rejected.New(booking, request, reason.Message()))
			return buffer.Flush(txCtx)
		}
		if changed {
			if err := s.storage.Save(txCtx, booking); err != nil {
				return err
			}
		}
		buffer.Add(booking_activated.New(booking, request))
		return buffer.Flush(txCtx)
	})
}
