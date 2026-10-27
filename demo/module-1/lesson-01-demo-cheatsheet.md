# Урок 1 — шпаргалка Swagger / Postman

Базовый URL: `http://localhost:8080`. В Swagger нажать **Try it out**, заполнить заголовки и body заранее; после `ACTIVE` — **Execute**. В Postman: Body → raw → JSON.

Трейс: `x-trace-id` из **ответа** → `http://localhost:16686/trace/<x-trace-id>`. В запрос его добавлять не нужно. Экспорт может занять несколько секунд.

Команды сбоя выполнять из `/Users/d.bulykin/Desktop/faut-tolerance-course/hotel`. Правило снимается через `duration` или Ctrl+C.

## 1. Таймаут, но бронь создана

### Подготовка — проверить свободные номера

**POST /v1/rooms/availability**

Заголовки для этого запроса и всех запросов Booking ниже:

```http
Content-Type: application/json
X-Demo-Role: GUEST
X-Demo-Actor: eda02a57-8eb4-4d48-bc05-706c85747e9a
```

```json
{
  "hotel_id": "cdd4685e-c610-4e7e-95ee-c51fb6807400",
  "starts_at": "2026-11-27T12:00:00Z",
  "ends_at": "2026-11-28T12:00:00Z"
}
```

**200** → выбрать номер из `room_ids`. Ниже используется `"1"`: если его нет в ответе, подставить свободный ID.

### Создать бронь во время сбоя

**POST /v1/bookings** — те же заголовки.

```json
{
  "room_id": "1",
  "starts_at": "2026-11-27T12:00:00Z",
  "ends_at": "2026-11-28T12:00:00Z",
  "idempotency_key": "281cb155-a727-4e56-b63d-ad1a97716166"
}
```

Сначала заполнить запрос, затем включить сбой:

```bash
./bin/hotelctl fault api-gateway booking --direction response --slowdown 8s --duration 10s
```

Сразу после `ACTIVE` отправить запрос → **504 примерно через 5 с**. Открыть трейс: Booking обработал запрос, Gateway не дождался ответа.

### После снятия сбоя — повторить и прочитать

Повторить **POST /v1/bookings** с **точно тем же телом и заголовками** → **200**, `booking.status = CONFIRMED`. Скопировать `booking.id`.

**GET /v1/bookings/{booking_id}** — подставить полученный ID. Заголовки гостя те же, тела нет → **200**.

Сам по себе успешный повтор не доказывает создание до 504: для этого во время разбора показать исходный трейс и сохранение в Booking.

**Повторная репетиция:** новый `idempotency_key` и свободный номер/период. Простой повтор этой фикстуры вернёт старую бронь. Даты примерные: перед уроком выбрать будущие и одинаковые в availability и создании.

Для сравнения задержки запроса использовать новую фикстуру:

```bash
./bin/hotelctl fault api-gateway booking --slowdown 8s --duration 10s
```

Ожидается **504**, запрос создания не дошёл до Booking.

## 2. Недоступен рейтинг

Запросы публичные: дополнительных заголовков и тела нет.

| Запрос | Во время сбоя | После снятия |
| --- | --- | --- |
| GET /v1/hotels/cdd4685e-c610-4e7e-95ee-c51fb6807400 | 503 | 200 |
| GET /v1/hotels | 503 | 200 |
| GET /v1/rooms/1 | 200 | 200 |

```bash
./bin/hotelctl fault hotels review --unavailable --duration 10s
```

После `ACTIVE` запросить отель, затем номер. Если не успел за 10 с — повторить включение сбоя.

## 3. Бронь есть, на ресепшене её нет

До остановки Kafka проверить availability как в сценарии 1, но на **2026-11-29T12:00:00Z — 2026-11-30T12:00:00Z**. При необходимости заменить номер ниже на свободный.

```bash
./bin/hotelctl stop kafka
```

**POST /v1/bookings**

```http
Content-Type: application/json
X-Demo-Role: GUEST
X-Demo-Actor: eda02a57-8eb4-4d48-bc05-706c85747e9a
```

```json
{
  "room_id": "1",
  "starts_at": "2026-11-29T12:00:00Z",
  "ends_at": "2026-11-30T12:00:00Z",
  "idempotency_key": "1d3d6a78-8e08-4cf8-a046-6778a0f155b5"
}
```

Ожидается **200**. Сохранить `booking.id`; **GET /v1/bookings/{booking_id}** с заголовками гостя → **200**.

**GET /v1/check-in/bookings** — тела нет.

```http
X-Demo-Role: RECEPTION
X-Demo-Actor: eda02a57-8eb4-4d48-bc05-706c85747e9a
```

Query-параметры (в Swagger — отдельные поля):

| Параметр | Значение |
| --- | --- |
| starts_at | 2026-11-29T00:00:00Z |
| ends_at | 2026-11-30T00:00:00Z |

Ожидается **200**, но нашей брони в списке нет. Искать по сохранённому ID: другие брони в списке допустимы.

```bash
./bin/hotelctl start kafka
```

Повторять тот же GET Check-in: после доставки события бронь появляется. **Kafka сама не запустится.** Этот сценарий ещё нужно проверить на стенде.

## Снять сбои вручную

```bash
./bin/hotelctl fault api-gateway booking --clear
./bin/hotelctl fault hotels review --clear
./bin/hotelctl start kafka
```

[Swagger](http://localhost:8080/docs/) · [Jaeger](http://localhost:16686) · [Grafana](http://localhost:3000) · [Kafka UI](http://localhost:8090)
