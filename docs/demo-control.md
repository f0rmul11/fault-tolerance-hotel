# Шпаргалка открытого урока: outbox и inbox

Все команды выполняются из корня `hotel`. Docker должен быть запущен.
Демонстрационная пара — Booking → Kafka → Check-in. У каждого сервиса своя БД.

## Перед эфиром

```bash
make build
./bin/hotelctl up --timeout 15m
sudo ./bin/hotelctl hosts --install  # один раз, для локальных имён БД
./bin/hotelctl status
./bin/hotelctl access
./bin/hotelctl smoke --business --timeout 3m
```

`smoke --business` оставляет отменённую тестовую бронь и события для просмотра.
`up` собирает и разворачивает стенд; во время демонстрации используйте `start`,
чтобы вернуть компонент без пересборки. Данные находятся на постоянных томах.

## Команды управления

```bash
./bin/hotelctl components                 # допустимые имена
./bin/hotelctl stop kafka                 # выключить Kafka и дождаться удаления Pod
./bin/hotelctl start kafka                # восстановить исходное число реплик
./bin/hotelctl stop booking-postgres      # выключить БД Booking, сохранив данные
./bin/hotelctl start booking-postgres
./bin/hotelctl restart check-in           # полный stop/start, без сборки
./bin/hotelctl stop booking check-in      # несколько компонентов
./bin/hotelctl start booking check-in
./bin/hotelctl logs booking -f
./bin/hotelctl logs kafka -f
./bin/hotelctl status
```

Имена: `api-gateway`, `booking`, `check-in`, `review`, `notification`, `hotels`,
`booking-postgres`, `check-in-postgres`, `hotels-postgres`, `review-postgres`,
`kafka`, `config-etcd`, `akhq`. Redis в актуальном стенде отсутствует.
Повторный `stop` сохраняет исходное число реплик. `start` возвращает его;
если история остановки отсутствует и компонент выключен, запускает одну реплику.
`restart` работает для запущенного компонента; для остановленного используйте `start`.
`stop` без аргументов останавливает весь k3s; вернуть весь стенд — `up`.
Команды останавливают только выбранные компоненты, не весь граф зависимостей.

## Подключения с компьютера

| Host | Port | Database / User | Password |
|---|---:|---|---|
| `booking-postgres` | 15432 | `booking` | `booking-local` |
| `check-in-postgres` | 15433 | `checkin` | `checkin-local` |
| `hotels-postgres` | 15434 | `hotels` | `hotels-local` |
| `review-postgres` | 15435 | `review` | `review-local` |

Это учебные пароли из `deploy/booking-dependencies/credentials.yaml`.
SSL выключен (`sslmode=disable`), схема `public`.
Порты постоянно опубликованы на loopback после `up`; отдельный `port-forward` не нужен.
Имена добавляет `sudo ./bin/hotelctl hosts --install`. До этого используйте `127.0.0.1`
с теми же портами. Удаление только записей CLI: `sudo ./bin/hotelctl hosts --remove`.
Изнутри k3s БД по-прежнему доступны по своим именам на порту 5432.
После перезапуска БД обновите соединение в SQL-клиенте: готовность Deployment
и обновление локального маршрута могут отличаться на несколько секунд.

- API: http://localhost:8080; Swagger: http://localhost:8080/docs/.
- Kafka UI (AKHQ): http://localhost:8090 → `hotelflow` → Topics → `booking.events.v1` → Data.
- Обратный поток: `checkin.events.v1`. В UI видны key, headers, payload, partition, offset.
- etcd: http://127.0.0.1:2379.

## Демо 1. Бронь сохранена, Kafka недоступна

Начинайте с полностью готового стенда: при холодном старте приложениям нужна Kafka.

1. Откройте две БД и Kafka UI. Вопрос аудитории: «Сможем ли создать бронь при выключенной Kafka?»
2. Выполните `./bin/hotelctl stop kafka`.
3. Создайте бронь через `POST /v1/bookings` (пример ниже).
4. В Booking покажите бронь `CONFIRMED` и запись outbox с `processed_at = null`.
   Они сохраняются одной транзакцией. В Check-in этой брони пока нет.
