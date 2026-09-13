# HotelFlow

Монорепозиторий учебного стенда отказоустойчивости гостиничной платформы.
Текущий этап: фундамент k3s и `api-gateway` с Echo по HTTP и gRPC.

## Запуск одной командой

После скачивания этого репозитория, из его корня:

```bash
./hotelctl up
```

Требуется работающий Docker с Linux-контейнерами, Bash, curl и sha256sum или shasum.
На macOS можно использовать Docker Desktop; на Linux — Docker Engine;
на Windows — Ubuntu в WSL2 с включённой интеграцией Docker Desktop.
Поддерживаемые архитектуры установщика: amd64 и arm64. Проверенные окружения перечислены в [docs/verification.md](docs/verification.md).

Go, kubectl, k3d и Protobuf-генераторы для обычного запуска вручную устанавливать не нужно.
Утилита скачивает фиксированные версии k3d/kubectl с проверкой SHA-256,
создаёт одноузловой k3s, собирает gateway из исходников в Docker,
импортирует образ в кластер, применяет манифесты и проверяет внешний HTTP Echo.
Первый запуск требует интернета для инструментов, базовых образов и Go-зависимостей.
Готовых опубликованных образов HotelFlow пока нет; повторные сборки используют Docker cache.
Для первого этапа ориентируйтесь на 2 CPU и 4 ГБ RAM, доступные Docker;
это ориентир для gateway, не системные требования полного будущего стенда.

Встроенный Kubernetes в Docker Desktop включать не требуется.
Существующий пользовательский kubeconfig не изменяется: конфигурация курса хранится в `.local/<cluster>/kubeconfig`.

## Echo снаружи кластера

```bash
curl -fsS http://localhost:8080/v1/echo \
  -H 'Content-Type: application/json' \
  -d '{"message":"Привет, HotelFlow!"}'
```

```json
{"message":"Привет, HotelFlow!"}
```

GET-вариант:

```bash
curl --get --data-urlencode 'message=Привет, HotelFlow!' http://localhost:8080/v1/echo
```

- Swagger UI: <http://localhost:8080/docs/>
- OpenAPI: <http://localhost:8080/swagger.json>

Все проверки доступны с компьютера по умолчанию, без `port-forward`:

| Назначение | Адрес |
|---|---|
| HTTP Echo | `http://localhost:8080/v1/echo` |
| gRPC Echo | `localhost:9090` |
| Liveness | `http://localhost:8081/healthcheck/live` |
| Readiness | `http://localhost:8081/healthcheck/ready` |
| Startup | `http://localhost:8081/healthcheck/startup` |

Используется пакет `internal/pkg/healthcheck` из эталонного gateway.
Admin запускается до основного сервера; liveness=200 и readiness=503 во время подготовки — ожидаемое состояние.
Admin опубликован через отдельный NodePort Service `api-gateway-admin` с `publishNotReadyAddresses: true`: диагностика доступна, даже когда Pod не готов принимать рабочий трафик.
gRPC опубликован через `api-gateway-access`. Оба порта имеют постоянные пробросы k3d.

```bash
curl -sS -i http://localhost:8081/healthcheck/live
curl -sS -i http://localhost:8081/healthcheck/ready
curl -sS -i http://localhost:8081/healthcheck/startup
```

Маршрут HTTP: порт компьютера → load balancer k3d → Traefik Ingress → Service → Pod api-gateway.
Маршрут gRPC и admin: порт компьютера → load balancer k3d → NodePort Service → Pod api-gateway.
По умолчанию все три порта привязаны к `127.0.0.1`. Для доступа из локальной сети выберите привязку при создании кластера:

```bash
HOTEL_BIND_ADDRESS=0.0.0.0 ./hotelctl up
```

Тогда ручка доступна по `http://<IP-компьютера>:8080/v1/echo` при разрешённом входящем соединении на компьютере.
Это локальный учебный HTTP API без TLS и авторизации. Порт Kubernetes API всегда привязан к loopback.
Смена привязки/портов существующего кластера требует его явного пересоздания или нового имени.

Нативный gRPC доступен с компьютера по `localhost:9090`, внутри кластера — по
`api-gateway.hotelflow.svc.cluster.local:9090`. Если установлен grpcurl:

```bash
grpcurl -plaintext -d '{"message":"HotelFlow"}' localhost:9090 api_gateway.echo.v1.EchoService/Echo
```

## Остановка, повторный запуск и диагностика

```bash
./hotelctl status
./hotelctl smoke
./hotelctl logs
./hotelctl stop
./hotelctl up
```

`stop` сохраняет контейнеры и состояние кластера. `up` повторно использует его,
пересобирает изменённый код и выкатывает образ с тегом, производным от image ID.
Если код не изменился, новый rollout не требуется. При ошибке запуска выводятся pods, events и логи gateway.
Порты задаются при создании через `HOTEL_HTTP_PORT`, `HOTEL_GRPC_PORT`, `HOTEL_ADMIN_PORT` и `HOTEL_API_PORT`.
Удаление только этого кластера и его состояния: `./hotelctl destroy`.
Не запускайте несколько `up` одновременно для одного имени кластера.

