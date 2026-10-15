# HotelFlow: действующая спецификация

Версия 3.0 · 14 октября 2026 года.

Этот документ описывает реализованный учебный стенд и согласованные границы его развития.
Он заменяет спецификацию 2.0 от 7 октября и первоначальный концепт курса.
Краткий контекст для разработки — [agent-context.md](agent-context.md), правила кода — [../AGENTS.md](../AGENTS.md).
История проверок сохраняется отдельно от требований: [verification.md](verification.md).

## Назначение и объём

Стенд нужен для изучения межсервисных взаимодействий и отказоустойчивости на понятном домене:
отель содержит номера, гость бронирует конкретный номер на непрерывный период, ресепшн
оформляет прибытие или отмену, Notification реагирует на события.

В базис уже включены локальные транзакции, GiST EXCLUDE, HTTP-идемпотентность,
CAS при смене состояния и outbox/inbox для Booking и Check-in. Их нельзя считать
отсутствующими учебными дефектами и удалять по старым документам.

Новые RPC retries, circuit breaker, DLQ, кеш, rate limiter, метрики, упражнения со сбоями
и CI в эту доработку не входят. Не добавлять их автоматически.
Нет платежей, тарифов, OTA, управления готовностью номера, checkout/no-show, редактирования
брони, управления каталогом и записи пользовательских оценок. Каталог и рейтинги задаёт seed.
Production IAM и высокая доступность инфраструктуры также не реализуются.

## Сервисы и владение данными

Всего шесть сервисов. Каждый — отдельный Go-модуль; приложения общаются через API и Kafka.

| Сервис | Ответственность и сущности | Зависимости | Хранение |
| --- | --- | --- | --- |
| API Gateway | Clay HTTP/gRPC proxy, роли и request_id; без бизнес-сущностей и use case | Booking, Check-in, Hotels | Нет |
| Booking | Booking: гость, выбранный номер, период, статус, занятость и запрет пересечений | Hotels до транзакции; Kafka | Свой PostgreSQL |
| Check-in | CheckIn: ожидаемое прибытие и оформление заселения; связь с бронью через booking_id | Kafka | Свой PostgreSQL |
| Hotels | Hotel и Room: сведения для UI, категория, оснащение, ограничения длительности | Review для рейтингов отелей | Свой PostgreSQL |
| Review | Пользовательский рейтинг отеля; batch-чтение агрегированных оценок | Нет | Свой PostgreSQL |
| Notification | Реакция на события Booking; gateway-заглушка push пишет в stdout | Kafka | Нет |

Отдельных Room, Search и Availability нет. У номеров нет пользовательского рейтинга.
Check-in не содержит сущность Booking и не меняет чужую БД. Отдельной бизнес-сущности Operation нет.

Каждый PostgreSQL имеет свой Deployment, Service, Secret и PVC:
`booking-postgres`, `check-in-postgres`, `hotels-postgres`, `review-postgres`.
Все прикладные и технические таблицы расположены в `public` собственной БД.
Kafka общая, адрес внутри кластера — `kafka:9092`. Один узел k3s не обеспечивает HA.
Прикладная runtime-конфигурация хранится в отдельном `config-etcd:2379` с собственным PVC
`config-etcd-data`. Это один member, HTTP без авторизации для доверенного локального стенда;
доступ с компьютера — через loopback port-forward (`make config-etcd`). `up/deploy` запускает
etcd, `reset` сохраняет overrides. Пакет reader готов, его bootstrap в API Gateway ещё не подключён.

## Идентификаторы, время и основные данные

| Объект | Существенные поля и правила |
| --- | --- |
| Hotel | UUID; имя, описание, официальные stars 0–5, страна, город, адрес, latitude/longitude. Рейтинг Review добавляется в ответ и не хранится в Hotels |
| Room | Системный положительный bigint ID, UUID hotel_id, number внутри отеля, строковый room_type, описание, capacity, room_count, bed_count, area_square_meters, amenities, min/max duration |
| Booking | UUID, guest_id, room_id, снимок room_number/room_type, starts_at/ends_at, status, idempotency_key, request_id, version, created_at/updated_at |
| CheckIn | Свой UUID, booking_id, room_id, снимок room_number/room_type, guest_id, период, собственные status/version, actor_id/request_id/requested_at, previous_status и failure |
| Rating | Среднее 0–5 и число оценок; Review хранит total_stars/review_count по hotel_id. Отсутствие рейтинга отличается от оценки 0 |

