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
