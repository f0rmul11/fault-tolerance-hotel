# hotelctl

Go CLI для управления стендом. Конфигурация: `../hotel.yaml`.
Для сборки нужны Go 1.26.1+. Команды стенда требуют запущенный Docker с Linux-контейнерами; `config get/set` подключаются напрямую к etcd и работают без Docker.

Из корня проекта:

```bash
go run ./hotelctl/cmd/hotelctl up  # запуск без отдельной сборки CLI
make build                       # собрать ./bin/hotelctl
```

Ниже `hotelctl` означает `./bin/hotelctl`.

| Команда | Назначение |
|---|---|
| `up [SERVICE...]` | Запустить k3s, собрать, развернуть и проверить стенд |
| `build [SERVICE...]` | Собрать образы; без имён — все сервисы |
| `deploy [SERVICE...]` | Развернуть ранее собранные образы |
| `config get SERVICE VARIABLE` | Прочитать override из etcd и его revision |
| `config set SERVICE VARIABLE VALUE` | Изменить runtime-переменную сервиса в etcd |
| `status [-o json]` | Состояние сервисов и их локальные порты |
| `logs SERVICE [-f] [--tail 100]` | Логи новейшего Pod; `-f` — следить до Ctrl+C |
| `doctor` | Проверить конфигурацию, Docker, порты и k3s |
| `smoke` | Проверить gRPC Echo всех сервисов, admin и HTTP gateway |
| `smoke --business` | Дополнительно создать, активировать и отменить тестовую бронь; проверить Kafka-обмен и stdout Notification |
| `reset --yes` | Согласованно очистить учебные БД/Kafka и восстановить seed |
| `stop` | Остановить стенд, сохранив данные |
| `destroy` | Удалить стенд **вместе с данными** |
| `kubectl ARGS...` | Выполнить kubectl внутри k3s |
| `completion SHELL` | Автодополнение: bash, zsh, fish, powershell |
| `COMMAND --help` | Справка по команде |

Общие флаги: `--project DIR`, `--config FILE`; версия — `--version`.
Таймаут: `up --timeout 15m`, `config get/set --timeout 5s`, остальные команды — `--timeout 5m` (кроме `kubectl`).
Перед `kubectl` указываются `--project` и `--config`; `exec -t` не поддерживается.
`logs -f` работает до Ctrl+C независимо от `--timeout`.

```bash
./bin/hotelctl build api-gateway
./bin/hotelctl deploy api-gateway
./bin/hotelctl logs api-gateway -f
./bin/hotelctl kubectl -n hotelflow get pods
```

`hotel.yaml` содержит версию k3s и список сервисов; стандартные пути CLI определяет сам.
Порты назначаются по порядку списка; фактические адреса показывает `status`.

[Архитектура и расширение CLI](../docs/cli-architecture.md).

## Проверка бизнес-сценария

```bash
./bin/hotelctl smoke --business --timeout 3m
# Или без сборки CLI:
make smoke-business
```

Команда проверяет справочник Hotels/Review и выбирает свободный номер на текущий период
с учётом минимальной длительности. Через публичный HTTP API она создаёт CONFIRMED,
повторяет запрос с тем же ключом и проверяет отказ при пересечении. Затем ждёт EXPECTED
в Check-in, активирует бронь до ACTIVE/CHECKED_IN и отменяет до CANCELED в обоих сервисах.
В конце проверяет доступность номера и три push-лога Notification с UUID correlation_id.
Так проверяются доставка через outbox/inbox и оба направления request/reply.

Каждый запуск использует новые actor_id и ключи идемпотентности. При успехе номер освобождён,
но отменённая тестовая бронь, события и уведомления остаются для просмотра в AKHQ.
Команда не меняет каталог и не очищает чужие данные. Если свободных номеров нет, она завершится
с понятной ошибкой. Запускайте её последовательно на учебном стенде без параллельного изменения
выбранного номера.

В случае ошибки после создания выполняется попытка отмены с отдельным лимитом 30 секунд.
Если сервисы недоступны или операция ещё выполняется, отмена может не пройти: команда сообщает
booking_id и ошибку. actor_id и ключ создания выводятся до запроса, чтобы повторить его при
потере ответа. Ненулевой код завершения означает неуспешную проверку.

`smoke` без флага и проверки внутри `up` остаются проверками доступности и не создают броней.
`--timeout` ограничивает проверки, но аварийная попытка отмены может добавить до 30 секунд.
Проверка не отключает Kafka и не является нагрузочным тестом.

## Сброс учебных данных

```bash
./bin/hotelctl reset --yes --timeout 5m
# Или из корня без сборки CLI:
make reset
```

Команда предназначена для работающего локального стенда с текущей схемой и конфигурацией.
Она удаляет все учебные записи четырёх БД, включая HTTP-ключи, inbox/outbox;
возвращает 3 отеля, 11 номеров и 2 рейтинга; пересоздаёт booking.events.v1/checkin.events.v1
(1 partition, replication factor 1) и удаляет consumer groups из конфигураций Booking,
Check-in и Notification вместе с offsets. Остальные топики/группы не затрагиваются.
История Goose, схемы таблиц, PostgreSQL, Kafka, образы и том k3s сохраняются.
Автоматической резервной копии каждого reset нет; `--yes` явно подтверждает удаление учебных данных.

Перед очисткой останавливаются все шесть приложений с ожиданием удаления Pod.
Reset Job выполняет очистку и seed каждой БД в одной локальной транзакции, затем Kafka.
После успеха возвращается исходное число реплик; Gateway запускается последним.
Исходно выключенный сервис остаётся выключенным. Запускается обычный smoke для включённых сервисов,
а не business smoke: в чистом стенде не должны появляться новые тестовые брони.

