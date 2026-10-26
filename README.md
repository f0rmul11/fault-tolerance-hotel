# HotelFlow

Учебный гостиничный стенд: один k3s-узел, API Gateway и пять сервисов. Booking атомарно сохраняет подтверждённую бронь конкретного номера, период бронирования, ключ идемпотентности и outbox. Hotels хранит каталог и ограничения длительности (seed: минимум 12 часов). Пересечения исключаются ограничением PostgreSQL EXCLUDE USING gist. Во всех сервисах сохранены gRPC Echo и probes.

## Запуск

Нужны Go 1.26.1+ и запущенный Docker с Linux-контейнерами.
Из корня проекта:

```bash
go run ./hotelctl/cmd/hotelctl up
```

Первый запуск требует интернета. CLI запускает k3s, собирает сервисы и ждёт готовности Kubernetes-компонентов. Проверка API запускается отдельно через `hotelctl smoke`.
Конфигурация — `hotel.yaml`, исходники CLI — `hotelctl/`, сервисов — `services/`.

## Команды

```bash
make build                              # собрать ./bin/hotelctl
./bin/hotelctl status                    # состояние стенда
./bin/hotelctl build api-gateway         # собрать сервис
./bin/hotelctl deploy api-gateway        # развернуть собранный образ
./bin/hotelctl logs api-gateway -f        # следить за логами
./bin/hotelctl smoke                     # проверить API с компьютера
./bin/hotelctl stop                      # остановить, сохранив данные
make test                               # тесты CLI и всех сервисов
make generate                           # Buf: lint, build и генерация всех сервисов
make -C services/booking generate       # собственный API и внешний клиент Hotels
```

Контракты находятся в `api` каждого сервиса; общий protobuf отсутствует. `buf.gen.local.yaml` генерирует свой API, `buf.gen.external.yaml` — клиенты из `api` соседнего сервиса. Штатная генерация требует только Docker: версии Buf и плагинов закреплены в codegen-образах. `generate-local` выполняет ту же цепочку при уже установленных Buf и плагинах.

## Все адреса для локального запуска

Адреса ниже доступны после `hotelctl up` со штатным `hotel.yaml`. В колонке «Порт» указан порт **на вашем компьютере**, а не внутри контейнера или k3s. Для опубликованных адресов `port-forward` не нужен. Grafana и Jaeger запускаются при `observability.enabled: true`.

