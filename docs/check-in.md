# Заселение и отмена

Booking владеет бронью, её статусом и занятостью номера. Check-in владеет `CheckIn`: ожидаемым прибытием и оформлением заселения. `booking_id` связывает эти две сущности; данные гостя, номера и периода нужны ресепшн. Check-in не хранит сущность Booking или её версию. Сервисы обмениваются событиями через два топика одного Kafka-кластера, без синхронных вызовов друг друга.

## Ручки

| HTTP | Назначение |
| --- | --- |
| GET `/v1/check-in/bookings` | Заселения EXPECTED/ARRIVING, дата начала которых попадает в диапазон |
| POST `/v1/check-in/bookings/{booking_id}/activate` | Начать оформление прибытия |
| POST `/v1/check-in/bookings/{booking_id}/cancel` | Запросить отмену подтверждённой или активированной брони |

Роли RECEPTION/OPERATOR, identity проверяет interceptor. `starts_at` и `ends_at` в GET передаются вместе в RFC3339. Диапазон полуоткрытый, фильтр по **началу** проживания. При отсутствии границ берутся текущие сутки в `timezone: Europe/Moscow`. Вчерашний заезд не входит, даже если проживание продолжается. Уже заселённые и отменяемые/отменённые записи не входят.

POST: `{"idempotency_key":"<UUID>"}`. Ответ HTTP 200 содержит `checkin`: id, booking_id, room_id, guest_id, starts_at, ends_at, status, version, failure. Первый ответ — ARRIVING или CANCELING: запрос сохранён с outbox, ответ Booking ещё ожидается. Повтор с тем же оператором и ключом возвращает **текущее состояние того же CheckIn**, включая итоговый статус. Замена booking_id/action для занятого ключа даёт HTTP 409. Старый ключ активации после отмены возвращает CANCELED и не запускает активацию снова.

Список возвращает `checkins`. gRPC-контракт локален Check-in; gateway содержит только Clay-прокси. Один RPC — один файл с mapping внутри метода и один use case. Kafka-сценарии имеют свои use case.

## События

| Событие | Топик | Действие получателя |
| --- | --- | --- |
| `booking.confirmed.v1` | `booking.events.v1` | Check-in регистрирует EXPECTED; повтор не сбрасывает состояние |
| `checkin.arrival-requested.v1` | `checkin.events.v1` | Booking активирует бронь |
| `checkin.cancellation-requested.v1` | `checkin.events.v1` | Booking отменяет бронь |
| `booking.activated.v1` | `booking.events.v1` | Check-in подтверждает заселение |
| `booking.canceled.v1` | `booking.events.v1` | Check-in подтверждает отмену |
| `booking.checkin-rejected.v1` | `booking.events.v1` | Check-in возвращает предыдущее состояние с русским пояснением |

Пакеты успешных ответов — `booking_activated` и `booking_canceled`; универсальное changed удалено. Запрос содержит checkin_id, booking_id, checkin_version, actor_id, request_id, requested_at. Ответ содержит checkin_id, booking_id, checkin_version, causation_id; отказ дополнительно содержит message. Это корреляция конкретного действия CheckIn, без снимка/версии сущности Booking.

У каждого события **свой correlation-id**, UUID, созданный один раз при формировании outbox. Он одинаков в Kafka header `x-correlation-id` и поле `correlation_id` envelope. Reply получает новый ID, `causation_id` ссылается на ID запроса. Повтор отправки из outbox сохраняет ID. Ни HTTP request_id, ни Kafka offset не являются ключом события.

## Состояния и атомарность

- Booking: CONFIRMED → ACTIVE; CONFIRMED/ACTIVE → CANCELED. Только CANCELED освобождает период в EXCLUDE. Активация проверяет время действия оператора в `[starts_at,ends_at)`, поэтому задержка Kafka не превращает своевременное прибытие в отказ.
- CheckIn: EXPECTED → ARRIVING → CHECKED_IN. Отмена: EXPECTED/CHECKED_IN → CANCELING → CANCELED. Отказ Booking возвращает предыдущее состояние и записывает failure. Одновременно принимается только одно действие; CAS применяется при каждом переходе.
- HTTP use case одной транзакцией вставляет технический ключ `(actor_id,key)`, сохраняет CheckIn и outbox. Явных row/advisory locks в Check-in нет. Конкурирующая запись проигрывает CAS и откатывает свой ключ и outbox. Таблица requests не имеет жизненного цикла и не является сущностью Operation.
- Подписчик сначала сохраняет событие в inbox по correlation-id, затем подтверждает Kafka offset. Уникальность `(correlation_id,topic)` закреплена индексом. Пакет inbox скопирован из payment-service; adapter → inbox topic handler → event router → event-specific handler → use case.
- Booking одной транзакцией сохраняет переход и reply outbox. Если business commit выполнен, а processed_at inbox ещё не записан, inbox повторяет use case. Повторная активация ACTIVE и отмена CANCELED не меняют статус/версию; поздняя активация CANCELED возвращает бизнес-отказ и не восстанавливает бронь. Ответ может сформироваться снова с новым correlation_id и прежним causation_id. Check-in игнорирует ответы для уже завершённой версии действия. Отдельной таблицы обработанных событий нет.
- Check-in применяет reply только к соответствующей ожидаемой версии собственного CheckIn. Старый или повторный ответ не изменяет новое состояние. Повтор создания использует unique booking_id и не перезаписывает существующий CheckIn.

Kafka key — booking_id, hash partitioner, отдельные consumer groups; новые группы начинают с oldest. Доставка at-least-once. Normal/error workers могут пересекаться, поэтому корректность опирается на ключи и CAS. Счётчик ошибок inbox не прекращает повторы и не является DLQ.

При отключении Kafka уже запущенные сервисы сохраняют изменения и outbox в своей БД. После восстановления обмен продолжается. В ожидании ответа CheckIn остаётся ARRIVING/CANCELING: локального таймера отмены нет. Старт runtime требует доступных БД и Kafka. Систему нельзя считать синхронной или обещать немедленную сходимость при бесконечном сетевом сбое.

## Разработка и проверка

Контракты генерируются Buf; DAO восстанавливает сущности через With/Option из xo-моделей. Схема и протокол изменены inplace: существующие учебные БД автоматически не мигрируются в новую модель. Генерация сама не сбрасывает данные.

Текущие проверки — [verification.md](verification.md); прежний отдельный прогон сохранён в [check-in-verification.md](check-in-verification.md). Команда hotelctl smoke --business проверяет полный обмен на локальном k3s. Основные автоматические тесты — unit, табличные проверки переходов/диапазонов/корреляции и имеющиеся lifecycle-тесты.

Идентификаторы и снимок номера в API/Kafka: [identifiers.md](identifiers.md).
