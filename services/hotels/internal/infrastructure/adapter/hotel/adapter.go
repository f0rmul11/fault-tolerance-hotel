package hotel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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
	origin Storage
	review Review

	cfg config.Cache

	loads  singlectx.Group[string, *entity.Hotel]
	writes singlectx.Group[string, uint32]

	client *cache.Client[payload.Hotel, *payload.Hotel]
}

// NewAdapter объединяет кеш и источники данных карточки отеля.
func NewAdapter(
	origin Storage,
	review Review,
	cfg config.Cache,
	client *cache.Client[payload.Hotel, *payload.Hotel],
) *Adapter {
	return &Adapter{
		origin: origin,
		review: review,
		cfg:    cfg,
		client: client,
	}
}

// loadSnapshot объединяет чтения одной карточки с одним CAS-токеном.
// После установки tombstone загрузка получает новый ключ singleflight.
func (a *Adapter) loadSnapshot(ctx context.Context, id uuid.UUID, version uint32) (*entity.Hotel, error) {
	snapshot, _, err := a.loads.Do(ctx, fmt.Sprintf("%s:%d", id, version), func(ctx context.Context) (*entity.Hotel, error) {
		ctx, cancel := context.WithTimeout(ctx, a.cfg.RefreshTimeout)
		defer cancel()

		hotel, err := a.origin.Get(ctx, id)
		if err != nil {
			return nil, err
		}

		ratings, err := a.review.GetHotelRatings(ctx, []uuid.UUID{id})
		rating := ratings[id]

		if err != nil {
			if !app.IsCode(err, app.Unavailable) &&
				!app.IsCode(err, app.DeadlineExceeded) &&
				!errors.Is(err, context.DeadlineExceeded) {
				// Каталог уже нашёл отель. Ошибка Review относится только к рейтингу.
				return hotel, err
			}
			rating = dto.Rating{Degraded: true}
		}

		hotel.SetRating(rating)
		return hotel, nil
	})
	return snapshot, err
}

func (a *Adapter) hotelEntry(key string, version uint32, hotel *entity.Hotel) Entry {
	return Entry{
		Key:        key,
		Value:      payload.ConvertFrom(hotel),
		Version:    version,
		Expiration: a.cfg.HardTTL,
	}
}

func (a *Adapter) casInBackground(ctx context.Context, entry Entry) {
	go func() {
		ctx := context.WithoutCancel(ctx)
		if err := a.cas(ctx, entry); err != nil && !app.IsCode(err, app.Conflict) {
			slog.WarnContext(ctx, "Не удалось записать карточку в кеш", "key", entry.Key, "error", err)
		}
	}()
}

// Для одного CAS-токена объединяем записи одной версии рейтинга или одной категории ошибки.
func (a *Adapter) cas(ctx context.Context, entry Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	flightKey := fmt.Sprintf("%s:%d:rating:%d", entry.Key, entry.Version, entry.Value.RatingVersion)
	if n := entry.Value.Negative; n != nil {
		flightKey = fmt.Sprintf("%s:%d:error:%d", entry.Key, entry.Version, n.Code)
	}

	_, _, err := a.writes.Do(ctx, flightKey, func(ctx context.Context) (uint32, error) {
		ctx, cancel := context.WithTimeout(ctx, a.cfg.RefreshTimeout)
		defer cancel()

		return a.client.Cas(ctx, entry)
	})
	return err
}
