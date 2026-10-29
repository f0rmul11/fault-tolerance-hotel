# Service Overview

Откройте [дашборд](http://localhost:3000/d/hotelflow-service-overview) после `hotelctl up`. В Grafana он находится в папке **HotelFlow**. Выберите сервис в поле **Service**: список поддерживает поиск по имени. **Protocol** позволяет отделить HTTP от gRPC; по умолчанию показаны оба входа. Нажатие на заголовок **Errors** сворачивает все три панели.

## Панели и значения

| Панель | Что показывает |
| --- | --- |
| Response codes ratio | Доли завершённых запросов по категориям, сумма областей — 100%. Легенда справа содержит исходный поток в req/s: Mean, Last, Max, Min за выбранный диапазон |
| Error rate by handler (only errors) | Частоту технических ошибок в req/s по HTTP-маршруту или полному имени RPC |
| Error rate by handler (only client errors) | Частоту отказов на входящие запросы: неверные аргументы, отсутствие прав, конфликт бизнес-состояния |

**Client errors** здесь означают ошибки запросов клиентов **к выбранному сервису**. Это не ошибки его исходящих gRPC-клиентов. `not_found` и `canceled` показаны отдельно в верхней панели и не входят в два нижних графика.

| `code` — категория | gRPC `status` | HTTP `status` |
| --- | --- | --- |
| `ok` | OK | < 400 |
| `canceled` | Canceled | 499 |
| `not_found` | NotFound | 404 |
| `client_error` | InvalidArgument, AlreadyExists, PermissionDenied, Unauthenticated, FailedPrecondition, Aborted, OutOfRange | Прочие 4xx |
| `error` | Unknown, DeadlineExceeded, ResourceExhausted, Unimplemented, Internal, Unavailable, DataLoss | 5xx |

Это политика дашборда, а не доказательство виновности клиента: например, PermissionDenied может быть следствием неверной настройки сервера. ResourceExhausted в стенде относится к техническим сбоям; HTTP 429 — к клиентским отказам. Смысл конкретного ответа уточняйте по логам и трейсу. [Значения gRPC-кодов](https://grpc.io/docs/guides/status-codes/).

Легенда показывает статистику **скорости запросов**, а не число запросов за весь период. Для приблизительного количества используйте `increase` в Explore:

```promql
sum by (code, status) (
  increase(hotelflow_server_requests_total{
    service_namespace="hotelflow", service_name="api-gateway"
  }[5m])
)
```

Метрика сохраняет точный статус отдельно от категории. Для его детализации:

```promql
sum by (handler, status) (
  rate(hotelflow_server_requests_total{
    service_namespace="hotelflow", service_name="api-gateway", code="error"
  }[1m])
)
```

## Где считаются метрики

Все шесть сервисов регистрируют два инструмента в стандартном глобальном Prometheus registry:

- `hotelflow_server_requests_total{protocol,handler,code,status}` — один increment на завершённый входящий запрос.
- `hotelflow_server_request_duration_seconds{protocol,handler,code}` — histogram серверной длительности в секундах; доступна в Explore, отдельная latency-панель пока не добавлена.

`service_name`, namespace и instance добавляет существующий telemetry SDK. Путь данных не меняется: Prometheus registry → OTel Prometheus bridge → OTLP → единый Collector → Prometheus. У баз данных и Kafka ничего дополнительно не собирается.

Для gRPC `grpc.InTapHandle(metrics.TapHandler)` сохраняет время поступления запроса на транспорт. Hook не блокирует I/O-поток и не принимает решений о допуске. `grpc.StatsHandler(metrics.ServerHandler())` фиксирует итоговый код и длительность на `stats.End`: сюда входят ожидание запуска RPC после tap, чтение/декодирование тела, interceptors, handler и серверная обработка отправки ответа. Таймер не заканчивается на возврате бизнес-handler. Поддерживаются unary и streaming RPC; для stream это длительность всего вызова, не отдельного сообщения.

Это **серверное** время: оно не включает подключение клиента, путь до сервера и не подтверждает доставку ответа клиенту. Отказы HTTP/2 до tap, отклонение в отдельном admission tap и несуществующие gRPC-методы вне зарегистрированного обработчика могут завершиться без `stats.End`; эти метрики не заменяют транспортные метрики прокси. Текущий tap ничего не отклоняет. [Контракт tap](https://pkg.go.dev/google.golang.org/grpc/tap), [события stats](https://pkg.go.dev/google.golang.org/grpc/stats).

У API Gateway внешний HTTP middleware измеряет фактический HTTP-ответ, включая ошибки JSON и routing. Локальный generated gRPC adapter не создаёт второй сетевой gRPC запрос. В labels попадает шаблон маршрута (`/v1/rooms/{room_id}`), а не ID или query string. Неизвестные HTTP-маршруты объединяются в `unmatched`. Healthcheck, reflection, swagger, docs и debug исключены. Бизнес-слои не содержат вызовов метрик.

Известные статусы экспортируются с нулём после первого обращения к обработчику, чтобы новая категория ошибки имела базовую точку для `rate`. До первого обращения обработчик может отсутствовать. Экспорт идёт раз в 15 секунд; для графика нужны хотя бы две точки. Dashboard использует `$__rate_interval` с шагом не меньше 15 секунд, поэтому короткий сбой растягивается окном `rate`. Не стоит ожидать точного совпадения фронтов со временем команды fault.

**Нулевая линия и отсутствие данных не доказывают доступность.** При отсутствии запросов все rates равны нулю, а доли математически не определены. При убийстве процесса его незавершённые запросы не успеют экспортироваться. Если Envoy отклонил запрос до входа в Hotels, ошибку увидит API Gateway, а Hotels её не получал. Для демонстрации такой связи смотрите вызывающий сервис и его трейсы. После рестарта `rate` вычисляется отдельно по instance до суммирования, поэтому сбросы counters не становятся отрицательными скачками.

## Демонстрация

Ниже read-only запросы к каталогу; брони и БД не очищаются. Дайте обычной нагрузке поработать около минуты до сбоя и после восстановления.

```bash
./bin/hotelctl load --addr localhost:9090 \
  --method api_gateway.hotels.v1.HotelsService/ListHotels \
  --data '{}' --rps 10 --duration 3m
```

В другом терминале:

```bash
./bin/hotelctl fault api-gateway hotels --unavailable --duration 15s
```

На Service Overview выберите **api-gateway**, Protocol **grpc**: вырастет категория `error` и rate для ListHotels. `fault` сам снимает сбой. Для таймаута можно повторить с `--slowdown 8s --duration 15s`. После восстановления продолжайте нагрузку: график должен вернуться к нулю с учётом окна `rate`.

Клиентский отказ можно вызвать отдельно через `GET http://localhost:8080/v1/rooms/not-a-number`: ожидается HTTP 422 и `client_error` на API Gateway; его причина видна как InvalidArgument на Hotels. Повторите несколько раз после появления первой точки метрики. Для такой проверки выберите Protocol **http**.

## Хранение дашборда

JSON находится в `deploy/observability/dashboards/service-overview.json`, provider — в `deploy/observability/grafana-dashboards.yaml`. Kustomize создаёт ConfigMaps и монтирует их в Grafana; `hotelctl deploy` применяет их вместе со стендом. Изменения provisioned-дашборда сохраняйте в JSON репозитория. Для самостоятельного эксперимента сохраните копию дашборда через Save as.