## Несколько нод на одном компьютере

k3d запускает узлы k3s в Docker-контейнерах. Отдельный демонстрационный кластер с одним server и двумя agent:

```bash
HOTEL_CLUSTER=hotelflow-demo HOTEL_HTTP_PORT=8180 HOTEL_GRPC_PORT=9190 \
  HOTEL_ADMIN_PORT=8181 HOTEL_API_PORT=6551 ./hotelctl up --agents 2
HOTEL_CLUSTER=hotelflow-demo ./hotelctl kubectl get nodes
HOTEL_CLUSTER=hotelflow-demo ./hotelctl kubectl -n hotelflow scale deployment/api-gateway --replicas=3
```

Для дальнейших команд достаточно `HOTEL_CLUSTER=hotelflow-demo`: параметры сохраняются.
Базовый gateway использует одну реплику; несколько узлов сами по себе не увеличивают число реплик.
Все локальные ноды разделяют один компьютер и Docker-среду. Это позволяет демонстрировать drain/отказы worker-нод,
но не обеспечивает устойчивость к потере физического компьютера и не создаёт HA control plane.

## Код и генерация

```text
hotelctl                         # единая точка установки и управления
go.work                          # workspace модулей сервисов
deployment/k3d.yaml               # одноузловой k3s
services/api-gateway/
  api/api-gateway/echo/v1/        # исходный Protobuf-контракт
  cmd/                           # entrypoint
  config/                        # YAML + env
  internal/app/echo/v1/           # реализация EchoService
  internal/pkg/pb/                # сгенерированные Go и OpenAPI
  build/                         # Dockerfile сервиса и генераторов
  build/deployment/              # k3s Deployment, Service, Ingress, probes
  buf.yaml / buf.lock            # зависимости Protobuf через Buf Schema Registry
```

Gateway следует структуре примера: Clay v0.3.0, chi, cleanenv, App с init-функциями,
контроллеры `Implementation`, дескрипторы `ServiceDesc`, grpc-gateway и OpenAPI из Protobuf.
HTTP-преобразование использует сгенерированный `RegisterEchoServiceHandlerServer`, как в примере:
HTTP и gRPC вызывают одну реализацию внутри процесса. Лишнего сетевого вызова HTTP → собственный gRPC нет.
Объявления, заказы и обращения к их downstream-сервисам отсутствуют.

Для изменения контракта:

```bash
make -C services/api-gateway proto-deps  # buf dep update: установить/обновить зависимости
make generate                     # buf lint, build, generate в Docker
make test                         # Go 1.26.1+, интеграционные тесты с race detector
./hotelctl up                     # пересборка и rollout
```

Сгенерированные файлы входят в репозиторий. `make generate` использует Buf v1.57.2,
protoc-gen-go v1.36.10, protoc-gen-go-grpc v1.5.1, gateway/openapiv2 v2.27.3 и goclay v0.3.0.
Protobuf-зависимости `buf.build/googleapis/googleapis` и `buf.build/grpc-ecosystem/grpc-gateway`
описаны в `buf.yaml`. Команда `make -C services/api-gateway proto-deps` выполняет `buf dep update`
и сохраняет конкретные commit ID и контрольные суммы в `buf.lock`.
`make generate` использует этот lock-файл; зависимости обновляются отдельно и явно.

`buf generate` использует remote plugins с версиями из `buf.gen.local.yaml`;
`protoc-gen-goclay` установлен в контейнере локально. Исходники внешних `.proto` берутся из BSR:
директория `third_party` не требуется и в проекте отсутствует.
Для обновления зависимостей и генерации нужен доступ к `buf.build`.
Сборка и запуск готового исходного дерева не обращаются к BSR.
Для генерации через Docker нужен `make`; установка стенда выполняется напрямую через `hotelctl`.

Локальный запуск Go без кластера: `make -C services/api-gateway run`.
Конфигурация: `CONFIG_PATH`, `HTTP_PORT`, `GRPC_PORT`, `ADMIN_HTTP_PORT`, `GRACEFUL_TIMEOUT`.
Жизненный цикл, admin-сервер и closers сохранены по эталону: `internal/app.go` скопирован без изменений, `internal/init.go` адаптирован для Echo и startup probe; перед Stop добавлена синхронизация с инициализацией Clay и убран повторный неограниченный Stop по timeout. Admin запускается до основного сервиса: liveness=200 при readiness/startup=503 до окончания инициализации является ожидаемым состоянием. Пакет healthcheck взят из эталона; в обработке panic исправлен порядок завершения goroutine перед закрытием канала результатов. Сохранена схема closers эталона; Kubernetes дополнительно ограничивает остановку Pod через `terminationGracePeriodSeconds`.

Следующие бизнес-сервисы будут добавляться сюда же, каждый со своей папкой `build/deployment`.
PostgreSQL, Kafka, Redis, etcd и мониторинг пока не разворачиваются: Echo не требует этих зависимостей.

Документация инструментов: [k3d](https://k3d.io/stable/), [k3s](https://docs.k3s.io/),
[grpc-gateway](https://grpc-ecosystem.github.io/grpc-gateway/).
