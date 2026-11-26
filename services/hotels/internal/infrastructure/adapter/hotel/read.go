package hotel

import (
	"context"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/valkey-io/valkey-go"

	entity "hotels/internal/domain/entity/hotel"
	"hotels/internal/infrastructure/adapter/hotel/negative"
	"hotels/internal/infrastructure/adapter/hotel/payload"
	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"
)

func (a *Adapter) Get(ctx context.Context, id uuid.UUID, minRatingVersion int64) (*entity.Hotel, error) {
	ctx, span := traces.Start(ctx, "adapter.hotel.Get")
	defer span.End()

	// формируем ключ кеширования
	var key = payload.HotelKey(id)

	entry, cacheErr := a.client.Get(ctx, key)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// если ошибки нет и ключ найден и не "мертвый"
	if cacheErr == nil && !entry.IsTombstone() {
		// если попали на negative результат — отдаем его
		if negativeErr, ok := entry.Value.IsNegative(); ok {
			span.AddEvent("cache.negative")
			return nil, negativeErr
		}

		if entry.Value.RatingVersion >= minRatingVersion && !entry.Value.Expired(a.cfg.MaxHardTTL()) {
			// если ключ вышел за рамки soft TTL — делаем refresh
			if entry.Value.Expired(a.cfg.SoftTTL) {
				span.AddEvent("cache.stale")
				// Пока фоновый refresh работает, возвращаем уже stale карточку.
				a.refreshInBackground(ctx, id, entry)
			} else {
				span.AddEvent("cache.fresh")
			}

			return entry.Value.ConvertTo(), nil
		}
	}

	miss := errors.Is(cacheErr, valkey.Nil)

	// получаем данные из источника
	hotel, err := a.loadSnapshot(ctx, id, entry.Version)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err != nil {
		// Если ошибка каталога входит в список кешируемых и ключ отсутствует, кешируем негатив.
		if hotelErr, ttl, ok := negative.Classify(err, a.cfg.Negative); ok && miss && hotel == nil {
			// в фоне выставляем negative payload в кеш
			a.casInBackground(ctx, Entry{
				Key:        key,
				Value:      payload.ConvertFromError(hotelErr),
				Expiration: ttl, // NEGATIVE TTL
			})
		}
		return nil, err
	}

	rating := hotel.Rating()

	if minRatingVersion > 0 && (rating.Degraded || rating.Version < minRatingVersion) {
		return nil, app.New(
			app.WithCode(int(app.Unavailable)),
			app.WithMessage("Запрошенная версия рейтинга пока недоступна"),
		)
	}

	// Tombstone заполняет обработчик события. При ошибке кеша токен неизвестен.
	if !rating.Degraded && !entry.IsTombstone() && (cacheErr == nil || miss) {
		// асинхронно устанавливаем данные в кеш
		a.casInBackground(ctx, Entry{
			Key:     key,
			Value:   payload.ConvertFrom(hotel),
			Version: entry.Version,
		})
	}
	return hotel, nil
}