| Компонент / назначение | Локальный endpoint | Порт | Как использовать |
| --- | --- | --- | --- |
| API Gateway — HTTP API | [http://localhost:8080/v1/echo?message=Hello](http://localhost:8080/v1/echo?message=Hello) | `8080` | Проверка Echo; остальные HTTP-методы `/v1/...` описаны в Swagger |
| Swagger UI | [http://localhost:8080/docs/](http://localhost:8080/docs/) | `8080` | Документация и выполнение запросов к HTTP API |
| OpenAPI / Swagger JSON | [http://localhost:8080/swagger.json](http://localhost:8080/swagger.json) | `8080` | Схема API для импорта в клиент |
| API Gateway — gRPC | `127.0.0.1:9090` | `9090` | gRPC-клиент, соединение без TLS |
| Booking — gRPC | `127.0.0.1:9091` | `9091` | Бронирование |
| Check-in — gRPC | `127.0.0.1:9092` | `9092` | Заселение, отмена и список заездов |
| Review — gRPC | `127.0.0.1:9093` | `9093` | Рейтинги отелей |
| Notification — gRPC | `127.0.0.1:9094` | `9094` | Служебный Echo; уведомления поступают через Kafka |
| Hotels — gRPC | `127.0.0.1:9095` | `9095` | Каталог отелей и номеров |
| API Gateway — admin | [http://localhost:8081/healthcheck/ready](http://localhost:8081/healthcheck/ready) | `8081` | Готовность; [pprof](http://localhost:8081/debug/) |
| Booking — admin | [http://localhost:8082/healthcheck/ready](http://localhost:8082/healthcheck/ready) | `8082` | Готовность; [pprof](http://localhost:8082/debug/) |
| Check-in — admin | [http://localhost:8083/healthcheck/ready](http://localhost:8083/healthcheck/ready) | `8083` | Готовность; [pprof](http://localhost:8083/debug/) |
| Review — admin | [http://localhost:8084/healthcheck/ready](http://localhost:8084/healthcheck/ready) | `8084` | Готовность; [pprof](http://localhost:8084/debug/) |
| Notification — admin | [http://localhost:8085/healthcheck/ready](http://localhost:8085/healthcheck/ready) | `8085` | Готовность; [pprof](http://localhost:8085/debug/) |
| Hotels — admin | [http://localhost:8086/healthcheck/ready](http://localhost:8086/healthcheck/ready) | `8086` | Готовность; [pprof](http://localhost:8086/debug/) |
| Grafana — метрики и логи | [http://localhost:3000](http://localhost:3000) | `3000` | Логин `admin`; получение пароля — под таблицей. Explore → Prometheus или Loki |
| Jaeger — трейсы | [http://localhost:16686](http://localhost:16686) | `16686` | Поиск трейсов по сервису и trace ID |
| AKHQ — Kafka UI | [http://localhost:8090](http://localhost:8090) | `8090` | Выбрать кластер `hotelflow`; просмотр топиков, сообщений и consumer groups |
| PostgreSQL — Booking | `127.0.0.1:15432` | `15432` | БД `booking`, пользователь `booking` |
| PostgreSQL — Check-in | `127.0.0.1:15433` | `15433` | БД `checkin`, пользователь `checkin` |
| PostgreSQL — Hotels | `127.0.0.1:15434` | `15434` | БД `hotels`, пользователь `hotels` |
| PostgreSQL — Review | `127.0.0.1:15435` | `15435` | БД `review`, пользователь `review` |
| etcd — runtime-конфигурация | `http://127.0.0.1:2379` | `2379` | `hotelctl config get/set` или etcd-клиент; [health](http://127.0.0.1:2379/health) |
| Kubernetes API | `https://127.0.0.1:6550` | `6550` | `kubectl --kubeconfig=.local/hotelctl/kubeconfig ...` или `hotelctl kubectl ...` |
| Prometheus — API и собственный UI | Прямой локальный endpoint не опубликован | — | Метрики доступны через Grafana → Explore → Prometheus; внутри k3s `http://prometheus:9090` |
| Loki — API логов | Прямой локальный endpoint не опубликован | — | Логи доступны через Grafana → Explore → Loki; внутри k3s `http://loki:3100` |
| OTel Collector — приём телеметрии | Прямой локальный endpoint не опубликован | — | Сервисы в k3s отправляют OTLP на `otel-collector:4317` (gRPC); также есть `4318` (HTTP) |
| Kafka — брокер | Прямой локальный endpoint не опубликован | — | Для просмотра используйте AKHQ; внутри k3s `kafka:9092`. Локальный `9092` занят gRPC Check-in |

Все gRPC endpoints работают без TLS и открываются gRPC-клиентом, например `grpcurl -plaintext`, а не браузером.
На каждом admin-порту доступны `/healthcheck/live`, `/healthcheck/ready`, `/healthcheck/startup` и `/debug/` (pprof). В таблице ссылки ведут на readiness; ответ `503` означает, что приложение ещё не готово. Admin-порт публикуется и для неготового Pod, пока сам процесс и admin-сервер работают.

**Вход в Grafana:** логин `admin`, пароль создаётся при первом запуске и хранится в Kubernetes Secret. Получить текущий пароль из корня проекта:

```bash
./bin/hotelctl kubectl -n hotelflow get secret grafana-admin -o jsonpath='{.data.admin-password}' | base64 -d
```

**Подключение к PostgreSQL:** SSL отключён (`sslmode=disable`), пароли находятся в [deploy/booking-dependencies/credentials.yaml](deploy/booking-dependencies/credentials.yaml). Для подключения по именам `booking-postgres`, `check-in-postgres`, `hotels-postgres`, `review-postgres` вместо `127.0.0.1` один раз выполните `sudo ./bin/hotelctl hosts --install`; порты остаются теми же.

Если изменили порты или порядок сервисов в `hotel.yaml`, проверьте актуальные адреса командами `./bin/hotelctl access` и `./bin/hotelctl status`. [Все команды CLI](hotelctl/README.md).

## Бронирование

Атомарное бронирование, идемпотентность и outbox: [Booking](services/booking/README.md).

Заселение/отмена и Kafka request/reply: [Check-in](services/check-in/README.md).

Каталог отелей и номеров: [Hotels](services/hotels/README.md). Пользовательские рейтинги отелей: [Review](services/review/README.md).

Gateway задаёт общий deadline **5 секунд** на обработку входящего HTTP или unary gRPC запроса,
включая downstream-вызовы. `X-Request-Timeout: 2s` сокращает HTTP-бюджет; увеличить его выше
5 секунд нельзя. Более ранний deadline клиента сохраняется. При истечении ожидания RPC —
`DeadlineExceeded` / HTTP 504; это не доказывает, что бронь не была сохранена.

## Runtime-конфигурация

В k3s разворачивается отдельный `config-etcd` с постоянным PVC. После `hotelctl up`
локальный адрес из [общей таблицы](#все-адреса-для-локального-запуска) доступен без туннеля; внутри кластера — `http://config-etcd:2379`.
Команды конфигурации: `./bin/hotelctl config get/set`. Reader запускается в API Gateway;
проверка флага в HTTP/gRPC обработчиках пока не добавлена.

## Доступ для демонстрации и управление компонентами

```bash
./bin/hotelctl components                 # все имена приложений и зависимостей
./bin/hotelctl stop kafka                 # остановить брокер; сохранить данные
./bin/hotelctl start kafka                # вернуть исходное число реплик
./bin/hotelctl restart booking            # остановка и запуск без пересборки
./bin/hotelctl logs booking-postgres -f    # логи также доступны для зависимостей
./bin/hotelctl access                     # адреса API, UI и PostgreSQL
sudo ./bin/hotelctl hosts --install       # один раз: локальные имена БД
```

`stop` без имён по-прежнему останавливает весь k3s. `status` показывает приложения, зависимости и включённые компоненты observability.
Адреса PostgreSQL и параметры подключения приведены в [общей таблице](#все-адреса-для-локального-запуска).
`hotelctl up` обновляет публикацию портов существующего контейнера, сохраняя том k3s.

## Kafka UI

AKHQ доступен сразу после `hotelctl up`; адрес — в [общей таблице](#все-адреса-для-локального-запуска).
Отдельный `port-forward` не нужен. Выберите `hotelflow` → топик → Data → Oldest.
Видны payload, key, headers, partition и offset. Kafka внутри кластера: `kafka:9092`;
наружу опубликован UI, а не брокер с внутренним advertised address.

Полный сценарий бронирования, заселения, отмены и уведомлений: `make smoke-business`.
[Что проверяет команда и какие данные оставляет](hotelctl/README.md#проверка-бизнес-сценария).

Согласованный сброс учебных БД, Kafka и восстановление seed: `make reset`.
Подробности и продолжение после прерывания — [hotelctl](hotelctl/README.md#сброс-учебных-данных).

## Наблюдаемость

OpenTelemetry Collector, Jaeger, Loki, Prometheus и Grafana включены вместе через `observability.enabled: true` в `hotel.yaml`.
Адреса Jaeger и Grafana, а также получение пароля приведены в [общей таблице](#все-адреса-для-локального-запуска). Трейсы, логи и метрики глобального Prometheus registry проходят через единый OTel Collector. Стандартные Go/process-метрики включены; собственные метрики регистрируются через prometheus/promauto.
Первый `up` с новыми портами перезапускает контейнер k3s с сохранением его тома.
[Устройство, команды и ограничения](deploy/observability/README.md).
