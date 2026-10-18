# Контекст разработки HotelFlow

Актуально на 14 октября 2026 года. Сначала читай [../AGENTS.md](../AGENTS.md),
[specification.md](specification.md) и относящиеся к задаче skills в `.agents/skills`.
Спецификация 2.0, исходный README курса и исторические отчёты не задают текущую архитектуру.

- Шесть сервисов: API Gateway, Booking, Check-in, Hotels, Review, Notification.
  Room/Search/Availability удалены; не восстанавливай их по старым документам.
- Hotels владеет Hotel/Room, описанием, удобствами и min/max длительностью. Review — только рейтингом
  отеля. ListHotels/GetHotel получают рейтинг одним batch RPC, который читает набор одним SQL.
- Booking владеет бронью и занятостью конкретного room_id на один период [starts_at, ends_at).
  Нет слотов/ночей/PENDING/UNCERTAIN. Новая бронь — CONFIRMED.
- Booking получает условия Hotels до локальной транзакции; создаёт бронь, HTTP-ключ и outbox
  атомарно. GiST EXCLUDE запрещает пересечение. Сохранённый повтор возвращает прежнюю бронь;
  при гонке вставок проигравший получает Conflict. Advisory lock и перечитывания ключа в confirm нет.
- Check-in владеет CheckIn, связанным с Booking только идентификатором и данными для заселения.
  Список EXPECTED/ARRIVING фильтруется по началу проживания; без фильтра — сегодня Europe/Moscow.
  Активация и отмена используют Kafka request/reply, свои HTTP-ключи и CAS.
- Booking: CONFIRMED → ACTIVE, отмена CONFIRMED/ACTIVE → CANCELED.
  CheckIn: EXPECTED → ARRIVING → CHECKED_IN; отмена EXPECTED/CHECKED_IN → CANCELING → CANCELED.
- Топики booking.events.v1 и checkin.events.v1. correlation_id UUID — ключ события,
  request_id — трассировка, idempotency_key — ключ HTTP; это разные идентификаторы.
  Kafka handlers вызывают use case. Booking и Check-in используют inbox/outbox.
  Повторная обработка допустима: доменные переходы идемпотентны, отдельного маркера событий нет.
  Reply может повториться; Check-in проверяет версию действия и не откатывает новое состояние.
- Notification читает confirmed/activated/canceled и пишет push-заглушки в stdout.
  У него нет inbox/БД/дедупликации, из RPC только Echo. Повторные логи допустимы.
- Прикладной `config-etcd` разворачивается отдельно в k3s с PVC `config-etcd-data`.
  `hotelctl up` публикует etcd на loopback:2379 для `hotelctl config get/set`; `reset` сохраняет overrides.
  Reader `rtconfig` автоматически запускается в API Gateway до serving и закрывается после drain.
  Локальная готовность клиента входит в `/healthcheck/ready` как `runtime_config`;
  недоступный etcd не делает валидный локальный snapshot неготовым. HTTP/gRPC gate пока не добавлен.
- У Booking, Check-in, Hotels, Review отдельные PostgreSQL; все таблицы в public.
  Нет чужих SQL-чтений. Gateway и Notification не используют БД.
- Hotel/Booking/CheckIn/guest/actor/request/event — UUID v4. Room ID — bigint, в API/событиях
  десятичная строка. number внутри отеля и room_type — отдельные строковые поля.
- Gateway — Clay proxy без domain/usecase. Отдельный requestid interceptor выполняется перед auth;
  проверку владения и бизнес-правил делают владельцы. Ошибки по-русски; NewBusinessErr возвращает
  FailedPrecondition (HTTP 409), неверный ввод — InvalidArgument (422), CAS/ключ — Conflict (409).
- Каждый сервис владеет локальным proto. Источники полей — services/*/api; генерация Buf.
  DDL — services/*/tools/migrations, события — services/*/internal/events. Общего business.proto нет.
- Новые защитные паттерны, CI, упражнения со сбоями и production-функции не добавлять без задачи.
  Метрики пока не нужны. Уже согласованные идемпотентность/CAS/inbox/outbox/EXCLUDE сохраняются.

Проверки: `make test`, `make smoke-business`. Сброс перед упражнением:
`./bin/hotelctl reset --yes` или `make reset`. Он согласованно очищает данные БД/Kafka,
возвращает seed и исходные реплики. При прерывании повторить ту же команду;
до окончания reset не запускать приложения вручную. `up`, генерация и миграции сами данные не сбрасывают.

Меняй связанные сценарии, контракты, события, документацию и tests вместе.
Следуй правилам пользователя и репозитория; не переноси старые ограничения ради совместимости с учебными данными.
