# Фундаменты сервисов

`booking`, `check-in`, `review`, `notification`, `hotels` — независимые Go-модули.
Структура и группировка зависимостей в `App` повторяют api-gateway; admin запускается до основного сервера.
Hotels владеет каталогом в PostgreSQL. Booking атомарно сохраняет бронь, период, ключ идемпотентности и outbox в собственной БД. Check-in хранит собственное заселение CheckIn и обменивается командами/ответами с Booking через Kafka и inbox/outbox. Review хранит пользовательские рейтинги отелей и читает их батчем. Notification читает события Booking напрямую из Kafka и выводит push-заглушки в stdout, без inbox и БД. У Booking, Check-in, Hotels и Review отдельные PostgreSQL, все таблицы в public. Подробности — [бронирование](booking.md).

## Проверка

```bash
go run ./hotelctl/cmd/hotelctl up
./bin/hotelctl status
./bin/hotelctl smoke
grpcui --plaintext 127.0.0.1:9091
```

У каждого сервиса есть `<service>.echo.v1.EchoService/Echo`.
Для `check-in` protobuf package называется `check_in.echo.v1`.
Запрос `{"message":"Hello"}` возвращает `{"message":"Hello","service":"booking"}`
для booking; поле `service` всегда содержит имя ответившего сервиса.
Reflection позволяет использовать grpcui/grpcurl без локальных proto-файлов.
В кластере адрес имеет вид `<service>.hotelflow.svc.cluster.local:9090`.

## Clay и транспорт

Clay v0.3.0 сохранён как серверный каркас. Его `ServiceDesc` требует методы
`RegisterHTTP` и `SwaggerDef` даже для gRPC-сервиса. Небольшой адаптер `internal/pkg/grpcdesc`
регистрирует только сгенерированный gRPC-сервер; `RegisterHTTP` ничего не регистрирует.

В новых сервисах нет HTTP-аннотаций protobuf, сгенерированных `.pb.gw.go` или OpenAPI-файлов.
Пакет grpc-gateway/runtime остаётся частью интерфейса самого Clay.
Внутренний HTTP listener Clay не опубликован в Kubernetes Service и не предоставляет Echo.
Публичное HTTP API и его генерация остаются у api-gateway.

Admin Service публикует неготовые endpoints: healthcheck доступен с компьютера и при `Ready=False` у Pod.
Liveness не зависит от readiness. До старта основного сервера liveness=200, readiness/startup=503.

## Контракты

```bash
make -C services/booking generate
make -C services/booking test
```

Buf выполняет lint, build и generate. В `build/Dockerfile.codegen` закреплены Buf v1.57.2,
protoc-gen-go v1.36.10 и protoc-gen-go-grpc v1.5.1. Внешние `.proto` новые контракты не импортируют,
поэтому BSR-зависимости, buf.lock и third_party им не нужны.
Сгенерированный код сохранён в каждом сервисе, запуск стенда не требует генерации или доступа к BSR.

## Конфигурация стенда

В `hotel.yaml` достаточно версии k3s и списка имён сервисов. По имени определяются
`services/<name>`, `build/Dockerfile`, `build/deployment`, образ и Deployment.
Сервисы после gateway получают gRPC-порты от 9091 и admin-порты от 8082 по порядку списка.
Чтобы сохранить привычные адреса, новые сервисы добавляют в конец.

Для нестандартного сервиса вместо имени можно указать объект с точечным переопределением:

```yaml
services:
  - api-gateway
  - name: booking
    grpc: {host: 19091, node: 31091}
    admin: {host: 18082, node: 31082}
```

`hotelctl up` обновляет публикацию портов, пересоздавая только контейнер k3s с тем же томом данных.
Исходные Kubernetes-манифесты при деплое не переписываются.
