# Каталог runtime-конфигурации

`rtconfig.yaml` описывает сервисные значения в форме scratch: `realtimeConfig`, `usage`, `group`, `value`, `type`, обязательный `writable` и необязательный `view.enum`. Это каталог и defaults; текущие remote overrides хранятся отдельно в etcd.

После изменения каталога из корня модуля выполните:

```sh
go generate ./internal/config
```

Генератор объединяет service YAML с `system.Definitions()` из `internal/pkg/rtconfig/system`, проверяет типы и коллизии и создаёт `internal/config/config.go`. Generated файл нельзя редактировать вручную. Bootstrap собирает тот же общий каталог и сверяет его `Hash()` с generated `SchemaHash`; ручная замена YAML без регенерации не является механизмом runtime update.

Чтение сохраняет scratch API:

```go
import config "hotels/internal/config"

enabled := config.GetValue(ctx, config.RoomsListEnabled).Bool()
```

При запуске Hotels global клиент устанавливается явно через `global.SetClient` из `hotels/internal/pkg/rtconfig/global` до serving. До установки чтение возвращает error Value с `ErrNotInitialized`, без скрытого подключения к сети. Для DI и параллельных тестов:

```go
cfg := config.NewClient(runtimeClient)
enabled := cfg.GetValue(ctx, config.RoomsListEnabled).Bool()
```

`internal/pkg/rtconfig/system` предназначен для платформенных определений и коротких callbacks. Каталог пока пуст. Его private `register` сохраняет пять аргументов scratch и добавляет options; `WithWritable(true)` явно разрешает remote override, `WithEnum(...)` задаёт допустимые string values. При инициализации `global.Init` вызывает `Freeze()`. `Definitions()` и `Callbacks()` возвращают копии; сетевые подписки создаёт общий manager, а не registry. Сервисный ключ не может переопределить одноимённый системный.

Manager автоматически запускается в lifecycle Hotels. Флаг `rooms_list_enabled` имеет default false. Use case `ListRooms` получает конкретный `*config.Client` через конструктор и проверяет флаг перед чтением БД. При false возвращается FailedPrecondition (HTTP 409); Review не вызывается. Чтобы разрешить получение номеров: `./bin/hotelctl config set hotels rooms_list_enabled true` из корня проекта.

## API генератора

Генератор находится в `internal/pkg/rtconfig/codegen`. `internal/config/generate/main.go` содержит только настройку путей и запуск; `go generate ./internal/config` вызывает его из директории `internal/config`.

```go
import (
    "hotels/internal/pkg/rtconfig/codegen"
    "hotels/internal/pkg/rtconfig/system"
)

func generateConfig() error {
    generator, err := codegen.New(codegen.Options{
        SchemaPath:        "../../config/rtconfig.yaml",
        OutputPath:        "config.go",
        SystemDefinitions: system.Definitions(),
    })
    if err != nil {
        return err
    }

    return generator.Generate()
}
```

Оба пути обязательны; относительные пути фиксируются от рабочей директории в момент `New`. В примере выше это `internal/config`; при запуске из корня модуля используйте `config/rtconfig.yaml` и `internal/config/config.go`. `SystemDefinitions` необязателен: без него генерируется только сервисный каталог. Генератор выпускает пакет `config` с импортами текущего модуля `hotels`.

`New` проверяет параметры и копирует системные определения без чтения или создания файлов. `Generate` заново читает YAML, проверяет общий каталог и Go-имена, форматирует код и атомарно заменяет выходной файл. При ошибке чтения, валидации или форматирования прежний результат сохраняется. Повторный вызов с теми же входными данными даёт идентичный файл.

## Запуск клиента

Hotels автоматически создаёт manager из YAML и системного registry, проверяет `SchemaHash`, устанавливает `global.SetClient` и запускает получение snapshot + watch до serving. Системные callbacks регистрируются и получают начальное значение до готовности приложения. При недоступном etcd сервис стартует с defaults после ограниченного ожидания, затем восстанавливает соединение в фоне. После успешного чтения при потере etcd остаётся последнее валидное состояние в памяти; после перезапуска процесса без etcd используются YAML defaults.

Статические настройки находятся в `config/config.yaml`, секция `runtime_config`. Общий тип определён в библиотеке:

```go
type Config struct {
    RuntimeConfig rtconfig.Config `yaml:"runtime_config"`
    // Остальные настройки сервиса.
}
```

После чтения static YAML вызываются `cfg.RuntimeConfig.ResolveSchemaPath(configPath)` и `Validate()`. Конструктор приложения вызывает `global.Init(ctx, cfg.RuntimeConfig, generated.SchemaHash)`: он возвращает `*rtconfig.Manager` и сам регистрирует закрытие в общем `closer`. Приложение вызывает closer после drain или при ошибке дальнейшей инициализации. Readiness использует `client.CheckInitialSync()`: до первого успешно применённого snapshot из etcd Pod не готов. После синхронизации потеря etcd или невалидное обновление не отменяют готовность последнего валидного snapshot. Значение false не делает Pod неготовым. `CheckReady()` отдельно сохраняет прежнюю проверку lifecycle клиента. В `main` достаточно `internal.New(ctx).Run(ctx)`; отдельные Start/Close не нужны.

Настройки:

| Переменная окружения | Назначение | По умолчанию |
|---|---|---|
| `RTCONFIG_ENDPOINTS` | Адреса etcd через запятую | `http://127.0.0.1:2379` |
| `RTCONFIG_PREFIX` | Обязательный префикс ключей сервиса | В YAML Hotels: `/hotelflow/dev/services/hotels/rtconfig/v1/values/`; в библиотеке default отсутствует |
| `RTCONFIG_SCHEMA_PATH` | Путь к каталогу, относительно директории static YAML | `rtconfig.yaml` |
| `RTCONFIG_STARTUP_TIMEOUT` | Budget первого получения snapshot; также отдельный budget initial callbacks | `5s` |
| `RTCONFIG_SHUTDOWN_TIMEOUT` | Ожидание завершения manager после drain | `3s` |

В k3s Deployment явно задаёт endpoint `http://config-etcd.hotelflow.svc.cluster.local:2379` и префикс `dev`. Для локального `go run ./cmd` сначала откройте `make config-etcd` из корня стенда. Клиент подключается к доверенному HTTP etcd стенда; TLS/credentials при инициализации приложения пока не настраиваются.

Готовность включена в стандартный admin readiness:

```bash
curl -s http://127.0.0.1:8081/healthcheck/ready
```

Проверка `runtime_config` возвращает `success` после успешного запуска reader. До запуска и после закрытия клиента readiness возвращает 503. Доступность etcd и свежесть snapshot не участвуют в этой проверке: валидные defaults или последнее состояние в памяти достаточны для обслуживания запросов. Readiness не подтверждает применение конкретной revision. Отдельного `/runtime-config/status` нет; программная диагностика остаётся в `client.Status()`.

При ошибке YAML, несовпадении schema hash или некорректных параметрах подключения публичный сервер не запускается. Сначала исправьте каталог и выполните `go generate ./internal/config`, затем пересоберите образ.
