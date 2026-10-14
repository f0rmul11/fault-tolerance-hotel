package activate

import (
	"context"
	"time"

	"github.com/gofrs/uuid"

	"check-in/internal/domain/entity"
	checkindto "check-in/internal/dto/checkin"
	"check-in/internal/events/arrival_requested"
	app "check-in/internal/pkg/checkinerror"
	"check-in/internal/pkg/event"
	"check-in/internal/pkg/transaction"
)

type Storage interface {
	Get(context.Context, uuid.UUID) (*entity.CheckIn, error)
	ClaimRequest(context.Context, checkindto.Request) (bool, error)
	Save(context.Context, ...*entity.CheckIn) error
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

func (s *UseCase) Execute(ctx context.Context, input Input) (*entity.CheckIn, error) {
	if input.BookingID == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Не указан идентификатор брони")
	}
	if input.IdempotencyKey == uuid.Nil {
		return nil, app.NewInvalidArgumentErr("Не указан ключ идемпотентности")
	}
	var result *entity.CheckIn
	err := transaction.Exec(ctx, func(txCtx context.Context) error {
		fresh, err := s.storage.ClaimRequest(
			txCtx,
			checkindto.Request{
				ActorID:   input.Context.ActorID,
				Key:       input.IdempotencyKey,
				BookingID: input.BookingID,
				Action:    entity.Arrive,
			},
		)
		if err != nil {
			return err
		}
		result, err = s.storage.Get(txCtx, input.BookingID)
		if err != nil || !fresh {
			return err
		}
		if err := result.Arrive(input.Context.ActorID, input.Context.RequestID, time.Now().UTC()); err != nil {
			return err
		}
		if err := s.storage.Save(txCtx, result); err != nil {
			return err
		}
		buffer := event.New(s.flusher)
		buffer.Add(arrival_requested.New(result))
		return buffer.Flush(txCtx)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