Hotel, Booking, CheckIn, guest/actor, request, idempotency key и correlation-id — UUID.
Новые UUID создаются через `uuid.NewV4()`. В примерах и seed нет UUID с закодированными порядковыми номерами.
Room ID передаётся в HTTP/gRPC/Kafka десятичной строкой, например `"1"`.
Номер двери `"101"` и системный ID `"1"` — разные поля; одинаковые номера дверей в разных отелях допустимы.
`room_type` — значение типа, например `"Standard"`, а не UUID.

Период — VO `Period`, полуоткрытый интервал `[starts_at, ends_at)`. Начало произвольное,
время нормализуется в UTC с точностью до микросекунд. Начало должно быть раньше конца.
Слотов и ночей в модели нет. Соседние интервалы без общего времени разрешены.
Минимум 12 часов задаётся текущим seed Hotels; Booking проверяет полученные min/max условия.
Отсутствие max означает отсутствие верхнего предела.

## Пользовательские сценарии

### Каталог и доступность

Гость читает отели, выбирает отель, номер и период. ListHotels/GetHotel получают рейтинг
через один batch RPC Hotels → Review; Review читает набор одним SQL.
ListRooms/GetRoom не обращаются в Review. Ошибка рейтингов не должна блокировать чтение номера для бронирования.

CheckAvailability проходит Gateway → Booking → Hotels: Hotels даёт номера и условия,
Booking исключает занятые по собственной БД. Результат — снимок доступности,
а не удержание номера и не гарантия последующего создания.

### Создание и чтение брони

1. Гость отправляет room_id, starts_at, ends_at и UUID idempotency_key. Guest ID берётся из caller.
2. Booking проверяет ранее сохранённый ключ. Сохранённый повтор не требует доступности Hotels.
3. Для новой брони Booking получает номер и его условия из Hotels до транзакции.
4. Локальная транзакция проверяет длительность и сохраняет CONFIRMED версии 1 вместе с outbox.
   Advisory lock и повторного чтения ключа внутри confirm нет.
5. PostgreSQL EXCLUDE запрещает пересечение периодов одного room_id.
6. После commit клиент получает BookingView. Relay доставляет booking.confirmed.v1 в Kafka.

Пара `(guest_id, idempotency_key)` уникальна. Тот же ключ и состав возвращают прежнюю бронь;
другой состав даёт Conflict. При гонке вставок проигравший получает Conflict;
следующий запрос с тем же ключом прочитает сохранённую бронь. request_id используется для трассировки, не для идемпотентности.
При потерянном ответе commit клиент повторяет запрос с тем же ключом. Сохраняемых PENDING/UNCERTAIN нет.
Чтение чужой брони гостем запрещено; RECEPTION/OPERATOR имеют операционный доступ.

### Ожидаемые прибытия

Check-in принимает booking.confirmed.v1, сохраняет inbox и создаёт собственный CheckIn EXPECTED.
GET списка выбирает EXPECTED/ARRIVING с **началом проживания** в `[starts_at, ends_at)`.
Обе границы фильтра передаются вместе. Без фильтра используются сегодняшние сутки
в `Europe/Moscow` из конфигурации Check-in. Вчерашнее начало не попадает в сегодняшний список,
даже если проживание продолжается; CHECKED_IN/CANCELING/CANCELED также не входят.

### Активация

Ресепшн отправляет booking_id и idempotency_key в Check-in.
Одна транзакция сохраняет ключ `(actor_id, key)`, переход EXPECTED → ARRIVING и outbox
с checkin.arrival-requested.v1. HTTP-ответ означает принятие команды, не завершение обмена.

Booking получает команду через inbox, проверяет статус и время действия оператора,
переводит CONFIRMED → ACTIVE и сохраняет ответ booking.activated.v1 той же транзакцией.
Check-in применяет ответ к ожидаемой собственной версии и переходит в CHECKED_IN.
Активация допустима внутри периода проживания; проверяется исходный requested_at,
поэтому задержка Kafka не превращает своевременное действие в опоздание.

### Отмена

Отмена инициируется через Check-in и разрешена для EXPECTED/CHECKED_IN.
Check-in сохраняет CANCELING и checkin.cancellation-requested.v1.
Booking переводит CONFIRMED/ACTIVE → CANCELED, освобождает период и публикует booking.canceled.v1.
Check-in после ответа становится CANCELED. Отменённая бронь больше не участвует в EXCLUDE и занятости.

Повтор POST с тем же оператором и ключом возвращает текущее состояние CheckIn без нового действия.
Изменение booking_id/action для занятого ключа — Conflict.
При бизнес-отказе Booking публикует booking.checkin-rejected.v1; Check-in возвращает
предыдущее состояние и сохраняет русское пояснение. Старый ответ не меняет новое состояние.
При недоступности Kafka ARRIVING/CANCELING сохраняются до доставки ответа; автоматического таймера восстановления нет.

### Уведомления

