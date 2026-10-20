# Check-in

Собственная сущность CheckIn и сценарии ресепшн: ListArrivals, ActivateBooking, CancelBooking. Обмен с Booking через Kafka, inbox/outbox; собственная БД checkin.

- gRPC в кластере: `check-in.hotelflow.svc.cluster.local:9090`.
- Контракт: `api/check-in/checkin/v1/checkin.proto`.
- `make test`: unit и существующие lifecycle-проверки; полный runtime-тест включается через HOTELFLOW_RUNTIME_TEST=1 с POSTGRES_DSN/KAFKA_BROKERS.
- `make generate`: Buf; сгенерированные файлы не редактируются вручную.
- `timezone: Europe/Moscow`; используется реальное время.

Kafka-подписчик передаёт все строковые заголовки сообщения в `message.WithHeaders`;
они сохраняются JSON-объектом в `public.inbox.header` без фильтрации по именам.
`x-correlation-id` проверяется на совпадение с событием и остаётся ключом идемпотентности.
Повторная обработка читает те же заголовки из inbox. Старые строки с пустыми заголовками
автоматически не восстанавливаются: исправление применяется к новым сообщениям.
