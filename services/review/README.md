# Review

Review владеет агрегатами пользовательских оценок отелей и топиком `review.rating.events.v1`.
Кеш и его инвалидация находятся в Hotels. Вызовов Hotels и проверки существования отеля по сети здесь нет.

## API

- `GetHotelRatings(hotel_ids)` читает всю коллекцию одним SQL, возвращая рейтинг, количество оценок и `rating_version` из одного снимка строки.
- `AddHotelRating(hotel_id, stars)` добавляет целую оценку от 1 до 5. Возвращает новый агрегат после commit рейтинга и outbox. Для нового hotel_id создаётся агрегат с версией 1.
- Повтор AddHotelRating означает новую оценку. HTTP-идемпотентность и автоматический повтор записи не добавлены. Конфликт конкурентного сохранения возвращает gRPC Aborted; некорректный UUID или оценка — InvalidArgument.

Сумма и количество сохраняются целыми, среднее вычисляется при чтении без округления. Версия меняется доменным методом; storage сохраняет с проверкой предыдущей версии. Сохранение агрегата и `event.Buffer.Flush` выполняются в одной `transaction.Exec`.

## События и outbox

`review.rating.changed.v1`: `hotel_id`, `rating_version`, `occurred_at`, `correlation_id`, пустой `payload`. Request ID не добавляется; span-контекст передаётся в Kafka headers. `occurred_at` — время изменения сущности, не точная отметка commit PostgreSQL.

Kafka key — hotel_id. Envelope и `x-correlation-id` содержат один UUID, сохраняющийся при повторной отправке outbox. Доставка at-least-once; после send и до commit возможен дубль. Событие уведомляет об изменении, полный рейтинг читается через API.

Outbox/event/pipe/worker перенесены из уже адаптированного образца order-service в Check-in. Relay работает всегда. Конфиг совпадает с Check-in: `outbox.limits`, `outbox.topics.<topic>.batch_size`, `worker.interval` и `worker.concurrency`. Метрики outbox не включены. Worker завершается до закрытия producer и PostgreSQL.

Producer использует WaitForAll, идемпотентный режим и hash partitioner. Недоступность Kafka во время работы оставляет сообщения pending; relay повторяет отправку. При недоступности Kafka во время создания producer запуск сервиса может завершиться ошибкой. Версии событий используются потребителем для интерпретации изменений; exactly-once и отсутствие запоздалых повторов не обещаются.

## Топик и миграции

Топик `review.rating.events.v1` принадлежит Review и создаётся через `hotelctl` в init-контейнере `topics` общего migration Job, как `booking.events.v1` и `checkin.events.v1`. Используется `--if-not-exists`, 3 партиции, RF=3 и minISR=2. Топик также включён в общий механизм `hotelctl reset`.

Исходная миграция ratings обновлена inplace по правилам учебного стенда. Для уже существующей БД перед запуском нового Review нужно явно применить добавление колонок, без удаления рейтингов:

```sql
alter table public.ratings add column if not exists version bigint default 1;
alter table public.ratings add column if not exists updated_at timestamptz default current_timestamp;
```

Затем `make migrate-review` с `POSTGRES_DSN` этой БД применяет новую миграцию outbox. На новой БД достаточно всех миграций. Генерация xo выполняется отдельно через `make xo-review` на изолированной мигрированной БД. Proto — `make -C services/review generate`; клиенты других сервисов в этой задаче не меняются.

## Проверки

```bash
cd services/review
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
# Отдельная мигрированная тестовая PostgreSQL:
REVIEW_TEST_POSTGRES_DSN=... go test -tags=integration ./internal/application/usecase/rating/add -v
```

Unit-тесты проверяют доменное изменение, ошибочные оценки, согласованность события, общий transaction context, rollback при ошибках и передачу ошибки commit. Отдельная интеграционная проверка выполняет SQL, настоящий rollback рейтинга при ошибке outbox и детерминированную гонку двух записей с одной исходной версией.

Пример вызова напрямую в локальный Review:

```bash
grpcurl -plaintext -d '{"hotel_id":"cdd4685e-c610-4e7e-95ee-c51fb6807400","stars":5}' localhost:9093 review.review.v1.ReviewService/AddHotelRating
grpcurl -plaintext -d '{"hotel_ids":["cdd4685e-c610-4e7e-95ee-c51fb6807400"]}' localhost:9093 review.review.v1.ReviewService/GetHotelRatings
```

