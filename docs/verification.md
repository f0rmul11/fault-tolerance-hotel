# Проверка фундамента HotelFlow

Дата: 13 сентября 2026 года.

## Окружение

- macOS, Apple Silicon (arm64), Docker через OrbStack.
- Docker Engine 28.3.3; Docker доступны 12 CPU и около 12 ГБ RAM.
- k3d v5.9.0; один server, без agent-нод.
- Kubernetes v1.35.2+k3s1; kubectl v1.35.2.
- Go 1.26.1 для локальных тестов и сборки в Docker.

## Выполненные проверки

- `bash -n hotelctl`: синтаксис установщика корректен.
- `make test`: тесты сервиса прошли с `go test -race ./...`.
  Проверены HTTP POST/GET Echo, нативный gRPC Echo, Unicode и пустые сообщения,
  некорректный JSON, OpenAPI, Swagger UI и завершение по SIGTERM.
- `TestAdminStartsBeforeMain`: admin отвечает по TCP до инициализации основного сервера;
  liveness возвращает 200, readiness и startup — 503.
- `TestHealthLifecycle`: readiness меняется при старте и завершении,
  liveness не зависит от этих флагов.
- Проверен panic внутри healthcheck: возвращается ошибка проверки без гонки закрытия канала.
- `internal/app.go` побайтово совпадает с эталоном, включая отступы и разделение полей App.
- Создан локальный одноузловой k3s и развёрнут gateway из исходников этого монорепозитория.
- Повторный `./hotelctl up` переиспользовал существующий кластер.
- `./hotelctl stop`, затем `./hotelctl up`: кластер восстановился, gateway — `1/1 Running`.
- `./hotelctl smoke` после перезапуска: HTTP POST/GET, OpenAPI и все три healthcheck прошли.
- Нативный gRPC Echo вызван Go-клиентом с компьютера через `localhost:9090`, в том числе после перезапуска.
- Постоянные порты Docker проверены: `127.0.0.1:8080`, `127.0.0.1:9090`, `127.0.0.1:8081`.
  Для этих проверок `kubectl port-forward` не использовался.

## Диагностика неготового Pod

В работающем кластере временно изменён путь Kubernetes readiness-пробы на несуществующий.
После перехода единственного Pod в `Running`, `Ready=False`:

- Все три настоящих healthcheck на `localhost:8081` оставались доступны и возвращали 200.
  Само приложение было исправно; отказ намеренно внесён в путь Kubernetes-пробы.
- HTTP Ingress на `localhost:8080` возвращал 503: рабочий трафик к неготовому Pod не поступал.
- Рабочая readiness-проба и стратегия rollout восстановлены; Deployment снова готов.

Доступ к admin обеспечивается отдельным Service `api-gateway-admin` с
`publishNotReadyAddresses: true`. Механизм описан в
[документации Kubernetes об EndpointSlice](https://kubernetes.io/docs/concepts/services-networking/endpoint-slices/#ready).
Состояние самого приложения «liveness=200, readiness=503» отдельно проверено тестами жизненного цикла.

## Проверка Buf Schema Registry

13 сентября 2026 года после восстановления подключения:

- Локальный Buf CLI v1.73.0 успешно выполнил `buf registry module info` для
  `buf.build/googleapis/googleapis` и `buf.build/grpc-ecosystem/grpc-gateway`.
- `make -C services/api-gateway proto-deps` выполнил `buf dep update` в Docker с Buf v1.57.2.
  В `buf.lock` сохранены commit ID и контрольные суммы обеих зависимостей, полученные из BSR.
- `make generate` успешно выполнил `buf lint`, `buf build` и `buf generate`.
  Go, gRPC, grpc-gateway и OpenAPI сгенерированы remote plugins с закреплёнными версиями;
  Clay-обвязка — локальным `protoc-gen-goclay` v0.3.0 в контейнере.
- Все пять пересозданных файлов в `internal/pkg/pb/api-gateway/echo/v1` побайтово совпали
  с сохранённой до генерации версией.
- `make test` после генерации прошёл с race detector, включая HTTP/gRPC Echo и жизненный цикл.
- Каталог `third_party` отсутствует; внешние Protobuf-зависимости разрешаются через BSR.

Стенд собирается из сохранённых сгенерированных файлов и для запуска не обращается к Buf.

## Непроверенные сценарии

Полный запуск на Linux, Windows/WSL2 и amd64 пока не проверен.
Установщик содержит соответствующие платформенные ветки.
Многоузловой вариант описан в README и поддерживается параметром `--agents`,
но в этой проверке разворачивался только один узел.
Это проверка фундамента с Echo; бизнес-сервисы, нагрузочные сценарии и отказоустойчивость полного стенда ещё не реализованы.
