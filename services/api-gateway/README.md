# API Gateway

Транспортный шлюз по образцу `fault-tolerant-stand/services/api-gateway`: Clay регистрирует generated HTTP/gRPC, обработчики `internal/app/booking/v1` напрямую вызывают Booking. Проверки ID номера, периода и доменных правил остаются в Booking/Hotels. Демонстрационная авторизация — `internal/pkg/auth`: только допустимые роли по RPC. UUID и обязательность идентификатора гостя проверяет владелец; шлюз передаёт их без повторной валидации.

Успешные HTTP-вызовы возвращают **200**, создание возвращает бронь `CONFIRMED`. Ошибка сериализуется стандартным grpc-gateway: `{"code":9,"message":"Номер занят в выбранное время","details":[]}`. Русское сообщение и gRPC-код владельца проходят без каталога ошибок шлюза. `InvalidArgument` соответствует 422, `FailedPrecondition`/`Aborted` — 409. Идентификатор запроса — `X-Request-Id`; metadata владельца сохраняются как gRPC trailers. Для HTTP trailers клиент передаёт `TE: trailers` и читает `Grpc-Trailer-X-Error-Code`.

`X-Demo-Role: GUEST` и `X-Demo-Actor: <uuid>` нужны для создания брони. GetBooking и CheckAvailability также доступны `RECEPTION` и `OPERATOR`. Клиент передаёт UUID `idempotency_key` в теле POST; gateway проксирует его без собственной логики идемпотентности. Если `X-Request-Id` отсутствует, interceptor создаёт UUID. Это учебная авторизация, не проверка токенов реального пользователя.

## Генерация

```sh
make generate
```

Buf зависимости объявлены в `buf.yaml`; `buf dep update` обновляет `buf.lock`. Go/gRPC/grpc-gateway/OpenAPI/Clay плагины закреплены в `build/Dockerfile.codegen`. Внешние клиенты генерируются из локальных контрактов Booking, Check-in и Hotels. Каталога `third_party` и копий внешних proto в репозитории нет.

Если BSR недоступен (в проверяемой среде `buf.build` отвечает HTTP 403), доступен явный путь с внешним кешем:

```sh
sh tools/prepare-proto-imports.sh /private/tmp/hotelflow-proto-imports
make generate-offline PROTO_IMPORTS=/private/tmp/hotelflow-proto-imports
```

Скрипт загружает `google/api/{annotations,http}.proto` из `googleapis/googleapis` commit `208993fa5be54732413481843b8154f15eefba52` и `protoc-gen-openapiv2/options/{annotations,openapiv2}.proto` из `grpc-ecosystem/grpc-gateway` tag `v2.27.3`, вместе с лицензиями. Buf well-known types предоставляет сам. Генерация выполняется теми же закреплёнными плагинами в отдельном временном workspace; в проект копируются только generated-файлы. После подготовки кеша этот путь не обращается к BSR.

```sh
go test -race ./...
go vet ./...
go build ./...
```

Публичный каталог: `GET /v1/hotels`, `GET /v1/hotels/{hotel_id}`, `GET /v1/hotels/{hotel_id}/rooms`, `GET /v1/rooms/{room_id}`. Эти маршруты доступны без demo-роли, gateway напрямую проксирует Hotels, без вызовов Review.