Во время reset не отправляйте внешние сообщения, не меняйте конфигурацию, реплики и данные вручную.
Состояние операции сохраняется в `.local/hotelctl/state.json`. При ошибке или Ctrl+C
повторите `reset --yes`: активный Job дождётся завершения, неуспешный — пересоздан.
Приложения остаются остановленными, пока очистка не закончилась. `up` и `deploy` в этот момент
возвращают указание завершить reset. Если очистка уже прошла, повтор продолжит запуск приложений
без повторной очистки. Job имеет лимит 5 минут; диагностировать его можно через `hotelctl kubectl`.

Reset не применяет изменённые inplace-миграции к уже существующей старой схеме.
Для полного пересоздания такого учебного стенда используются отдельные `destroy` и `up`.
После reset можно запустить `make smoke-business`; эта проверка снова оставит отменённую бронь и события.

## Runtime-конфигурация в etcd

```bash
./bin/hotelctl config get api-gateway hotels_list_enabled
./bin/hotelctl config set api-gateway hotels_list_enabled true
./bin/hotelctl config set api-gateway hotels_list_enabled false
```

CLI выбирает сервис из `hotel.yaml`, читает `<service.context>/config/rtconfig.yaml`
и проверяет имя, тип, `writable` и string enum выбранной переменной до подключения к etcd.
Сейчас сервисный каталог есть у API Gateway; для другого сервиса нужно сначала добавить его каталог.
Ключи из Go registry `rtconfig/system` этой командой не редактируются.
CLI не импортирует внутренние пакеты сервисов: границей служат YAML-каталог и JSON-скаляры в etcd.
При чтении сервис независимо проверяет весь snapshot, поэтому CLI не заменяет reader validation.

Адрес по умолчанию — `http://127.0.0.1:2379`, окружение — `dev`.
Ключ строится как `/<stand.name>/<env>/services/<service>/rtconfig/v1/values/<variable>`;
стандартное имя стенда — `hotelflow`. `--prefix` явно заменяет весь префикс выбранного сервиса.

```bash
./bin/hotelctl config set api-gateway hotels_list_enabled true \
  --endpoints=http://127.0.0.1:12379 --env=dev --timeout=5s
```

Etcd должен быть доступен с компьютера, где запущен CLI. Команда не разворачивает etcd
и не создаёт port-forward. Deployment отдельного config etcd и bootstrap reader в Gateway
пока не подключены: успешный `set` подтверждает запись в хранилище, а не применение флага Pod.
`get` показывает значение из etcd; если ключ отсутствует — YAML default с `source=default revision=0`.
Он не опрашивает локальный snapshot сервиса и не показывает его last-known-good состояние.

Значение передаётся обычным CLI-аргументом; JSON-кодирование выполняется автоматически:

| Тип | Пример аргумента | Значение в etcd |
|---|---|---|
| `bool` | `false` | `false` |
| `string` | `'maintenance mode'` | `"maintenance mode"` |
| `duration` | `250ms` | `"250ms"` |
| `int` / `int64` | `42` | `42` |
| `uint` / `uint64` | `18446744073709551615` для uint64 | точное JSON-число |
| `float64` | `1.5` | `1.5` |

Для отрицательного числа используйте завершитель флагов: `config set SERVICE VARIABLE -- -1`.
Переполнение, NaN/Inf, неверный bool, enum, readonly и JSON value больше 1 KiB отвергаются.
Для string не нужно вручную добавлять JSON-кавычки: они стали бы частью самой строки.
Изменение значения не переписывает YAML и не требует генерации кода.

Каждый `set` читает текущий ModRevision и выполняет compare-and-swap.
При конкурентном изменении команда завершается конфликтом и не повторяет запись автоматически.
Для защиты ранее прочитанного значения передайте revision из `get` явно:

```bash
./bin/hotelctl config set api-gateway hotels_list_enabled false --expect-revision=42
# Создать override только если ключ ещё отсутствует:
./bin/hotelctl config set api-gateway hotels_list_enabled true --expect-revision=0
```

При потере ответа команда возвращает ошибку без сообщения об успехе. Сначала выполните `get`
и проверьте хранилище перед повтором. Повторный `get` показывает текущее состояние, но не доказывает,
какой именно writer его установил. Вызов `hotelctl reset` runtime overrides не очищает.

Для защищённого etcd используются отдельные operator credentials с read/write на service prefix:

```bash
# HOTELCTL_ETCD_PASSWORD задаётся в окружении вне аргументов команды.
./bin/hotelctl config set api-gateway hotels_list_enabled false \
  --endpoints=https://config-etcd.example:2379 \
  --user=config-writer --cacert=/path/to/ca.pem
```

`--password-env NAME` выбирает другую переменную окружения. Для mTLS добавьте
`--cert=/path/to/client.pem --key=/path/to/client-key.pem`. TLS-проверка сертификата остаётся включённой.
Пароль не принимается внутри URL endpoint или отдельным значением аргумента.

Unit-проверки входят в `go test -race ./...`. Отдельный suite запускает свой временный etcd
на случайных loopback ports и проверяет команды, JSON, CAS, изоляцию сервисов/окружений,
offline timeout и authentication/RBAC:

```bash
cd hotelctl
ETCD_BIN=/absolute/path/to/etcd go test -race -tags=integration ./pkg/cmd/config -count=1
```
