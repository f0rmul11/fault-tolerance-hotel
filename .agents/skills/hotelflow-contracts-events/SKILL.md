---
name: hotelflow-contracts-events
description: Меняй API-контракты, исходящие gateway и события HotelFlow с явными границами моделей и без циклов зависимостей. Используй при работе с protobuf, HTTP, gRPC, Kafka и событиями через скопированные inbox/outbox.
---

# Контракты и интеграции HotelFlow

## API и генерация

- Каждый сервис владеет своим protobuf-контрактом в локальном `api/<сервис>/<область>/v1`, например `services/booking/api/booking/booking/v1/booking.proto`. Общего корневого proto и исключений для бронирования нет. Используй отдельный protobuf namespace сервиса: `booking.booking.v1`, `hotels.hotels.v1`. Даже одинаковые транспортные структуры RequestContext принадлежат локальному контракту; не создавай ради них общий контракт всех сервисов.
- Публичный HTTP-контракт принадлежит `services/api-gateway/api`. Внутренние owner API — gRPC-контракты сервисов. Не добавляй внутренний REST из-за того, что пример ad-service генерирует HTTP для всех RPC.
- Сначала уточни request/response, доменные ошибки и mapping согласно спецификации; затем меняй proto и генерацию. Не пропускай `body: "*"` для JSON POST/PATCH, когда тело предусмотрено HTTP-контрактом. Соответствие метода, path/query/body и статусов проверяется через настоящий handler.
- Собственные generated-типы — `internal/pkg/pb`; generated-клиенты чужих сервисов — `internal/pkg/pb/external`, как в external Buf templates образца. В Booking внешний Hotels-клиент и в API Gateway внешние Booking-, Check-in- и Hotels-клиенты генерируются из `api` сервиса-владельца через `buf.gen.external.yaml` с `inputs.directory: ../<сервис>/api`, как в исходном стенде. У исходного контракта один владелец; дубли proto не редактируются независимо. Синхронизация/генерация клиентов должна быть воспроизводимой и описанной в Makefile. Согласуй module path, `go_package` и реальный output; не копируй несовпадающие импорты из образца.
- Вся protobuf-генерация выполняется через Buf: `make generate` для всех сервисов или `make -C services/<service> generate`. Штатный target запускает закреплённый Docker toolchain: `buf lint`, `buf build`, `buf generate --template buf.gen.local.yaml`, затем external template при наличии зависимости. Прямые вызовы `protoc` и обходные скрипты генерации не нужны. `generate-local` использует те же Buf templates с уже установленными локальными плагинами. Версии закреплены в `build/Dockerfile.codegen`; third_party в репозитории запрещён. Используй Buf dependencies/plugins; если BSR недоступен, допускается документированный внешний кеш исходников с закреплёнными версиями и тот же buf generate.
- Не редактируй вручную `*.pb.go`, `*_grpc.pb.go`, `*.pb.gw.go`, `*.pb.goclay.go` и generated Swagger. Изменяй исходный proto, конфигурацию/шаблон, затем генерируй. Транспортный adapter в `internal/app` остаётся тонким независимо от способа его создания.
- Публичная документация API генерируется из proto шлюза в OpenAPI v2. Старое требование отдельного общего OpenAPI 3.1 отменено; действующая спецификация ссылается на локальные proto и generated Swagger.
- На текущем этапе пользователь разрешил inplace без обратной совместимости. Меняй локальные контракты и всех потребителей вместе; не вводи legacy API или версии ради учебных данных.

## API Gateway как отдельный сервис

При работе со шлюзом обязательно прочитай [оформление API Gateway](references/api-gateway.md): структура, init, прямые generated-клиенты, Clay, auth, mapping, metadata, Buf и проверки. Владелец сценария — Booking; шлюз содержит только транспортный proxy.

## Исходящий gateway

В сервисах-владельцах размещай интеграцию в `internal/infrastructure/gateway/<сервис>`. Это адаптер **исходящего** вызова; он отличается от отдельного сервиса api-gateway на входе системы.

Порт определяется рядом с потребителем в use case/service и описывает необходимую операцию. Gateway принимает/возвращает локальный DTO или другой внутренний тип; protobuf и transport error остаются внутри адаптера. Не используй внутреннюю сущность чужого сервиса как свой агрегат.

Gateway преобразует внутренний запрос в клиентскую модель, вызывает клиент с переданным context, преобразует ответ и ошибку, сохраняет request_id/metadata и различимость известного отказа и неизвестного результата. Он не обращается к чужой БД, не меняет локальные статусы, не принимает бизнес-решение за сущность.

