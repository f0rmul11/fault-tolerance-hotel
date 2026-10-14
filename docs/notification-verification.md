> Исторический документ: отражает версию стенда на момент работы, а не текущие требования. Действующая [спецификация](specification.md) и [проверки](verification.md).

# Notification и отдельные PostgreSQL — проверка 14 октября 2026

Изолированный прогон: шесть процессов сервисов, четыре контейнера PostgreSQL и одна общая для них тестовая Kafka. Рабочий k3s не пересоздавался. Результат: [JSON](notification-verification.json).

Проверено:

- Четыре разных system_identifier PostgreSQL; каждый экземпляр содержит только таблицы своего владельца в public.
- Миграции Goose, seed Hotels/Review и xo-генерация из public. Старые схемы сервисов не создаются.
- Бронирование, повтор ключа идемпотентности, снимок номера, проверка доступности.
- Шесть конкурентных запросов на один номер: одна успешная бронь и пять отказов; отмена освобождает номер.
- Некорректный ID → HTTP 422; допустимый период короче минимума номера → NewBusinessErr, gRPC FailedPrecondition / HTTP 409.
- Запросы активации и отмены проходят Check-in → Kafka → Booking → Kafka → Check-in.
- Notification получает confirmed/activated/canceled и пишет русские JSON-сообщения в stdout с booking_id и correlation_id. БД и inbox у него нет.
- После graceful restart Notification продолжает с подтверждённых offsets.
- Намеренный повтор события с прежним correlation-id повторяет уведомление: дедупликация не включена.
- При pause тестовой Kafka бронь сохраняется в своей БД с pending outbox. После unpause событие приходит и в Check-in, и в Notification.
- Реальный Echo, reflection и graceful shutdown Notification с Kafka прошли с -race.
- go test -race и go vet: шесть сервисов и hotelctl.
- 13 локальных .proto: buf format/lint/build/generate, включая внешние клиенты. Семантика RPC, номера/типы полей и HTTP-маршруты неизменны. Для Gateway использован штатный offline-процесс Buf с внешним закреплённым кешем импортов.

Прогон не доказывает exactly-once: Notification без inbox может повторить stdout при повторной доставке. Старые Kubernetes-объекты и данные не удалялись, исходные миграции изменены inplace.