Notification читает booking.confirmed.v1, booking.activated.v1 и booking.canceled.v1.
Каждое событие обрабатывает отдельный use case; infrastructure/gateway/push пишет
структурированный русский текст в stdout с booking_id и correlation_id.
Inbox, outbox, БД и дедупликации у Notification нет. Offset подтверждается после handler;
повторная доставка может повторить лог. Это согласованное поведение заглушки.

## API и ошибки

Источник точных полей — локальные `.proto` владельцев. Публичный контракт —
[api-gateway/api](../services/api-gateway/api); generated Swagger доступен на `/docs/` и `/swagger.json`.
Общего business.proto нет. Используется Buf; generated-файлы вручную не редактируются.
Generated OpenAPI шлюза имеет версию 2.0; требование отдельного общего OpenAPI 3.1 из старой спецификации отменено.

| Метод и путь | Сценарий | Доступ |
| --- | --- | --- |
| GET /v1/hotels | Список отелей с рейтингами; limit 1–100 (по умолчанию 20), offset ≥ 0 | Публичный |
| GET /v1/hotels/{hotel_id} | Один отель с рейтингом | Публичный |
| GET /v1/hotels/{hotel_id}/rooms | Номера отеля | Публичный |
| GET /v1/rooms/{room_id} | Номер и условия длительности | Публичный |
| POST /v1/rooms/availability | Свободные room_ids для hotel_id и периода | GUEST/RECEPTION/OPERATOR |
| POST /v1/bookings | Создание/повтор брони | GUEST |
| GET /v1/bookings/{booking_id} | Чтение брони | Гость-владелец/RECEPTION/OPERATOR |
| GET /v1/check-in/bookings | Ожидаемые прибытия по началу проживания | RECEPTION/OPERATOR |
| POST /v1/check-in/bookings/{booking_id}/activate | Запрос активации | RECEPTION/OPERATOR |
| POST /v1/check-in/bookings/{booking_id}/cancel | Запрос отмены | RECEPTION/OPERATOR |

Успешные операции возвращают HTTP 200. У Check-in итоговая смена состояния асинхронна.
Demo-заголовки: `x-demo-role`, `x-demo-actor` UUID; это учебная авторизация, не проверенный токен пользователя.
Отдельный requestid interceptor шлюза принимает/создаёт `x-request-id`, помещает в context и ответ;
auth выполняется после него и проверяет роли. Identity и владение проверяют сервисы-владельцы.

| Ситуация | gRPC | HTTP |
| --- | --- | --- |
| Неверные входные данные | InvalidArgument | 422 |
| Ресурс отсутствует | NotFound | 404 |
| Доступ запрещён | PermissionDenied | 403 |
| Нарушение бизнес-инварианта: NewBusinessErr | FailedPrecondition | 409 |
| CAS или конфликт ключа идемпотентности | Aborted | 409 |
| Технический сбой зависимости | Unavailable / DeadlineExceeded | 503 / 504 |
| Внутренняя ошибка | Internal | 500 |

Все пользовательские пояснения русские. Gateway возвращает стандартный gRPC Status JSON через Clay,
без Problem, gatewayerror, собственных use case и повторной бизнес-валидации.
Каждый сервис сохраняет Echo и probes; у Notification бизнес-ручек нет, Review доступен внутренним batch RPC.

## События и гарантии доставки

| Топик | Producer | Событие | Consumer |
| --- | --- | --- | --- |
| booking.events.v1 | Booking | booking.confirmed.v1 | Check-in, Notification |
| booking.events.v1 | Booking | booking.activated.v1 | Check-in, Notification |
| booking.events.v1 | Booking | booking.canceled.v1 | Check-in, Notification |
| booking.events.v1 | Booking | booking.checkin-rejected.v1 | Check-in; Notification пропускает |
| checkin.events.v1 | Check-in | checkin.arrival-requested.v1 | Booking |
| checkin.events.v1 | Check-in | checkin.cancellation-requested.v1 | Booking |

Других рабочих топиков нет; `__consumer_offsets` — внутренний топик Kafka.
Envelope и payload определены в `services/<producer>/internal/events`;
подробности — [check-in.md](check-in.md) и [identifiers.md](identifiers.md).
Kafka key/aggregate_id — UUID брони. correlation_id — отдельный UUID события,
одинаковый в envelope и `x-correlation-id`; повтор relay сохраняет его.
Reply получает собственный correlation_id, causation_id указывает на команду.
Снимок создания включает room_id строкой, room_number, room_type и границы периода.