## Load shedding (перенос старого стенда)

Пакет `internal/pkg/loadshedding` перенесён из
`fault-tolerant-stand/services/review-service/internal/pkg/loadshedding`.
Используется та же версия `github.com/platinummonkey/go-concurrency-limits v0.10.0`.
Расчёты Vegas и очередь библиотеки сохранены; добавлены наблюдение через MetricRegistry,
метрики допуска и структурированные логи изменения лимита.

В `config/config.yaml` защита включена только для
`/review.review.v1.ReviewService/GetHotelRatings`. Echo и HTTP healthcheck не входят
в этот лимит. Каждый процесс Review имеет собственный limiter; это не общий лимит
кластера и не ограничение RPS. Один batch RPC занимает один слот независимо от
числа hotel_id. Параметры защищённого метода сохранены из старого YAML:

| Параметр | Значение |
| --- | --- |
| Начальный / максимальный concurrency | 40 / 10000 |
| Backlog (LIFO) / максимальное ожидание | 1000 / 2s |
| Размер выборки / интервал пересчёта | 100 / 1–2s |
| Минимальный учитываемый RTT | 1ms |
| Сглаживание / множитель probe | 1.0 / 1 |
| Коэффициенты alpha / beta | 3 / 6, явно |

Порядок: `load shedding → error mapping → demo delay → handler → PostgreSQL`.
Limiter получает уже преобразованные gRPC-ошибки. Его собственный отказ выходит
как `ResourceExhausted`, без повторного преобразования в `Internal` внутри Review.

### Что именно демонстрирует код

1. При свободном слоте запрос допускается в обработчик.
2. При исчерпании concurrency запрос ждёт в ограниченном backlog. При освобождении
   слота приоритет получает более свежий запрос (LIFO). Это не гарантирует fairness.
3. Полный backlog, истёкшее ожидание либо отмена контекста во время ожидания приводят
   к отказу в Acquire; interceptor возвращает `ResourceExhausted`. Клиентский gRPC
   при собственном истёкшем deadline может наблюдать `DeadlineExceeded` вместо него.
4. После допуска измеряется длительность до возврата обработчика, включая demo delay
   и ожидание/работу PostgreSQL. Ожидание в backlog в этот RTT **не входит**.
5. Vegas получает **минимальный** успешный RTT окна и максимальный observed in-flight.
   В библиотеке обновление требует **больше** `window_size` успешных учитываемых
   измерений, то есть минимум 101 при значении 100, и наступившего срока обновления.
   Интервал 1–2s не является таймером: без завершений и выборки пересчёта не будет.
6. Оценка очереди — `ceil(limit * (1 - RTT_no_load / RTT))`, в запросах, не в байтах.
   Это оценка по задержке, а не измеренный размер backlog. Alpha/beta зависят от
   текущего limit через `Log10RootFunction`, коэффициенты 3/6 — не фиксированные
   пороги 3/6 запросов при любом лимите. Есть сглаживание и обновление базового RTT.
   Это прикладная адаптация Vegas, не реализация TCP slow start/ACK/retransmission.

### Ограничения сохранённого исходника

- В `config.go:enrich` alpha ошибочно наследуется из beta. В YAML оба коэффициента
  заданы прямо у защищённого метода, поэтому этот дефект не меняет его настройки.
- Значения `0` считаются отсутствующими. `max_backlog_size: 0` не выключает очередь.
  `enabled: false` метода наследует `true` из default, если default включён. Поэтому
  default оставлен выключенным; выключать защиту нужно через `handlers[0].enabled`.
- Ошибка создания limiter (например, `window_size: 1` или min window > max window)
  молча отключает защиту метода: fail open. Встроенной валидации этих параметров нет.
  Проверен текущий корректный YAML; произвольные изменения требуют отдельной проверки.
- Локальный reject до допуска не вызывает `OnDropped`. `ResourceExhausted`, вернувшийся
  из handler, исходник классифицирует как permanent (`OnIgnore`). Для `Internal`,
  `Unknown`, `Unavailable`, `Canceled`, `DeadlineExceeded` вызывается `OnDropped`.
  Отмена клиентом не обязательно означает перегрузку, но здесь трактуется именно так.
