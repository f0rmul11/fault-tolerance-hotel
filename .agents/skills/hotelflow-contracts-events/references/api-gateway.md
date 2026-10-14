# API Gateway: оформление и границы

Образец — `/Users/d.bulykin/Desktop/fault-tolerant-stand/services/api-gateway`. Для HotelFlow принят тот же подход Clay + generated descriptors + прямые gRPC-клиенты. Этот документ имеет приоритет над прежними требованиями Problem JSON и собственного gatewayerror.

## Структура и инициализация

- Публичный proto: `api/api-gateway/<область>/v1`; его generated-код: `internal/pkg/pb/api-gateway/...`. Downstream proto принадлежит владельцу, generated-клиент шлюза находится в `internal/pkg/pb/external/<владелец>/...`.
- `internal/app/<область>/v1/service.go`: `Implementation` встраивает generated `Unimplemented...Server`, хранит `external` с generated client interface и имеет конструктор `New...Service`.
- Каждый RPC — отдельный файл с именем операции: `create_booking.go`, `get_booking.go`. Реализация состоит из преобразования входа, одного вызова `i.external`, возврата ошибки и преобразования ответа.
- В шлюзе нет domain, application/usecase, application/service, бизнес-DTO, storage и дополнительного infrastructure/gateway. Владелец данных выполняет сценарий и проверяет инварианты.
- `initGrpcConn` один раз создаёт map соединений и регистрирует закрытие в lifecycle. Имена сервисов — константы `config`, адреса — конфигурация.
- `initControllers` создаёт generated client из `a.grpcConn[config.BookingService]`, передаёт в `NewBookingService`, затем регистрирует `NewBookingServiceDesc`. Не создавай дополнительный registry для этой прямой сборки и не добавляй nil-fallback.
- Сохраняй существующие probes, порядок graceful shutdown и отдельные init-методы. Подготовка сети не должна скрываться в конструкторе handler.

## Методы прокси

Маппинг делается прямо в методе через `lo.Map` или понятный цикл. Optional protobuf-поля сохраняют отсутствие значения. Не нужны отдельные `response.go`, универсальные конвертеры или промежуточные DTO, повторяющие контракт.

UUID, даты, принадлежность брони гостю, условия номера и результат резервирования проверяются в сервисах-владельцах. Не повторяй их в шлюзе и не добавляй чтение Room/Availability из CreateBooking. Единственный downstream создания брони — Booking.

Передавай исходный `ctx` и caller. Нет собственного timeout, повторов RPC или fallback. Обработчик возвращает downstream ошибку напрямую; перенос metadata решается общим клиентским interceptor, а не копируется в каждый метод.

## Clay, HTTP и авторизация

- Регистрируй generated descriptor Clay. Не пиши собственные `http.go`, JSON decoder, маршрутизатор или `problem.go` для protobuf RPC.
- Clay получает `server.WithGRPCUnaryMiddlewares(requestid.Interceptor(), auth.Interceptor())`; middleware применяется и generated HTTP-адаптером. Проверяй оба входа тестом, не полагайся на предположение о gRPC chain.
- Авторизация находится в `internal/pkg/auth`, policy выбирается по generated full method constants. CreateBooking разрешён GUEST; GetBooking — GUEST/RECEPTION/OPERATOR. Используй enum внешнего контракта, без доменной модели шлюза и сырых списков строк.
- Auth проверяет роль и передаёт actor владельцу без повторной проверки UUID. Отдельный `internal/pkg/requestid.Interceptor` выполняется перед auth: принимает `x-request-id`, при отсутствии создаёт `uuid.NewV4()`, кладёт ID в context и возвращает в metadata ответа. Auth получает ID через `requestid.FromContext(ctx)` и добавляет его в caller; генерация и response headers не относятся к auth. Это работает и для отказов авторизации, и для публичных RPC. Request ID — трассировка, не ключ идемпотентности. Владение бронью проверяет Booking.
- `internal/pkg/httptransport` содержит только настройки grpc-gateway runtime: proto field names, сериализацию, header matching и транспортный mapping.
- Успешный Create/Get — HTTP 200, в текущем сценарии — CONFIRMED. Ошибка — стандартный gRPC Status JSON с `code`, `message`, `details`. InvalidArgument (включая malformed JSON) → 422; FailedPrecondition/Aborted → 409; остальные статусы — стандартный mapping. Routing 404/405 имеют русские пояснения.

## Ошибки и metadata

Шлюз не содержит gatewayerror и собственного ErrorInterceptor владельца. `internal/pkg/grpc/intercept.ProxyMetadata` принимает headers/trailers downstream и передаёт вверх. Это транспортные поля, а не расширения Error.

Сохраняются gRPC status и русское пояснение владельца; `x-error-code` различает бизнес-причины с одним gRPC-кодом. Известный `x-booking-id` позволяет искать попытку после ошибки, но не доказывает сохранение и не заменяет идемпотентность. Клиент может не получить metadata при обрыве связи.

Сетевые Unavailable/DeadlineExceeded/Canceled без owner metadata получают фиксированные русские сообщения в общем interceptor. Не обещай отсутствие брони или rollback после timeout. Не возвращай Problem JSON и не добавляй каталог ошибок домена в gateway.

## Proto и генерация

Контракты находятся локально у владельцев. Публичный контракт шлюза содержит HTTP annotations, а внешний клиент генерируется из `api` Booking. Не импортируй `internal` другого Go-модуля.

Используй `buf.yaml` dependencies и `buf.gen.local.yaml`/`buf.gen.external.yaml` с закреплёнными версиями плагинов. Проверки: Buf format, lint, build и целевая генерация. Generated-файлы не исправляются вручную. Proto оформляется отступами и отдельными объявлениями полей, без сжатых однострочных сообщений.

`third_party` в репозитории не нужен. При недоступности BSR используй документированный внешний кеш зависимостей и существующий offline target; не подделывай buf.lock и не превращай сбой registry в новый способ генерации через прямой protoc.

## Проверка изменения

Проверь маппинг request/response, отсутствие потерь optional-полей, исходный context, отсутствие собственных повторов, русские ошибки и перенос metadata. Для ролей и HTTP status используй generated descriptor с fake downstream; прямой gRPC также должен соблюдать policy. Существующие lifecycle-тесты сохраняются. Бизнес-тесты остаются в сервисах-владельцах.

Ключ идемпотентности `idempotency_key` передаётся из публичного CreateBookingRequest напрямую в Booking. Шлюз не генерирует его, не хранит результаты и не сравнивает состав. CheckAvailability — такой же тонкий RPC/Clay-прокси.

Каталог Hotels проксируется четырьмя GET: `/v1/hotels`, `/v1/hotels/{hotel_id}`, `/v1/hotels/{hotel_id}/rooms`, `/v1/rooms/{room_id}`. Эти read-only маршруты публичные; auth не добавляет им роли бронирования. Gateway не вызывает Review: рейтинг обогащает владелец Hotels. Generated optional rating копируется указателем, чтобы не потерять различие отсутствия оценки и 0.
