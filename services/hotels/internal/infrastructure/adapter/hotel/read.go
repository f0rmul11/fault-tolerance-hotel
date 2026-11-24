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

// Get читает готовый снимок из кеша или собирает его из Storage и Review.
func (a *Adapter) Get(ctx context.Context, id uuid.UUID) (*entity.Hotel, error) {
	ctx, span := traces.Start(ctx, "adapter.hotel.Get")
	defer span.End()

	// формируем ключ
	key := payload.HotelKey(id)

	// Попадание в кеш позволяет обойти оба источника.
	entry, err := a.client.Get(ctx, key)
	if err == nil && !entry.IsTombstone() {
		return entry.Value.ConvertTo(), nil
	}

	// Отсутствующий ключ — обычный промах. Ошибка кеша не блокирует чтение.
	if err != nil && !errors.Is(err, valkey.Nil) {
		slog.WarnContext(ctx, "Не удалось прочитать отель из кеша", "hotel_id", id, "error", err)
	}

	hotel, err := a.loadSnapshot(ctx, id)
	if err != nil {
		return nil, err
	}

	// Неполный ответ и маркер инвалидации не заменяем обычным снимком.
	if hotel.Rating().Degraded || entry.IsTombstone() {
		return hotel, nil
	}

	err = a.client.Set(ctx, Entry{
		Key:        key,
		Value:      payload.ConvertFrom(hotel),
		Expiration: a.cfg.HardTTL,
	})
	if err != nil {
		// Данные уже получены: ошибка заполнения кеша не меняет результат чтения.
		slog.WarnContext(ctx, "Не удалось сохранить отель в кеш", "hotel_id", id, "error", err)
	}

	return hotel, nil
}

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
	startedAt := time.Now()
	loadCtx, cancel := context.WithTimeout(ctx, a.cfg.RefreshTimeout)
	defer cancel()

	hotel, err := a.loadSnapshot(loadCtx, id)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}

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

	if err := a.publish(loadCtx, key, hotel, entry.Version, startedAt); err != nil {
		// Свежий ответ уже получен; проигрыш CAS не превращаем в безусловный SET.
		slog.DebugContext(ctx, "Снимок не опубликован в кеш", "hotel_id", id, "error", err)
	}

	return hotel, nil
}

/*GetWithCAS — вариант для следующей демонстрации.

Инвалидатор должен записывать tombstone через Cas, а не удалять ключ через DEL.
Маркер должен сохраняться до завершения начатых чтений: после его истечения
или eviction отсутствие ключа снова позволит записать старый снимок.
CAS защищает запись в кеш; уже прочитанный ответ и задержку доставки события
этот алгоритм не делает актуальными автоматически.

func (a *Adapter) GetWithCAS(ctx context.Context, id uuid.UUID) (*entity.Hotel, error) {
	ctx, span := traces.Start(ctx, "adapter.hotel.GetWithCAS")
	defer span.End()

	key := payload.HotelKey(id)

	// Запоминаем состояние кеша до обращения к источникам.
	entry, err := a.client.Get(ctx, key)
	if err == nil && !entry.IsTombstone() {
		return entry.Value.ConvertTo(), nil
	}

	// При промахе или ошибке кеша читаем данные из источников.
	if err != nil && !errors.Is(err, valkey.Nil) {
		slog.WarnContext(ctx, "Не удалось прочитать отель из кеша", "hotel_id", id, "error", err)
	}

	hotel, err := a.loadSnapshot(ctx, id)
	if err != nil {
		return nil, err
	}

	// Tombstone означает, что идёт обновление: читатель не заполняет кеш.
	// Неполный ответ с недоступным рейтингом также не кешируем.
	if entry.IsTombstone() || hotel.Rating().Degraded {
		return hotel, nil
	}

	// Передаём токен, полученный при первом чтении кеша.
	// При промахе версия 0 разрешит запись только в отсутствующий ключ.
	_, err = a.client.Cas(ctx, Entry{
		Key:        key,
		Value:      payload.ConvertFrom(hotel),
		Expiration: a.cfg.HardTTL,
		Version:    entry.Version,
	})
	if err != nil {
		// Не повторяем запись старого снимка и не подменяем Cas обычным Set.
		slog.WarnContext(ctx, "Не удалось заполнить кеш через CAS", "hotel_id", id, "error", err)
	}

	return hotel, nil
}*/
