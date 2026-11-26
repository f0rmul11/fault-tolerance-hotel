package hotel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid"
	singlectx "resenje.org/singleflight"

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

	loads  singlectx.Group[string, loadedSnapshot]
	writes singlectx.Group[string, uint32]
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

// Участники одной загрузки отсчитывают TTL от одного момента чтения источников.
type loadedSnapshot struct {
	hotel     entity.Hotel
	startedAt time.Time
}

// loadSnapshot объединяет чтения одной карточки с одним CAS-токеном.
// После установки tombstone загрузка получает новый ключ singleflight.
func (a *Adapter) loadSnapshot(ctx context.Context, id uuid.UUID, version uint32) (loadedSnapshot, error) {
	key := fmt.Sprintf("%s:%d", id, version)
	snapshot, _, err := a.loads.Do(ctx, key, func(ctx context.Context) (loadedSnapshot, error) {
		ctx, cancel := context.WithTimeout(ctx, a.cfg.RefreshTimeout)
		defer cancel()

		startedAt := time.Now()

		hotel, err := a.origin.Get(ctx, id)
		if err != nil {
			return loadedSnapshot{}, err
		}

		ratings, err := a.review.GetHotelRatings(ctx, []uuid.UUID{id})
		rating := ratings[id]

		if err != nil {
			if !app.IsCode(err, app.Unavailable) && !app.IsCode(err, app.DeadlineExceeded) && !errors.Is(err, context.DeadlineExceeded) {
				return loadedSnapshot{}, err
			}
			rating = dto.Rating{Degraded: true}
		}

		hotel.SetRating(rating)
		return loadedSnapshot{hotel: *hotel, startedAt: startedAt}, nil
	})
	return snapshot, err
}

// publish объединяет одновременные попытки CAS с одним ожидаемым токеном.
func (a *Adapter) publish(ctx context.Context, key string, snapshot loadedSnapshot, version uint32) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	flightKey := fmt.Sprintf("%s:%d", key, version)
	_, _, err := a.writes.Do(ctx, flightKey, func(ctx context.Context) (uint32, error) {
		ctx, cancel := context.WithTimeout(ctx, a.cfg.RefreshTimeout)
		defer cancel()

		value := payload.ConvertFrom(&snapshot.hotel)

		value.SoftDeadline = snapshot.startedAt.Add(a.cfg.SoftTTL)
		value.HardDeadline = snapshot.startedAt.Add(a.cfg.HardTTL)

		remaining := time.Until(value.HardDeadline)
		if remaining <= 0 {
			return 0, context.DeadlineExceeded
		}

		return a.client.Cas(ctx, Entry{
			Key:     key,
			Value:   value,
			Version: version,
			// Сохраняем CAS-токен до завершения уже начатых чтений.
			Expiration: remaining + a.cfg.RefreshTimeout,
		})
	})
	return err
}
