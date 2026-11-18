# Notification

Уведомления по событиям брони: Kafka → обработчик → use case → gateway-заглушка. Реального SMS/push-провайдера нет: `internal/infrastructure/gateway/push` пишет структурированный JSON в stdout через slog. Не требует PostgreSQL, inbox или outbox.

| Событие booking.events.v1 | Заголовок | Текст |
| --- | --- | --- |
| booking.confirmed.v1 | Бронирование подтверждено | Номер в отеле, его тип, период и UUID брони |
| booking.activated.v1 | Добро пожаловать! | Бронь активирована, заселение оформлено |
| booking.canceled.v1 | Бронирование отменено | Бронь отменена |

Каждый лог содержит `service`, `channel`, `provider`, `booking_id`, `correlation_id`, `text`. События смены статуса несут идентификатор брони: дополнительные RPC или локальная проекция для обогащения не нужны. Эти логи показывают реакцию на событие; доставки на устройство пока нет.

### Как Notification повторяет доставку

Каждая подписка получает собственный `retry.Router` при сборке сервиса. `MessageSubscriber` ждёт срока, вызывает обработчик бронирования и передаёт ошибку этому маршрутизатору. Так устроен [пример курса](https://github.com/Fault-Tolerance-Course/homework-stand/pull/11). В Notification к нему добавлена вторая ступень и jitter.

| Где находится событие | Кто читает | Куда ведёт временный отказ push |
| --- | --- | --- |
| `booking.events.v1` | `notification-booking-events` | `notification.booking.retry.1.v1` через 5–6 с |
| `notification.booking.retry.1.v1` | `notification-booking-events-retry-1` | `notification.booking.retry.2.v1` через 30–36 с |
| `notification.booking.retry.2.v1` | `notification-booking-events-retry-2` | `notification.booking.dlq.v1` с причиной `retries_exhausted` |

Запись в следующий топик появляется сразу. Обработчик retry ждёт до `deliver-at`, поэтому после рестарта он ждёт только остаток задержки. Срок отсчитывается от последнего отказа и включает равномерный случайный jitter до 20% базовой задержки. Ожидание занимает одну партицию retry, основная группа продолжает читать новые события. Успех завершает маршрут на любой ступени. Неизвестный тип корректного события пропускается. Некорректное событие уходит сразу в DLQ с причиной `invalid_event`, испорченные заголовки на retry — с `invalid_retry_metadata`.

Ключ, исходное значение и номер партиции сохраняются. Заголовки Booking имеют по одному значению на ключ. Если в записи из AKHQ ключ заголовка повторяется, Notification читает и пересылает его последнее значение. Обычный Kafka producer публикует следующую запись, затем потребитель подтверждает исходный offset. Ошибка публикации возвращается потребителю. При аварии между двумя операциями или после принятого push возможен повтор. DLQ доступна для ручного разбора 14 дней, записи в retry хранятся 7 дней.

### Как показать маршрут в AKHQ

Откройте Data в топиках Notification в [AKHQ](http://127.0.0.1:8090). Дважды щёлкните по числу в ячейке **Headers**, чтобы развернуть заголовки записи. Найдите записи одного события по `original-topic`, `original-partition` и `original-offset`. Заголовки `last-failed-at` и `deliver-at` показывают выбранную задержку. После двух повторов в DLQ будут `failed-stage=retry-2` и `dlq-reason=retries_exhausted`. При ручном повторе через retry-1 выберите партицию из `original-partition`.

Из API есть только gRPC `notification.echo.v1.EchoService/Echo`, запрос `{"message":"Hello"}`. Служебные healthcheck и reflection сохранены. В кластере адрес `notification:9090`; локально через hotelctl — gRPC `127.0.0.1:9094`, admin `http://127.0.0.1:8085/healthcheck/ready`.

```bash
make build
./bin/hotelctl build notification
./bin/hotelctl deploy notification
./bin/hotelctl logs notification -f
make -C services/notification test
# Проверка настоящего старта, Echo и graceful shutdown с тестовой Kafka:
HOTELFLOW_RUNTIME_TEST=1 KAFKA_BROKERS=127.0.0.1:9092 make -C services/notification test
```

В `config/config.yaml` поле `consumer_group` задаёт основную группу. Ключ каждой записи в `retry` задаёт имя топика. Поля `group` и `delay` задают его consumer group и базовую задержку. `hotelctl reset` читает имена групп из того же файла. Для локального запуска адрес брокера и основную группу можно переопределить через `KAFKA_BROKERS` и `KAFKA_CONSUMER_GROUP`.
