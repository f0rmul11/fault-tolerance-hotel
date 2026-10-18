# Основания правил и ограничения образцов

Проверены исходники `/Users/d.bulykin/Desktop/fault-tolerant-stand`, ветка `module-4/async`, commit `43c0bce3d76687ccdaa304c645c8e06987b20851`. Использовано чтение кода; сервисы, миграции, генераторы и outbox из образца не запускались. Проверен также текущий каркас HotelFlow и его Makefile/Buf-конфигурации. Выводы ниже основаны на статическом чтении.

## Что взято из кода

| Правило | Файлы в репозитории образца |
| --- | --- |
| Закрытое состояние, Old/new, domain increment | `services/ad-service/internal/domain/entity/ad.go`, `option.go` |
| DAO embeds xo, mapping и явный SQL | `services/ad-service/internal/infrastructure/storage/ad/dao/ad.go`, `storage/ad/ad.go` |
| Контекстный transaction executor | `services/ad-service/internal/pkg/transaction/{exec.go,abstract,wrapper}`; сравнение с order-service |
| Использование Exec при нескольких записях | `services/order-service/internal/application/service/order/create_order/service.go` |
| Исходящий gateway и локальное преобразование | `services/ad-service/internal/infrastructure/gateway/review` |
| Раздельные публичные и внутренние proto | `services/api-gateway/api`, `services/{ad-service,order-service,review-service}/api` и их `buf.gen.*.yaml` |
| Goose, xo и шаблоны | `services/ad-service/tools/migrations`, `tools/xo_templates`, `Makefile` |
| Payload/envelope и converter | `services/order-service/internal/events/base.go`, `events/order_created/convertor.go` |
| Buffer и outbox/relay | `services/order-service/internal/pkg/event/buffer.go`, `pkg/outbox`, `pkg/connector/kafka/connector.go` |

## Осознанные отличия и обнаруженные ограничения

1. **Структура проекта.** Пользователь выбрал `domain/vo`, `application/usecase`, `internal/application/service`, `internal/dto`, `infrastructure/storage`; пример местами использует другие пути. Нормативны пути пользователя.
2. **Transaction.** В ad-service nil options вызывают опасное type assertion; в order-service nil обработан. Обе реализации Exec могут вызвать commit при panic callback. Перед переносом нужны исправление и целевые проверки.
3. **xo/DDL.** Во второй части правил пользователь разрешил PK/UNIQUE и индексы; явный NOT NULL и остальные неразрешённые constraints запрещены. Неявный NOT NULL от PK допустим. Mapping nullable обязателен; запись нескольких строк с одним ID не считается успехом. xo helpers с некавыченным UUID/скрытым time.Now и преобразование numeric через float64 не являются требованием.
4. **Миграции.** В образце `make migrations` делает reset, `make xo` применяет миграции, DSN xo жёстко использует localhost, версия xo — latest. Эти побочные эффекты нельзя переносить в обычный codegen.
5. **Контракты.** API-подход подтверждается ad/order/review и api-gateway. Payment работает с Kafka и не содержит аналогичного полного API-toolchain; его шаблонный make generate не является доказательством общей команды. В образце внутренние сервисы публикуют также HTTP/Swagger, а HotelFlow сохраняет внутренний gRPC.
6. **Генерация.** Образец смешивает Buf и go generate/implgen, содержит незакреплённые плагины и незавершённый fast-generate. В HotelFlow уже настроена другая цепочка. В публичном POSTCreateAd образца отсутствует body mapping для JSON — не копировать этот контракт как эталон тела запроса.
7. **Чистота слоёв.** По второй части правил registry с concrete imports разрешён как часть composition root под управлением init.go. Leaf use case/service зависит от узких портов и не получает весь registry.
8. **События.** Исходный envelope не содержит полного HotelFlow metadata; HotelFlow сохраняет свой контракт envelope. По последнему требованию конвертер повторяет образец и возвращает `pipe.Func[event.Events]`, выполняемый при Flush. Сущность между добавлением конвертера и Flush не изменяется. Buffer исправлен: очищается после успешного flusher.
9. **Outbox.** Запись outbox и бизнес-данных внутри одной собственной DB transaction переносится как подход. Удержание отдельной relay-транзакции при Kafka send **разрешено пользователем**, это не требование переписать worker. Возможные дубли, переход к следующим chunks после ошибки, RoundRobin partitioner и metric вместо DLQ нужно учитывать, а не обещать отсутствующие гарантии.
10. **Учебный этап.** Исходный запрет CAS/outbox отменён пользователем. Новое требование — копировать outbox и связанные runtime-пакеты из образца; настройки Sarama, включая retries/idempotent producer/RoundRobin, перенесены вместе с ним. Идемпотентность входящих команд теперь включена по module-2; автоматические RPC retry остаются отложенными.

