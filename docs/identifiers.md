# Идентификаторы HotelFlow

- Hotel, Booking, CheckIn, guest/actor, request, idempotency key и correlation-id используют UUID. Новые UUID создаются через `uuid.NewV4()`. В seed и примерах используются заранее сгенерированные UUID v4, без шаблонов `10000000-...` и кодирования смысла в UUID.
- Системный ID Room — положительный `int64`/PostgreSQL `bigint`; в API и Kafka он передаётся десятичной строкой (`"1"`). Источник ID — последовательность `public.rooms`; ID уникален во всём каталоге. Booking/Check-in сохраняют его как bigint. Шлюз передаёт строку напрямую; владельцы разбирают её локально через strconv, без новых общих helpers.
- `number` — отдельное обозначение номера внутри отеля (`"101"`), уникальное только в пределах hotel_id. Оно может совпадать у разных отелей и не используется для бронирования или GiST-проверки. `room_type` — строковое значение типа (`"Standard"`/`"Comfort"`/`"Family"`), без ID/отдельной сущности категории.
- Booking сохраняет room_number и room_type как снимок из Hotels при создании брони, без изменения версии и без дополнительного сетевого запроса. Источник актуального каталога остаётся Hotels. Replay возвращает исходный снимок и не обновляет его. Событие booking.confirmed.v1 передаёт room_id строкой, room_number и room_type; Check-in сохраняет этот снимок. События смены статуса идентифицируют бронь её UUID, без повторной загрузки каталога.
- booking_id и ключ Kafka — UUID брони, не room_id и не number. correlation-id — отдельный UUID идемпотентности события, стабильный при повторной доставке outbox. Исторические записи Kafka и отчёты о прежних прогонах не переписываются как новые результаты; актуальные контракты и исходные миграции меняются inplace.

Пример payload события `booking.confirmed.v1` (остальные поля опущены):

```json
{
  "booking_id": "ad0cb779-e78d-4d5f-a60d-cdbfa8af862c",
  "payload": {
    "room_id": "1",
    "room_number": "101",
    "room_type": "Standard"
  }
}
```

События `booking.activated.v1` и `booking.canceled.v1`, а также запросы Check-in продолжают ссылаться на UUID брони. Состав брони и сведения о номере уже известны подписчику из `booking.confirmed.v1`.

Текущие исходные миграции рассчитаны на чистую учебную БД. Запуск новой версии поверх старой схемы UUID room_id не поддерживается. Для просмотра нового формата в AKHQ необходим запуск обновлённого стенда на актуальной схеме; старые записи Kafka сохраняют прежний формат.

Проверка: [identifiers-verification.md](identifiers-verification.md).

В Kafka-событиях используй явные имена: booking_id в envelope, booking_version для версии Booking; checkin_id/checkin_version в payload для CheckIn. Не используй aggregate_id/aggregate_version и безымянные id/version в wire-контракте. Не дублируй booking_id, request_id и версии между envelope и payload. correlation_id идентифицирует событие, causation_id — исходную команду, request_id — запрос для трассировки. Внутреннее API скопированного event/outbox не переименовывается.