- `OnDropped` учитывается при обновлении окна; поток одних ошибок без достаточного
  числа успешных RTT не гарантирует уменьшение лимита. Уменьшение в этой версии
  библиотеки аддитивное с логарифмическим шагом, не уменьшение вдвое при каждом timeout.
- При panic в handler освобождение токена не защищено `defer`. Внешний recover,
  позволяющий процессу продолжить работу, может оставить слот занятым.
- Метрики подключены через штатный `core.MetricRegistry` и существующий telemetry
  bridge Prometheus → OTLP. Раздел **Load shedding** в Service Overview показывает
  лимит/активные токены, реальный backlog с wait p95 справа, admission admitted/rejected, RTT Vegas,
  оценку очереди с alpha/beta. Общая latency-панель не менялась.
- `load_shedding_estimated_limit{handler}` обновляется callback Vegas, `inflight` и
  `backlog` читаются живыми suppliers и возвращаются к нулю после завершения вызовов.
  `vegas_rtt_seconds` — MIN успешных RTT окна DefaultLimiter, без admission wait;
  `vegas_min_rtt_seconds` — текущий baseline, включая probe reset.
- `vegas_queue_estimate` восстановлена из целого limit перед решением; при дробном
  внутреннем limit возможна разница в один запрос. Это не admission backlog.
  Alpha/beta показываются для текущего limit. При смене baseline точка
  решения отсутствует (NaN). Во время простоя RTT/оценка сохраняют последний sample.
- `admissions_total{result="rejected"}` включает полный backlog, истечение ожидания и
  отмену context: библиотека не различает их. Admitted обозначает допуск, не бизнес-успех.
  Реальные статусы — в Errors.
- `acquire_duration_seconds` измеряет длительность Acquire всех допущенных и отклонённых
  попыток: ожидание допуска плюс небольшие накладные расходы, без выполнения handler.
  На панели admission backlog справа показан wait p95 за 15s; слева остаётся число
  ожидающих запросов и capacity. Границы histogram уплотнены около таймера 200ms.
- Изменение целого лимита пишет Info «Изменён лимит load shedding»: old_limit/new_limit,
  sample_rtt_ms/baseline_rtt_ms, queue_estimate и did_drop. Нет лога на каждый RPC.
  В общем режиме лимитер помечается handler="__shared__".
- Hotels сохраняет downstream `ResourceExhausted` через отдельный app.Code;
  API Gateway возвращает клиенту gRPC 8 / HTTP 429 с безопасным русским пояснением.
  Отказ перегрузки не превращается в Internal.

### Конфигурация и демонстрация

Конфигурация читается при запуске, hot reload/etcd для этого пакета отсутствует.
При обычной контейнерной сборке YAML копируется в образ (`build/Dockerfile`): после
изменения нужно пересобрать и развернуть Review. Можно использовать `CONFIG_PATH`
для внешнего файла, но его изменение тоже требует рестарта процесса.

Команды, параметры и порядок сравнения «без / с защитой», а также двух пар alpha/beta
собраны в [шпаргалке урока](../../demo/module-6/lesson-06-load-shedding-demo-cheatsheet.md).
Для каждого нового процесса Review сначала включить неизменную базовую задержку
**100ms** и выполнить прогрев из шпаргалки. Затем при той же базе добавлять задержку
от inflight. Прогрев без задержки запоминает слишком маленький baseline RTT и меняет
условия сравнения. Для восстановления лимита возвращать uniform 100ms; полный сброс
инъекции выполнять после завершения опыта.

Сохранённые настройки пакета backlog=1000 / 2s отличаются от демонстрационных
100 / 200ms; точные блоки YAML приведены в шпаргалке. Отказ не обязан быть мгновенным
при достижении 40 активных RPC: лимит адаптируется, а запросы могут ждать допуска.
Handler-инъекция входит в RTT Vegas и моделирует рост времени обслуживания при
конкуренции; сама по себе она не доказывает наличие очереди в PostgreSQL.

Проверки: `GOWORK=off go test -race ./...`, `GOWORK=off go vet ./...`.
Новые тесты не используют PostgreSQL/k3s; они проверяют реальный limiter и цепочку
interceptor'ов с управляемым обработчиком, очередь/отказ/освобождение слотов,
обход Echo и реакции Vegas на управляемые RTT. Это не нагрузочный E2E-прогон.