Consumer groups: `booking-checkin-events`, `check-in-booking-events`, `notification-booking-events`.
Новые группы читают oldest. Delivery — at-least-once, без обещания exactly-once.
Booking/Check-in сначала сохраняют inbox с уникальным `(correlation_id, topic)`, затем подтверждают offset.
Booking сохраняет CAS-переход и reply outbox одной транзакцией. Inbox может повторить use case
после business commit; идемпотентные доменные методы не повторяют изменение статуса/версии.
Отдельного маркера обработанных событий нет. Ответное событие может создаться повторно
с новым correlation_id и прежним causation_id; Check-in защищает его применение собственной
версией действия. Повтор создания не перезаписывает CheckIn.

Бизнес-транзакции не вызывают внешние системы. Для отдельного outbox relay разрешено
держать транзакцию БД во время отправки в Kafka, как в выбранном образце.
Некорректное сообщение может блокировать обработку; автоматического DLQ/пропуска нет.

## Данные и инварианты хранения

| БД / владелец | Таблицы public |
| --- | --- |
| Booking | bookings, inbox, outbox, goose_db_version |
| Check-in | checkins, requests, inbox, outbox, goose_db_version |
| Hotels | hotels, rooms, goose_db_version |
| Review | ratings, goose_db_version |

Точные DDL — `services/<owner>/tools/migrations`; единого storage.sql нет.
SQL lower case; primary key/unique/индексы допустимы, FK и каскадные связи не добавляются.
Для bookings согласовано узкое исключение: NOT NULL границ/room_id, CHECK конечного непустого
периода и EXCLUDE USING gist `(room_id with =, tstzrange(starts_at, ends_at, '[)') with &&)`
для неотменённых записей. Это защищает от пересечения конкурентных INSERT, включая прямой SQL.
Остальные инварианты и переходы принадлежат сущностям, не DAO/handler.

## Запуск, проверка и сброс

- `hotelctl up` собирает приложения, поднимает инфраструктуру и применяет миграции/seed.
- `hotelctl smoke` проверяет доступность HTTP/gRPC/probes без создания данных.
- `hotelctl smoke --business` создаёт собственную бронь, проверяет повтор и пересечение,
  активирует/отменяет через Kafka и проверяет три уведомления. Отменённая запись и события остаются.
- `hotelctl reset --yes` удаляет учебные данные всех четырёх БД, восстанавливает seed,
  пересоздаёт два рабочих топика и удаляет три consumer group вместе с offsets.
- `hotelctl stop` сохраняет данные; `hotelctl destroy` удаляет весь стенд и его том.

Reset сначала сохраняет исходные реплики и останавливает шесть приложений до исчезновения Pod.
Kubernetes Job последовательно очищает public без goose_db_version (restart identity),
загружает seed Hotels/Review в тех же локальных транзакциях, затем очищает Kafka.
После успеха возвращает исходные реплики; Gateway открывается последним.
Схема, миграционная история, k3s, брокер, PostgreSQL, PVC и образы сохраняются.
Это сброс данных актуальной схемы, а не пересоздание БД после inplace-правки старой миграции.

Если команда прервалась, checkpoint и Job позволяют продолжить `reset --yes`.
До завершения очистки приложения не запускаются; `up`/`deploy` блокируются.
Успешный этап очистки не повторяется при ошибке последующего запуска.
Исходно выключенный сервис остаётся выключенным. Во время reset не запускайте внешние producer,
ручное масштабирование, изменение конфигурации или другие запросы к данным.
Команда предназначена для текущего локального стенда, а не произвольного общего Kafka-кластера.

Seed: 3 отеля, 11 номеров (ID 1–11), 2 агрегированных рейтинга (4.5 и 4.0).
У третьего отеля нет номеров/рейтинга; у семейного номера ID 11 максимум 7 суток.
После reset нет броней, CheckIn, HTTP-ключей, обработанных событий и ожидающих сообщений.
AKHQ доступен через `make kafka-ui` на localhost:8090; туннель запускается отдельно.

## Границы гарантий и проверки результата

EXCLUDE запрещает двойную занятость; он не проверяет существование номера и длительность.
Чтение Hotels до транзакции даёт снимок условий; запись каталога отсутствует.
Потеря ответа не доказывает rollback. Идемпотентный повтор — способ узнать результат.
Kafka outage задерживает проекцию и активацию/отмену, но не отменяет committed бронь.
Потеря диска PostgreSQL, Kafka retention, длительное отсутствие reply и ошибочные события
не закрываются обещанием «inbox всё восстановит».

Готовность базиса подтверждают сборка и unit/race-тесты, `smoke`, полный `smoke --business`,
а для reset — пустые бизнес/технические таблицы, seed 3/11/2, отсутствие старых событий/offsets
и успешный бизнес-сценарий после сброса. Проверки на fake не заменяют прогон на реальном k3s.
