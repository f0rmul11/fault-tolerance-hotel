# Notification

Уведомления по событиям брони: Kafka → обработчик → use case → gateway-заглушка. Реального SMS/push-провайдера нет: `internal/infrastructure/gateway/push` пишет структурированный JSON в stdout через slog. Не требует PostgreSQL, inbox или outbox.

| Событие booking.events.v1 | Заголовок | Текст |
| --- | --- | --- |
| booking.confirmed.v1 | Бронирование подтверждено | Номер в отеле, его тип, период и UUID брони |
| booking.activated.v1 | Добро пожаловать! | Бронь активирована, заселение оформлено |
| booking.canceled.v1 | Бронирование отменено | Бронь отменена |

Каждый лог содержит `service`, `channel`, `provider`, `booking_id`, `correlation_id`, `text`. События смены статуса несут идентификатор брони: дополнительные RPC или локальная проекция для обогащения не нужны. Эти логи показывают реакцию на событие; доставки на устройство пока нет.

Consumer group `notification-booking-events` независима от Check-in. Новая группа читает с oldest. Kafka offset подтверждается после успешной обработки. Дедупликации нет: повтор события повторяет лог с тем же correlation-id. Неизвестный тип корректного события пропускается, ошибка десериализации/обработки не подтверждается. Автоматического DLQ нет.

Из API есть только gRPC `notification.echo.v1.EchoService/Echo`, запрос `{"message":"Hello"}`. Служебные healthcheck и reflection сохранены. В кластере адрес `notification:9090`; локально через hotelctl — gRPC `127.0.0.1:9094`, admin `http://127.0.0.1:8085/healthcheck/ready`.

```bash
./bin/hotelctl build notification
./bin/hotelctl deploy notification
./bin/hotelctl logs notification -f
make -C services/notification test
# Проверка настоящего старта, Echo и graceful shutdown с тестовой Kafka:
HOTELFLOW_RUNTIME_TEST=1 KAFKA_BROKERS=127.0.0.1:9092 make -C services/notification test
```

Настройки: `kafka_brokers` / `KAFKA_BROKERS`, `consumer_group` / `KAFKA_CONSUMER_GROUP`. Стандартный запуск — `go run ./cmd` из каталога сервиса. При локальном запуске укажите адрес Kafka через окружение.