Первичный аудит выше выполнялся при оформлении правил. Последующая реализация и перенос runtime описаны ниже.

## Дополнение по ошибкам, DI и тестам

Проверены order-service/internal/pkg/ordererror/{error.go,option.go}, internal/pkg/grpc/intercept/server.go, internal/{app.go,init.go}, application/service/registry.go и domain/entity/{order.go,option.go}. Аналогичные interceptors найдены в ad/review. Изучены локальные зависимости grpc-gateway v2.27.3 и Clay v0.3.0.

- Unwrap образца создаёт текстовую ошибку вместо исходного base; WithBase(string) теряет тип причины; BaseMessage паникует при nil; New без опций даёт пустой External. Последнее указание пользователя требует точную копию: эти детали сохраняются, вызывающий код не полагается на identity cause и не вызывает BaseMessage без base.
- В ErrorInterceptor нет case Internal, а External всегда превращается в Internal. Неизвестные ошибки возвращаются без нормализации. В API-gateway/Payment образца mapper не подключён. В HotelFlow полный mapper подключён у сервисов-владельцев; API Gateway проксирует status/metadata через Clay, без собственного пакета ошибок.
- Стандартный grpc-gateway даёт 400 для InvalidArgument/FailedPrecondition и тело google.rpc.Status; актуальный HotelFlow сохраняет 422/409, но использует стандартный Status JSON. Прокси не переносит downstream trailers. Один WithGRPCOpts не гарантирует обработку локального HTTP binding. Проверено чтением исходников, не сетевым прогоном.
- Пояснения в образце местами английские; русский язык — новое обязательное правило HotelFlow.
- Registry собирает concrete зависимости, но leaf create_order.Service хранит узкие Storage/event.Flusher. DAO использует entity.With и опции из option.go.
- HotelFlow использует testing/httptest и slog. Make test включает race и существующие subprocess/TCP lifecycle-тесты: это не unit-only target. Go lint framework не настроен, Buf lint относится к proto. Sprintf внутри slog не является образцом структурированных полей.
- Новые unit-тесты не требуют PostgreSQL/Kafka/Docker; новые метрики и outbox.WithMetrics пока не подключаются.

## Перенос runtime при рефакторинге 11 октября 2026 года

По прямому указанию пользователя Booking использует копии `order-service/internal/pkg/{outbox,event,pipe,worker,connector,msgbus}` и infrastructure messagebus/outbox handlers. Сохранены API `NewOutbox`, `Flush`, `HandlePendingMessages`, event buffer и pipeline-конвертеры. Инициализация разделена по слоям в `init.go`; PostgreSQL Ping и создание Kafka producer выполняются при старте, без режима с nil use cases.

Адаптации ограничены путями импортов, именами сервисов и топиков, конфигурацией HotelFlow и действующими правилами: метрики исключены, DAO не содержит JSON-тегов, DDL приведён к разрешённым ограничениям. Накопленные сообщения перенесены новой миграцией без изменения body. Исправлены конкретные дефекты образца: buffer сохраняется при ошибке конвертации/Flush; worker при Stop закрывает сигнал и дожидается завершения управляющей горутины до Wait по задачам, устраняя зависание после отмены context и гонку Add/Wait. В reference pipe-тесте удалён вызов отсутствующего `Anyway`; проверка остановки pipeline при ошибке сохранена.

