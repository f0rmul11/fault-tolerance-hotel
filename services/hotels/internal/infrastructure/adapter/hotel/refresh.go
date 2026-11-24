package hotel

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"

	"hotels/internal/infrastructure/adapter/hotel/payload"
	"hotels/internal/pkg/cache"
	"hotels/internal/pkg/telemetry/traces"
)

// Refresh обновляет снимок по событию Review, защищая запись токеном версии кеша.
func (a *Adapter) Refresh(ctx context.Context, id uuid.UUID) error {
	ctx, span := traces.Start(ctx, "adapter.hotel.Refresh")
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, a.cfg.RefreshTimeout)
	defer cancel()

	key := payload.HotelKey(id)
	startedAt := time.Now()

	// 1. Устанавливаем tombstone и запоминаем токен для последующей записи.
	version, err := a.client.Cas(ctx, cache.NewTombstone[payload.Hotel, *payload.Hotel](
		key, a.cfg.HardTTL+a.cfg.RefreshTimeout,
	))
	if err != nil {
		return err
	}

	// 2. Читаем актуальные данные напрямую из источников, обходя кеш.
	hotel, err := a.loadSnapshot(ctx, id)
	if err != nil {
		return err
	}

	// Fallback без рейтинга не должен стать новым снимком или подтвердить событие.
	if hotel.Rating().Degraded {
		return errors.New("Не удалось обновить кеш: рейтинг временно недоступен")
	}

	// 3. Заменяем tombstone полным снимком, только если токен ещё актуален.
	// При конфликте возвращаем ошибку: старые данные не записываем через Set.
	return a.publish(ctx, key, hotel, version, startedAt)
}

// refreshInBackground запускает обновление с токеном исходного чтения.
// Объединение запросов через singleflight добавим в следующем уроке.
func (a *Adapter) refreshInBackground(ctx context.Context, id uuid.UUID, entry Entry) {
	// Завершение запроса не отменяет refresh; время загрузки ограничено отдельно.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.RefreshTimeout)

	go func() {
		defer cancel()

		ctx, span := traces.Start(ctx, "adapter.hotel.background_refresh")
		defer span.End()

		// Используем токен исходного чтения; tombstone здесь не нужен.
		startedAt := time.Now()
		hotel, err := a.loadSnapshot(ctx, id)
		if err == nil && hotel.Rating().Degraded {
			err = errors.New("Рейтинг временно недоступен")
		}
		if err == nil {
			err = a.publish(ctx, entry.Key, hotel, entry.Version, startedAt)
		}
		if err != nil {
			traces.RecordError(span, err)
			slog.DebugContext(ctx, "Фоновое обновление не опубликовано", "hotel_id", id, "error", err)
		}
	}()
}
