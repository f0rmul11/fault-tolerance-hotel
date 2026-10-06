# hotelctl

Go CLI для управления стендом. Конфигурация: `../hotel.yaml`.
Нужны Go 1.26.1+ и запущенный Docker с Linux-контейнерами.

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
| `status [-o json]` | Состояние сервисов и их локальные порты |
| `logs SERVICE [-f] [--tail 100]` | Логи новейшего Pod; `-f` — следить до Ctrl+C |
| `doctor` | Проверить конфигурацию, Docker, порты и k3s |
| `smoke` | Проверить gRPC Echo всех сервисов, admin и HTTP gateway |
| `stop` | Остановить стенд, сохранив данные |
| `destroy` | Удалить стенд **вместе с данными** |
| `kubectl ARGS...` | Выполнить kubectl внутри k3s |
| `completion SHELL` | Автодополнение: bash, zsh, fish, powershell |
| `COMMAND --help` | Справка по команде |

Общие флаги: `--project DIR`, `--config FILE`; версия — `--version`.
Таймаут: `up --timeout 15m`, остальные команды — `--timeout 5m` (кроме `kubectl`).
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
