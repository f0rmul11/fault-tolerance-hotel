# Бронирование конкретного номера

Hotels хранит отели, номера, категории и min/max длительности бронирования. Booking хранит гостя, room_id, один непрерывный период и статус брони. Availability удалён: занятие номера и клиентская бронь — одна запись в БД Booking.

Успешный результат — сразу CONFIRMED, версия 1. Сохраняемых PENDING/UNCERTAIN, отдельного резерва и компенсаций нет. В БД либо сохранены бронь и событие, либо ничего. Потерянный ответ восстанавливается повтором с тем же ключом идемпотентности.

## Сценарий

1. Clay Gateway проверяет учебную роль и передаёт запрос в Booking.
2. Booking создаёт Period, проверяет вход и ищет операцию по `(guest_id, idempotency_key)`. Совпадающий повтор возвращает сохранённый результат без Hotels.
3. Booking получает условия номера из Hotels до транзакции.
4. `application/service/booking/confirm` вызывает `transaction.Exec`, без advisory lock и повторного чтения ключа.
5. Проверяет длительность через `Limits.Allows(Period)` и вставляет одну строку брони. PostgreSQL EXCLUDE не позволяет пересечь существующую бронь того же номера.
6. `event.Buffer.Flush(txCtx)` записывает событие в outbox. Commit фиксирует бронь, ключ и событие вместе.
7. Отдельный relay доставляет событие в Kafka. Kafka не вызывается из транзакции бронирования.

## Период и конкуренция

`Period` — полуоткрытый интервал `[starts_at, ends_at)`, UTC, точность до микросекунд. Начало произвольное. Нет слотов, квантования времени и дочерней таблицы. Минимум 12 часов задан seed Hotels; максимум зависит от номера (для семейного номера ID 11 — 7 суток). Это длительность брони, а не шаг сетки.

```sql
constraint bookings_room_period_excl exclude using gist (
    room_id with =,
    tstzrange(starts_at, ends_at, '[)') with &&
) where (status is distinct from 'CANCELED')
```

Расширение btree_gist предоставляет GiST-оператор равенства bigint. Для одного номера PostgreSQL запрещает пересечение периодов, в том числе при конкурентных INSERT/UPDATE и прямом SQL. Разные номера независимы. Периоды 00:00–12:00 и 12:00–00:00 следующего дня соседние и разрешены.

`room_id`, `starts_at`, `ends_at` обязательны. CHECK требует конечные границы и `starts_at < ends_at`: NULL/пустой интервал не могут обойти защиту. Это согласованное узкое исключение из прежнего ограничения на SQL constraints. Min/max длительность задаёт домен, FK и триггеров нет.

Room lock, поиск конфликтующих броней в Go и протокол «прочитал — проверил — записал» удалены. SQLSTATE 23P01 именно ограничения `bookings_room_period_excl` становится NewBusinessErr (FailedPrecondition) / HTTP 409. PostgreSQL также может завершить конкурирующую транзакцию deadlock 40P01: клиент получает Conflict и может повторить запрос с тем же ключом. Автоматических retry нет.

`CheckAvailability` получает hotel_id и период, загружает номера из Hotels, фильтрует условия длительности и исключает занятое время запросом в свою БД. Это снимок доступности: окончательное решение принимает EXCLUDE при записи.

CONFIRMED и ACTIVE занимают номер. Отмена в CANCELED освобождает период благодаря предикату EXCLUDE. Переход и ответное событие сохраняются одной транзакцией. Inbox может повторить обработку; доменные методы не повторяют смену статуса и версии, повторный reply допустим. [Сценарий Check-in](check-in.md).

## Идемпотентность

Клиент задаёт обязательный ненулевой UUID `idempotency_key`. UNIQUE `(guest_id, idempotency_key)` хранится вместе с бронью без TTL. Образец — module-2/transient-failures исходного стенда.

- Тот же ключ, номер и период возвращают ту же бронь в её текущем состоянии, без нового события. Эквивалентные часовые пояса дают одинаковый период.
- Другой номер или период с тем же ключом — Conflict / HTTP 409.
- Ключи разных гостей независимы. `request_id` — трассировка, он может меняться между попытками.
- Сохранённый повтор обрабатывается use case до вызова Hotels. Для конкурирующих INSERT unique по гостю и ключу возвращает проигравшему Conflict. Advisory lock и повторное чтение в confirm не используются.
- Ошибка до commit не занимает ключ. Потеря ответа на commit не означает rollback: нужно повторить исходный запрос, сохранив ключ.

EXCLUDE отвечает за занятость, UNIQUE и ключ — за повторы, outbox — за событие. CAS защищает изменения существующей брони при активации и отмене.

## API и события

Локальные контракты генерируются через Buf. Gateway — тонкий Clay-прокси. Booking: CreateBooking, GetBooking, CheckAvailability. Hotels: GetHotel, GetRoom, ListRooms.

- `POST /v1/bookings`: GUEST; room_id, starts_at, ends_at, idempotency_key.
- `GET /v1/bookings/{booking_id}`: GUEST читает свою бронь; RECEPTION/OPERATOR — любую.
- `POST /v1/rooms/availability`: hotel_id, starts_at, ends_at; роли GUEST/RECEPTION/OPERATOR; результат room_ids.

```sh
curl -sS http://localhost:8080/v1/bookings \
  -H 'Content-Type: application/json' \
  -H 'X-Demo-Role: GUEST' \
  -H 'X-Demo-Actor: de3dab97-8166-4908-97ab-ce3e20f7df75' \
  -d '{
    "idempotency_key":"e886b633-1e19-45fd-b06d-c2a17e61ccbf",
    "room_id":"1",
    "starts_at":"2026-10-14T00:00:00Z",
    "ends_at":"2026-10-14T12:00:00Z"
  }'
```

Успех — 200. Ошибки — Clay/gRPC Status JSON на русском: InvalidArgument → 422, конфликт/пересечение → 409, отказ зависимости → 503. Демо-заголовки не являются production-аутентификацией.

Событие `booking.confirmed.v1` в `booking.events.v1` содержит room_id, starts_at, ends_at и данные брони; ключ Kafka — ID брони. [AsyncAPI](../api/events.asyncapi.json). Повтор HTTP не создаёт второго события. Relay обеспечивает at-least-once: возможен повтор доставки того же correlation_id. Занятость номера не зависит от Kafka.

## Схема и генерация

Booking в собственной БД: `public.bookings`, `public.outbox` и `public.inbox`. Hotels в своей БД: `public.hotels` и `public.rooms`. DAO встраивает xo-модель и восстанавливает Booking через With/Option.

```sh
make booking-tools
make generate-booking-api
POSTGRES_DSN='...' make migrate-booking
POSTGRES_DSN='...' make xo-booking
```

Закреплённый xo установлен с узким patch обхода expression-index при генерации row-моделей. Генерация только читает БД; EXCLUDE не удаляется и данные не сбрасываются.

Миграции и контракты изменены inplace. Старую учебную БД нужно явно пересоздать перед запуском новой версии; обновление приложения не преобразует старую схему автоматически. `hotelctl reset --yes` очищает данные актуальной схемы и восстанавливает seed, но не применяет заново уже выполненную миграцию.

Текущие проверки — [verification.md](verification.md); прежний отдельный прогон — [period-booking-verification.md](period-booking-verification.md). Сетевые риски — [network-failure-audit.md](network-failure-audit.md).

Идентификаторы и снимок номера в API/Kafka: [identifiers.md](identifiers.md).
