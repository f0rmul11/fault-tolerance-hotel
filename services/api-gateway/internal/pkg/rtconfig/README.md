# Runtime configuration

`rtconfig` хранит проверенную конфигурацию в immutable snapshot. `Value` читает atomic pointer и map без mutex, парсинга, логов и сетевых вызовов. Один manager владеет etcd client, одной watch-подпиской и последовательным dispatcher callbacks.

```go
import config "api-gateway/internal/config"

enabled := config.GetValue(ctx, config.HotelsListEnabled).Bool()

// Независимый клиент для DI и параллельных тестов.
cfg := config.NewClient(manager)
enabled = cfg.GetValue(ctx, config.HotelsListEnabled).Bool()
```

Клиент подключён к lifecycle API Gateway и устанавливается до начала serving. Writer доступен через `hotelctl config get/set` — см. [инструкцию CLI](../../../../../hotelctl/README.md#runtime-конфигурация-в-etcd). Отдельный `config-etcd` с PVC разворачивается вместе со стендом; endpoint внутри k3s — `http://config-etcd.hotelflow.svc.cluster.local:2379`. Локальная готовность reader включена в admin `/healthcheck/ready` как `runtime_config`. HTTP/gRPC gate пока не подключён. Флаг `hotels_list_enabled` пока служит примером каталога и сам по себе не меняет обработку запросов.

## Структура

| Файл | Ответственность |
|---|---|
| `client.go` | Контракты, options и локальный status |
| `config.go` | Общий тип статических настроек `rtconfig.Config` |
| `value.go` | Неизменяемые типизированные скаляры |
| `schema.go` | Общая валидация YAML, defaults и remote values |
| `manager.go` | Lifecycle и атомарная публикация snapshot |
| `watch.go` | Get → Watch, revisions, freshness и recovery |
| `observer.go` | Регистрация и последовательная доставка callbacks |
| `global/` | Явная инициализация общего клиента, системные callbacks и регистрация закрытия |
| `system/` | Registry платформенных определений и callbacks |
| `codegen/` | Генератор facade: проверка каталога, Go identifiers и атомарная запись |

`global`, `system` и `codegen` зависят от core `rtconfig`; `global.Init` собирает core и system и регистрирует закрытие в общем `closer`. Core не импортирует эти подпакеты. `codegen` объединяет сервисный YAML с явно переданными системными определениями и создаёт facade `internal/config/config.go`, который использует `global`. Короткая точка запуска находится в `internal/config/generate/main.go`; в `cmd` остаётся только приложение. В корневом `config` находятся статическая конфигурация и YAML-каталог сервиса.

## Каталог и wire format

Канонический сервисный каталог — `config/rtconfig.yaml`:

```yaml
realtimeConfig:
  hotels_list_enabled:
    usage: Разрешает получение списка отелей через API Gateway
    group: features
    value: "false"
    type: bool
    writable: true
```

Поддерживаются `bool`, `string`, `int`, `int64`, `uint`, `uint64`, `float64`, `duration`. `writable` в YAML обязателен. `view.enum` задаётся только для string. Неизвестные поля, дубликаты, YAML aliases, неверный default или enum отвергаются. `NewSchema` и `ParseSchema` возвращают готовый immutable каталог; zero-value `Schema` не используется.

В etcd хранится **JSON-скаляр** по ключу `<Prefix><name>`: `true`, `42`, `"text"`, `"250ms"`. Строка `"false"` допустима как YAML default для bool, но remote bool должен быть JSON `false`. Целые разбираются без преобразования через float64; NaN/Inf и overflow отвергаются.

Отсутствующий ключ означает default. Delete возвращает default. Remote override для `writable: false` отвергается даже при совпадении с default. Неизвестные ключи не входят в effective snapshot, но учитываются в ограничениях размера. Невалидный кандидат отклоняется целиком: последнее валидное состояние остаётся доступным. Сырые изменения сохраняются до исправления, поэтому исправление одного ключа не теряет остальные значения той же транзакции.

После изменения каталога из корня Go-модуля:

```sh
go generate ./internal/config
```

Точка запуска передаёт в `codegen.New(codegen.Options{...})` пути `SchemaPath`, `OutputPath` и `SystemDefinitions: system.Definitions()`, затем вызывает `Generate()`. [Пример использования API](../../../config/rtconfig.md#api-генератора).

Генератор объединяет YAML и `system.Definitions()`, сортирует ключи, проверяет коллизии Go identifiers и выпускает `SchemaHash`. Hash включает имя, тип, default, writable и enum; описательный текст в него не входит. Generated-файл вручную не редактируется.

## Подключение к lifecycle

Общие настройки встраиваются в статический конфиг сервиса как `RuntimeConfig rtconfig.Config`. Prefix выбирает сам сервис в YAML или `RTCONFIG_PREFIX`, в библиотеке нет имени сервиса или окружения. `ResolveSchemaPath(configPath)` разрешает путь к каталогу рядом со статическим YAML; `Validate()` проверяет настройки.

В API Gateway всё подключено внутри `internal.New(ctx)`: сначала запускается rtconfig, затем создаются основной сервер и обработчики. В `main` достаточно обычного запуска приложения:

```go
internal.New(ctx).Run(ctx)
```

Явный `global.Init(ctx, cfg.RuntimeConfig, generated.SchemaHash)` загружает каталог, проверяет hash, запускает manager и системные callbacks, устанавливает общий клиент. При ошибке созданный manager закрывается, прежний общий клиент восстанавливается. Успешный запуск регистрирует закрытие в общем `closer`, который приложение вызывает после drain и при ошибке дальнейшей инициализации. Для закрытия используется отдельный `ShutdownTimeout`.

Внутри `initRuntimeConfig` остаётся только:

```go
client, err := global.Init(ctx, config.Instance().RuntimeConfig, runtimeconfig.SchemaHash)
if err != nil {
    return err
}
a.runtimeConfig.Store(client)
return nil
```

Возвращается обычный `*rtconfig.Manager`, без дополнительной обёртки. Его `CheckReady()` возвращает ошибку до запуска и после закрытия; валидные defaults или последний snapshot достаточны для готовности. В Gateway указатель публикуется после успешного `Init`, проверка входит в `/healthcheck/ready`. Отдельного HTTP status endpoint нет. Readiness не проверяет свежесть или соединение с etcd.

`global.Init` вызывается один раз при старте приложения. `global.Client()` только возвращает установленный клиент; чтение не запускает сеть. Для независимых клиентов используйте `rtconfig.New`/`Manager.Start`/`Manager.Close` и самостоятельно управляйте их lifecycle.

`config/rtconfig.yaml` включён в Docker-образ. [Параметры запуска](../../../config/rtconfig.md#запуск-клиента).

Минимальные options для локального стенда:

```go
opts := rtconfig.Options{
    Endpoints: []string{"http://127.0.0.1:2379"},
    Prefix:    "/hotelflow/dev/services/api-gateway/rtconfig/v1/values/",
    ProjectID: "api-gateway/dev",
}
```

Пакет поддерживает статические `TLS`, `Username`, `Password`; endpoint не содержит credentials. Для защищённого etcd reader следует выдать только read-доступ к своему prefix, writer — отдельную identity. Текущая инициализация Gateway подключается к доверенному HTTP etcd стенда без авторизации. При включённой авторизации недоступный etcd во время создания client также оставляет defaults и повторяется в фоне.

| Budget | Default |
|---|---|
| `StartupTimeout` — полное начальное ожидание | 5 s |
| `RequestTimeout` — открытие/auth и отдельный Get | 3 s |
| `WatchTimeout` — регистрация подписки | 5 s |
| `ProbeInterval` / `ProbeTimeout` | 30 s / 5 s |
| `StaleAfter` | 90 s |
| `RetryMin` / `RetryMax` с jitter | 250 ms / 30 s |

Нулевые durations выбирают defaults, отрицательные отвергаются. Истечение startup budget не отменяет manager. `Start` одноразовый; `Close` идемпотентный. После Close опубликованные values доступны для чтения, новых обновлений нет. До `SetClient` facade возвращает error Value с `ErrNotInitialized`, без скрытой инициализации сети.

## Чтение и observers

`Value(ctx, name)` возвращает immutable значение или `ErrVariableNotFound`. Отмена request context не мешает локальному чтению. `Values(ctx)` копирует map **одного** поколения; два независимых вызова `Value` могут увидеть разные поколения. Сохранённые Value не меняются после следующей публикации.

`String()` возвращает каноническое представление всех типов. Bool/Float64/Duration getters требуют соответствующего типа; signed и unsigned getters допускают только свой класс и проверяют диапазон результата. Getter неподходящего типа возвращает zero и не меняет `Err()`: проверяйте контракт consumer через `RequireType` при инициализации. `NewNilValue(err)` сохраняет ошибку и предоставляет безопасные zero getters.

`WatchVariable` и `WatchAllVariables` регистрируются после Start. Context ограничивает регистрацию; после успешного возврата подписка живёт до остановки manager. Первый callback получает old с именем ключа и `ErrNoPreviousValue`, без nil. WatchAll доставляет ключи в стабильном порядке.

Публикация и регистрация сериализованы; callbacks выполняются вне lock. Initial snapshot сохраняется, последующие обновления coalesce до последнего pending snapshot. Old — последнее доставленное этому observer состояние; no-op не уведомляет. Это доставка состояния, а не журнал каждого изменения. Callback обязан быть коротким и идемпотентным; он не должен синхронно вызывать `Close` или ждать `WaitForInitial` из dispatcher.

`WaitForInitial` ждёт первые уведомления подписок, существовавших в момент вызова. Это барьер initial generation, без гарантии совпадения внешних эффектов с самым новым snapshot. Panic изолируется и попадает в status; зависший callback блокирует dispatcher, но не публикацию и чтения. Close отменяет сетевую работу и ждёт до своего budget, возвращая ошибку при зависшем callback. После отмены оставшиеся callbacks текущего WatchAll batch не запускаются.

## Протокол и диагностика

Linearizable prefix Get даёт полный raw snapshot и revision R. Watch начинается с R+1; изменения одной revision применяются вместе. Cursor вычисляется по `ModRevision` событий; watch headers не пропускают исторические события. Invalid candidate продвигает observed revision, но не applied revision.

После compaction, закрытия watch-канала, ошибки watch-цикла, смены cluster ID, rollback revision или превышения лимитов начинается новый полный Get. Обычный сетевой разрыв официальный etcd client может восстановить прозрачно, продолжив Watch с сохранённой revision. Старые raw keys не переносятся в новую epoch. Периодический quorum read плюс watch progress проверяет, что watch достиг подтверждённой revision; обычная тишина в каталоге не считается неисправностью. Reconnect использует bounded exponential backoff с jitter.

`Ready` и `Version.IsReady()` означают доступность **локального** snapshot, включая defaults. `Status` различает source, transport connectivity, freshness, invalid candidate, observed/applied revisions и поколения observers. `Stale` диагностический: недоступность etcd не стирает последнее валидное значение. В status не включаются значения, credentials или произвольные тексты удалённых ошибок.

Лимиты: 256 keys, 128 bytes в имени, 1 KiB на remote JSON value, 256 KiB на raw keys + values, 64 observers, 1 MiB на получаемое etcd сообщение. Кандидат сверх лимита отклоняется целиком. Каталог рассчитан на контролируемую частоту административных изменений; внутренние очереди официального etcd SDK не дают гарантии жёсткого memory bound при бесконечном потоке записей.

## Проверки

Из корня Go-модуля:

```sh
go generate ./internal/config
go test -race ./...
go vet ./...
go build ./...
go test ./internal/pkg/rtconfig -run '^$' -bench BenchmarkManagerValueParallel -benchmem
ETCD_BIN=/absolute/path/to/etcd go test -race -tags=integration ./internal/pkg/rtconfig -count=1
```

Integration suite запускает свой standalone etcd на временных loopback ports и останавливает его при cleanup. Проверены настоящие watch/update/delete, atomic transactions, invalid snapshot/correction, restart, offline delete + compaction + resync, cold offline recovery и authentication/RBAC. Проверки выполнены с etcd 3.6.15; сценарии не доказывают работу production HA-кластера или TLS внутри deployment image.