Ошибки транзакции relay, включая commit, логируются на внешней границе; повтор одной ошибки подавляется до успеха или изменения причины. Порог ошибок outbox записывает структурированный лог один раз при достижении порога вместо подключения метрики. Другой алгоритм relay не вводился; RoundRobin и переход к следующим chunks после ошибки не дают гарантии порядка. Процессные тесты Booking включаются через `HOTELFLOW_RUNTIME_TEST=1` с PostgreSQL/Kafka; обычные unit-тесты инфраструктуры не требуют.

## Упрощение сценария и API Gateway

API Gateway повторно сопоставлен с `fault-tolerant-stand/services/api-gateway`: `initControllers` создаёт публичный generated descriptor и передаёт в `internal/app/booking/v1` generated Booking-клиент. Собственный application/domain/DTO и второй исходящий gateway-слой удалены. Бизнес-сценарии выполняет Booking. HotelFlow сохраняет свой HTTP-контракт через настройки generated adapter; дополнительные таймауты и RPC retry из примера не перенесены.

Booking create получает условия из Hotels и делегирует атомарную запись в `application/service/booking/confirm`: transaction.Exec, проверка длительности, insert брони с EXCLUDE, event.Flusher. Availability и старый finalize удалены.

## Идемпотентность: module-2/transient-failures

Изучены create_order/service.go, dto.go, dal/order/repostory.go и миграции ветки origin/module-2/transient-failures. Основа: ключ от клиента, UNIQUE и insert on conflict do nothing с проверкой RowsAffected. HotelFlow ограничивает ключ гостем и возвращает существующую бронь при совпадении состава. Версия списка заказов из образца не переносится: в HotelFlow сериализуются занятость одного номера и один ключ операции. Outbox остаётся в бизнес-транзакции по более позднему модулю async.

## Локальные контракты и Buf

Повторно проверены `ad-service/buf.gen.external.yaml`, `api-gateway/buf.gen.external.yaml`, local templates и proto order/review. Каждый контракт хранится в `api` своего сервиса; local template пишет собственный generated API, external template читает `../<владелец>/api` и пишет клиент в `internal/pkg/pb/external`. Исключение с общим корневым proto отменено.

Весь protobuf-код HotelFlow генерируется Buf с закреплёнными локальными плагинами Docker toolchain. Third_party удалён; используются Buf dependencies и документированный внешний кеш импортов при недоступности BSR. В этом изменении повторно выполнена целевая генерация Booking/Gateway; прямые вызовы protoc не добавлялись.

## Inbox Booking / Check-in (13 октября 2026)

Источник: `/Users/d.bulykin/Desktop/fault-tolerant-stand/services/payment-service/internal/pkg/inbox`. Все 10 Go-файлов в каждой копии совпадают с исходником по Go-токенам после замены module prefix и форматирования. Пакет не переписан. NOT NULL в техническом DDL сохранены; метрики скопированы, но опция их регистрации не вызывается.

Router и структура обработчиков перенесены из payment-service/internal/infrastructure. Адаптации HotelFlow: envelope с correlation_id/booking_id, наши payload/use case и имена топиков. Подписчик принимает только стабильный x-correlation-id, совпадающий с envelope. Это ключ inbox; fallback на event_id или offset запрещён. Producer использует hash partitioner по booking_id. Etcd leader не добавлялся: повторы безопасны благодаря идемпотентным переходам Booking, уникальности booking_id Check-in и проверке собственной версии CheckIn. Reply может создаваться повторно; отдельного маркера обработки Booking нет. Ядро пакета inbox скопировано без изменений алгоритма; индекс (correlation_id,topic) уникальный.
