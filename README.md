# HotelFlow

Учебный гостиничный стенд: один k3s-узел, API gateway и шесть сервисов с gRPC Echo. Хранилищ пока нет.

## Запуск

Нужны Go 1.26.1+ и запущенный Docker с Linux-контейнерами.
Из корня проекта:

```bash
go run ./hotelctl/cmd/hotelctl up
```

Первый запуск требует интернета. CLI запускает k3s, собирает сервисы и проверяет API.
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
make -C services/api-gateway proto-deps  # обновить зависимости Buf
make generate                           # сгенерировать контракты
```

## Доступ с компьютера

| Сервис | gRPC на компьютере | Admin |
|---|---|---|
| api-gateway | `127.0.0.1:9090` | `8081` |
| availability | `127.0.0.1:9091` | `8082` |
| booking | `127.0.0.1:9092` | `8083` |
| check-in | `127.0.0.1:9093` | `8084` |
| room | `127.0.0.1:9094` | `8085` |
| notification | `127.0.0.1:9095` | `8086` |
| search | `127.0.0.1:9096` | `8087` |

HTTP gateway: `http://localhost:8080/v1/echo?message=Hello`, Swagger: `/docs/`.
Healthcheck: `http://localhost:<admin>/healthcheck/{live,ready,startup}`.

Порты доступны без `port-forward`; admin остаётся доступным при неготовности Pod.

[Сервисы и gRPC](docs/services.md) · [Все команды CLI](hotelctl/README.md) · [Архитектура](docs/cli-architecture.md) · [Проверки](docs/verification.md)
