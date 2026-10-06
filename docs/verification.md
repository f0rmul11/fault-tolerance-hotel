# Проверка HotelFlow

## Фундаменты сервисов — 16 сентября 2026 года

- Добавлены availability, booking, check-in, room, notification, search: по одному готовому Pod в k3s.
- Каждый сервис — отдельный Go-модуль. App и отступы совпадают с gateway после замены имени модуля.
- Clay сохранён. Генерируются только protobuf и gRPC; HTTP RPC в новых сервисах не регистрируются.
- Buf v1.57.2 выполнил dep update, lint, build и generate для всех шести контрактов.
  Плагины Go v1.36.10 и Go gRPC v1.5.1 закреплены в Dockerfile.codegen.
  Внешних Protobuf-зависимостей у этих контрактов нет, buf.lock и third_party не требуются.
- `make test`: все семь сервисов и CLI прошли с race detector.
  Проверены Echo, Unicode/пустое сообщение, имя ответившего сервиса, reflection,
  отсутствие HTTP RPC, ранний запуск admin, liveness/readiness/startup и SIGTERM.
- Каждый образ собран независимо от go.work, затем развёрнут командой `hotelctl up`.
- `hotelctl smoke` вызвал настоящий gRPC Echo каждого сервиса с компьютера и проверил admin.
- Временный Pod успешно вызвал все семь сервисов по `<service>.hotelflow.svc.cluster.local:9090`.
  Проверочный Pod удалён.
- У availability временно изменён путь readiness-пробы: при PodNotReady все три admin-ручки
  оставались доступны на 127.0.0.1:8082, а рабочий gRPC EndpointSlice имел ready=false.
  Исходная проба и rollout восстановлены; общий smoke после этого прошёл.
- Проверены значения по умолчанию и переопределения короткого hotel.yaml, дубли портов,
  публикация admin в манифестах и сохранение тома/владельца при обновлении Docker-контейнера.
- hotel.yaml сокращён до версии k3s и списка сервисов (11 строк).
- Во время проверки BSR отдавал 403 при загрузке модулей, хотя чтение метаданных работало.
  Генерация новых контрактов успешно выполнена локальными плагинами Buf и не зависит от BSR.
  Конфигурация генерации api-gateway не менялась.

Дата миграции CLI: 14 сентября 2026 года.

## Окружение

- macOS arm64, Go 1.26.1, Docker Engine 28.3.3 через OrbStack.
- Один контейнер `rancher/k3s:v1.35.2-k3s1`; узел `hotelflow-k3s`.
- CLI использует Docker SDK и client-go. k3d и локальный kubectl не требуются.

## Автоматические проверки

- `make test`: тесты CLI и gateway прошли с race detector.
- Отдельный модуль CLI также проверен через `GOWORK=off go test -race ./...`.
- Проверены ошибочные аргументы до создания runtime, справка без Docker/проекта,
  выбор сервисов и ограничение времени команды.
- Проверены блокировка параллельных операций, атомарное сохранение состояния,
  права 0600, отказ от использования повреждённого состояния и удаления чужих ресурсов.
- Частичный сбой запуска сохраняет владельца для повторного `up`;
  сбой удаления сохраняет состояние для повторного `destroy`.
- JSON-диагностика возвращает ошибку выполнения при недоступности Kubernetes.
- Проверены Kustomize-манифесты, подстановка версии образа и ожидание новой ревизии Deployment.
- Сохранились тесты HTTP/gRPC Echo, Unicode, некорректного JSON, OpenAPI и SIGTERM.
- `TestAdminStartsBeforeMain`: admin уже отвечает до основного сервера;
  liveness — 200, readiness и startup — 503.
- `internal/app.go` побайтово совпадает с эталоном.
- CLI собран для macOS arm64, Linux amd64 и Windows amd64.

## Локальный стенд

- Новый `hotelctl up` создал контейнер k3s, сеть, постоянный том и gateway из исходников.
- После исправления вызова встроенного `ctr` повторный запуск продолжил незавершённое развёртывание.
- `hotelctl stop`, затем `hotelctl up`: кластер восстановился, gateway — `1/1 Running`.
- `doctor`, `status -o json`, `logs api-gateway` и `kubectl get nodes` работают.
- `smoke`: HTTP POST/GET Echo, OpenAPI, три healthcheck и TCP-порт gRPC доступны с компьютера.
- Дополнительно выполнен настоящий gRPC Echo Go-клиентом через `127.0.0.1:9090`.
- HTTP — `127.0.0.1:8080`, admin — `127.0.0.1:8081`, Kubernetes API — `127.0.0.1:6550`.
  `port-forward` не использовался.

## Неготовый Pod

Временно задан несуществующий путь Kubernetes readiness-пробы. При единственном Pod
в состоянии `Running`, `Ready=False` все три настоящих healthcheck на порту 8081
оставались доступны и возвращали 200. Приложение было исправно; отказ внесён в Kubernetes-пробу.

Рабочий HTTP NodePort не направлял запрос к неготовому Pod: соединение закрывалось без HTTP-ответа.
Admin доступен благодаря `publishNotReadyAddresses: true` у отдельного Service.
После проверки исходная проба и стратегия rollout восстановлены, gateway снова готов.
Сочетание «liveness=200, readiness=503» отдельно проверено тестами жизненного цикла приложения.

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

## Границы проверки

Полный запуск выполнен на macOS arm64. Для Linux и Windows amd64 проверена сборка CLI;
запуск с Docker на этих платформах ещё не проверен.
Созданы gateway и фундаменты шести сервисов с Echo. Бизнес-операции, хранилища и сценарии отказоустойчивости предстоит добавить.