Для ошибок обязателен [единый порядок преобразования](../hotelflow-errors-tests/references/errors.md): ErrorInterceptor в сервисах-владельцах и транспортный proxy в API Gateway и русские пользовательские пояснения. Не рассчитывай, что локальный HTTP вызов generated adapter обязательно проходит через gRPC interceptor. Gateway сохраняет downstream gRPC-код, reason и доступные trailers; не заворачивает любой отказ зависимости в Internal.

Имена зависимостей объявляй строковыми константами в `config/services.go`, например `BookingService = "booking"`; значение совпадает с ключом `Config.Targets`. Gateway registry имеет сигнатуру `NewRegistry(conn map[string]grpc.ClientConnInterface) *Registry`. Он выбирает `conn[config.BookingService]` и создаёт generated-клиент и gateway. В `initGateway` передавай всю map: `gateway.NewRegistry(a.grpcConn)`, без сырых строк и сборки отдельных клиентов в init.

Gateway следует форме образца:

```go
type ExternalClient pb.BookingClient

func NewExternalClient(conn grpc.ClientConnInterface) ExternalClient {
	return pb.NewBookingClient(conn)
}

type Client struct {
	external ExternalClient
}

func NewClient(external ExternalClient) *Client {
	return &Client{
		external: external,
	}
}
```

Registry собирает `booking.NewClient(booking.NewExternalClient(conn[config.BookingService]))`. Методы `Client` преобразуют вход, вызывают `c.external` и преобразуют ответ. Передавай входящий `ctx` напрямую: собственные `context.WithTimeout`, timeout-поля и параметры конструктора не нужны.

Если запрос целиком определяется своей сущностью, передавай её в gateway: например, `Reserve(ctx, *entity.Booking, roomdto.DurationLimits)`. Protobuf-запрос собирается внутри gateway. Не создавай промежуточный DTO только ради копирования getters. DTO ответа допустим, когда представляет результат чужого сервиса; не импортируй его внутреннюю сущность.

Соединения открывает `initGrpcConn`, конкретные клиенты создаёт registry; адаптер получает клиент через конструктор. Не создавай соединение на каждый запрос. Закрытие регистрируется в существующем lifecycle сервиса. Не добавляй retry/breaker/fallback или новую deadline-политику под видом технического адаптера; следуй выбранному учебному этапу и контракту.

Если задача затрагивает события или outbox, прочитай [правила событий](references/events-outbox.md).

## Проверка изменения

Проверь генерацию и компиляцию затронутых клиентов, mapping ошибок и неоднозначный transport outcome, передачу контекста и соответствие API реальному handler. Для JSON HTTP проверь именно JSON body. Для событий проверь сериализацию envelope/payload и отсутствие импорта generated transport в domain. Не запускай все генераторы и обновление всех зависимостей для локальной правки одного контракта.

JSON-модели HTTP и event payload отделены от внутренних DTO/entity. Сбор событий строго следует `New(entity) pipe.Func[event.Events]`; технические пакеты копируются из order-service, а не заменяются собственным relay. Подробности и пример — в [events-outbox.md](references/events-outbox.md).


## Контракт бронирования

Одна бронь — room_id и один период starts_at/ends_at. В CreateBooking, CheckAvailability, BookingView и payload booking.confirmed.v1 используются эти скалярные границы, без TimeSlot/slots. Gateway передаёт их напрямую, Period создаёт Booking. Локальные proto и AsyncAPI/OpenAPI меняются inplace, затем выполняется Buf-генерация.

Для inbox и request/reply Booking/Check-in читай раздел входящих событий в [events-outbox.md](references/events-outbox.md).

Kafka-хендлеры вызывают отдельные use case. Каждое исходящее событие получает стабильный `correlation-id` — ключ идемпотентности, совпадающий в envelope и Kafka header. Повторы из outbox сохраняют ID; reply имеет собственный ID и `causation_id` запроса. События успешного перехода — `booking_activated` и `booking_canceled`, без универсального changed. Детали — в references/events-outbox.md.

## Оформление protobuf

Все локальные .proto оформляй через buf format: отступы в два пробела, отдельное поле на строке, пустые строки между логическими блоками. Русские комментарии объясняют RPC, сообщения, назначение полей, единицы измерения, форматы ID/времени, optional-семантику и асинхронные ответы. Не собирай весь контракт в сплошной блок. После изменения источника запускай Buf lint/build/generate и проверяй внешние клиенты; generated-файлы вручную не редактируй.