5. Выполните `./bin/hotelctl start kafka`. Дождитесь доставки.
6. Покажите `processed_at` в outbox, событие в AKHQ, запись inbox и `EXPECTED` в Check-in.

Для каждого нового прогона выбирайте свободный номер и новый ключ.
Пример для macOS (номер 1; при HTTP 409 проверьте занятость через
`POST /v1/rooms/availability` или задайте другой период):

```bash
DEMO_ACTOR=$(uuidgen | tr '[:upper:]' '[:lower:]')
DEMO_KEY=$(uuidgen | tr '[:upper:]' '[:lower:]')
DEMO_START=$(date -u -v-5M '+%Y-%m-%dT%H:%M:%SZ')
DEMO_END=$(date -u -v+13H '+%Y-%m-%dT%H:%M:%SZ')
curl --fail-with-body -sS http://localhost:8080/v1/bookings \
  -H 'Content-Type: application/json' \
  -H 'X-Demo-Role: GUEST' \
  -H "X-Demo-Actor: $DEMO_ACTOR" \
  -d "{\"idempotency_key\":\"$DEMO_KEY\",\"room_id\":\"1\",\"starts_at\":\"$DEMO_START\",\"ends_at\":\"$DEMO_END\"}"
```

Сохраните `booking.id` из ответа. Повтор с тем же телом и ключом вернёт ту же бронь.
Ниже замените `<booking_id>` её UUID.

Booking:

```sql
select id, room_id, status, version from public.bookings where id = '<booking_id>';

select id, entity_id, topic, convert_from(body, 'UTF8')::jsonb as event,
       processed_at, errors_count, error_desc
from public.outbox where entity_id = '<booking_id>' order by id;
```

Check-in:

```sql
select id, entity_id, correlation_id, topic, processed_at, errors_count
from public.inbox where entity_id = '<booking_id>' order by id;

select id, booking_id, status, version
from public.checkins where booking_id = '<booking_id>';
```

## Демо 2. Повторная доставка и inbox

Вопрос аудитории: «Событие записано в Kafka, но relay упал до отметки в outbox. Что будет после перезапуска?»

1. Зафиксируйте ID уже доставленного `booking.confirmed.v1` в outbox Booking,
   его `correlation_id`, число строк inbox Check-in и состояние проекции.
2. В учебной БД Booking выполните для **одной выбранной** строки:

   ```sql
   update public.outbox set processed_at = null where id = <outbox_id>;
   ```

   Это управляемая имитация повторной отправки; payload и идентификатор события сохраняются.
3. В AKHQ появится второй record с новым offset и тем же событием.
4. В Check-in останется одна строка inbox для пары `(correlation_id, topic)`;
   состояние и версия `checkins` не изменятся. В этом проекте `correlation_id`
   используется для дедупликации конкретного сообщения — не как общий ID всей цепочки.

Граница гарантии: inbox защищает приём от дублей; обработчик inbox тоже может быть
выполнен повторно. Бизнес-переходы должны быть идемпотентны, поскольку изменение
проекции и отметка `processed_at` выполняются отдельными шагами.
Notification — заглушка без дедупликации, поэтому повторный лог там ожидаем.

Остановка всего Check-in показывает отставание consumer, но не накопление строк в его inbox:
при выключенном consumer сообщения остаются в Kafka.
Retry topics и DLQ пока не реализованы в стенде: их обсуждаем по презентации.

## После демонстрации сбоя

```bash
./bin/hotelctl start kafka booking-postgres check-in-postgres booking check-in
./bin/hotelctl status
```

Возвращайте также другие компоненты, которые выключали. Для готовых компонентов
`start` сохраняет текущее число реплик. Полный сброс учебных данных — отдельная,
разрушительная операция `hotelctl reset --yes`; для восстановления после сбоя он не нужен.
