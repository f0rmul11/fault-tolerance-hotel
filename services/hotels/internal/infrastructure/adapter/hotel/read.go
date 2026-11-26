package hotel

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"
	"github.com/valkey-io/valkey-go"

	entity "hotels/internal/domain/entity/hotel"
	"hotels/internal/infrastructure/adapter/hotel/payload"
	app "hotels/internal/pkg/hotelserror"
	"hotels/internal/pkg/telemetry/traces"
)

// GetWithSoftTTL разделяет свежий снимок, допустимый stale и истёкшие данные.
func (a *Adapter) GetWithSoftTTL(ctx context.Context, id uuid.UUID, minRatingVersion int64) (*entity.Hotel, error) {
	ctx, span := traces.Start(ctx, "adapter.hotel.GetWithSoftTTL")
	defer span.End()

	key := payload.HotelKey(id)
	entry, cacheErr := a.client.Get(ctx, key)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := time.Now()

	// Снимок можно вернуть, только если он не инвалидирован и подходит клиенту.
	cacheHit := cacheErr == nil && !entry.IsTombstone()
	if cacheHit && entry.Value.CanServe(now, minRatingVersion) {
		if !now.Before(entry.Value.SoftDeadline) {
			// Возвращаем stale немедленно и запускаем обновление в фоне.
			a.refreshInBackground(ctx, id, entry)
			span.AddEvent("cache.stale")
		} else {
			span.AddEvent("cache.fresh")
		}

		return entry.Value.ConvertTo(), nil
	}

	// После hard deadline, при tombstone или недостаточной версии читаем источники синхронно.
	// Даже если Valkey ещё хранит байты, ответ получаем заново из источников.
	loadCtx, cancel := context.WithTimeout(ctx, a.cfg.RefreshTimeout)
	defer cancel()

	snapshot, err := a.loadSnapshot(loadCtx, id, entry.Version)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}

	hotel := &snapshot.hotel
	// Проверяем ответ источника до публикации: fallback не выполняет требование версии.
	if minRatingVersion > 0 && (hotel.Rating().Degraded || hotel.Rating().Version < minRatingVersion) {
		return nil, app.New(
			app.WithCode(int(app.Unavailable)),
			app.WithMessage("Запрошенная версия рейтинга пока недоступна"),
		)
	}

	if hotel.Rating().Degraded {
		span.AddEvent("cache.rating_unavailable")
		return hotel, nil
	}

	// При ошибке чтения кеша токен неизвестен. Tombstone заполняет только его владелец.
	if entry.IsTombstone() || (cacheErr != nil && !errors.Is(cacheErr, valkey.Nil)) {
		return hotel, nil
	}

	if err = a.publish(loadCtx, key, snapshot, entry.Version); err != nil {
		// Свежий ответ уже получен; проигрыш CAS не превращаем в безусловный SET.
		slog.DebugContext(ctx, "Снимок не опубликован в кеш", "hotel_id", id, "error", err)
	}

	return hotel, nil
}
