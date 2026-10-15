# Каталог runtime-конфигурации

`rtconfig.yaml` описывает сервисные значения в форме scratch: `realtimeConfig`, `usage`, `group`, `value`, `type`, обязательный `writable` и необязательный `view.enum`. Это каталог и defaults; текущие remote overrides хранятся отдельно в etcd.

После изменения каталога из корня модуля выполните:

```sh
go generate ./internal/config
```

Генератор объединяет service YAML с `system.Definitions()` из `internal/pkg/rtconfig/system`, проверяет типы и коллизии и создаёт `internal/config/config.go`. Generated файл нельзя редактировать вручную. Bootstrap должен собрать тот же общий каталог и сверить его `Hash()` с generated `SchemaHash`; ручная замена YAML без регенерации не является механизмом runtime update.

Чтение сохраняет scratch API:

```go
import config "api-gateway/internal/config"

enabled := config.GetValue(ctx, config.HotelsListEnabled).Bool()
```

Global клиент устанавливается явно через `global.SetClient` из `api-gateway/internal/pkg/rtconfig/global` до serving. До установки чтение возвращает error Value с `ErrNotInitialized`, без скрытого подключения к сети. Для DI и параллельных тестов:

```go
cfg := config.NewClient(runtimeClient)
enabled := cfg.GetValue(ctx, config.HotelsListEnabled).Bool()
```

`internal/pkg/rtconfig/system` предназначен для платформенных определений и коротких callbacks. Каталог пока пуст. Его private `register` сохраняет пять аргументов scratch и добавляет options; `WithWritable(true)` явно разрешает remote override, `WithEnum(...)` задаёт допустимые string values. После регистрации bootstrap вызывает `Freeze()`. `Definitions()` и `Callbacks()` возвращают копии; сетевые подписки создаёт общий manager, а не registry. Сервисный ключ не может переопределить одноимённый системный.

Этот каталог и facade сами по себе не активируют флаг в HTTP/gRPC обработчиках и не меняют существующий static `Config`. Пример `hotels_list_enabled` имеет default false; подключение manager и consumer к lifecycle приложения выполняется отдельно.

## API генератора

Генератор находится в `internal/pkg/rtconfig/codegen`. `internal/config/generate/main.go` содержит только настройку путей и запуск; `go generate ./internal/config` вызывает его из директории `internal/config`.

```go
import (
    "api-gateway/internal/pkg/rtconfig/codegen"
    "api-gateway/internal/pkg/rtconfig/system"
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

Оба пути обязательны; относительные пути фиксируются от рабочей директории в момент `New`. В примере выше это `internal/config`; при запуске из корня модуля используйте `config/rtconfig.yaml` и `internal/config/config.go`. `SystemDefinitions` необязателен: без него генерируется только сервисный каталог. Генератор выпускает пакет `config` с импортами текущего модуля `api-gateway`.

`New` проверяет параметры и копирует системные определения без чтения или создания файлов. `Generate` заново читает YAML, проверяет общий каталог и Go-имена, форматирует код и атомарно заменяет выходной файл. При ошибке чтения, валидации или форматирования прежний результат сохраняется. Повторный вызов с теми же входными данными даёт идентичный файл.
