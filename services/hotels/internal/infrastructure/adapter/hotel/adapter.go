package hotel

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/uuid"

	"hotels/config"
	entity "hotels/internal/domain/entity/hotel"
	dto "hotels/internal/dto/hotel"
	"hotels/internal/infrastructure/adapter/hotel/payload"
	"hotels/internal/pkg/cache"
	app "hotels/internal/pkg/hotelserror"
)

type Entry = cache.Entry[payload.Hotel, *payload.Hotel]

type Storage interface {
	Get(context.Context, uuid.UUID) (*entity.Hotel, error)
}

type Review interface {
	GetHotelRatings(context.Context, []uuid.UUID) (map[uuid.UUID]dto.Rating, error)
}

// Adapter кеширует единый снимок отеля и его пользовательского рейтинга.
type Adapter struct {
	client *cache.Client[payload.Hotel, *payload.Hotel]
	origin Storage
	review Review

	cfg config.Cache
}

// NewAdapter объединяет кеш и источники данных карточки отеля.
func NewAdapter(
	client *cache.Client[payload.Hotel, *payload.Hotel],
	origin Storage,
	review Review,
	cfg config.Cache,
) *Adapter {
	return &Adapter{
		client: client,
		origin: origin,
		review: review,
		cfg:    cfg,
	}
}

// loadSnapshot допускает явную деградацию рейтинга, но не подменяет её старым значением.
func (a *Adapter) loadSnapshot(ctx context.Context, id uuid.UUID) (*entity.Hotel, error) {
	hotel, err := a.origin.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	ratings, err := a.review.GetHotelRatings(ctx, []uuid.UUID{id})
	if err != nil {
		if app.IsCode(err, app.Unavailable) || app.IsCode(err, app.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			hotel.SetRating(dto.Rating{Degraded: true})
			return hotel, nil
		}
		return nil, err
	}

	hotel.SetRating(ratings[id])
	return hotel, nil
}

// publish назначает deadlines только успешно прочитанному снимку и публикует его через CAS.
func (a *Adapter) publish(
	ctx context.Context,
	key string,
	hotel *entity.Hotel,
	version uint32,
	startedAt time.Time,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	value := payload.ConvertFrom(hotel)
	// Отсчитываем возраст от начала чтения: медленный источник не «омолаживает» данные.
	value.SoftDeadline = startedAt.Add(a.cfg.SoftTTL)
	value.HardDeadline = startedAt.Add(a.cfg.HardTTL)

	remaining := time.Until(value.HardDeadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}

	_, err := a.client.Cas(ctx, Entry{
		Key:     key,
		Value:   value,
		Version: version,
		// Короткий запас хранения сохраняет CAS-токен для незавершённых чтений.
		// Разрешение отдавать данные всё равно заканчивается строго по HardDeadline.
		Expiration: remaining + a.cfg.RefreshTimeout,
	})
	return err
}
