# HotelFlow

Учебный гостиничный стенд: один k3s-узел, API Gateway и пять сервисов. Booking атомарно сохраняет подтверждённую бронь конкретного номера, период бронирования, ключ идемпотентности и outbox. Hotels хранит каталог и ограничения длительности (seed: минимум 12 часов). Пересечения исключаются ограничением PostgreSQL EXCLUDE USING gist. Во всех сервисах сохранены gRPC Echo и probes.

[Действующая спецификация](docs/specification.md)

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
make generate                           # Buf: lint, build и генерация всех сервисов
make -C services/booking generate       # собственный API и внешний клиент Hotels
```

Контракты находятся в `api` каждого сервиса; общий protobuf отсутствует. `buf.gen.local.yaml` генерирует свой API, `buf.gen.external.yaml` — клиенты из `api` соседнего сервиса. Штатная генерация требует только Docker: версии Buf и плагинов закреплены в codegen-образах. `generate-local` выполняет ту же цепочку при уже установленных Buf и плагинах.

## Доступ с компьютера

| Сервис | gRPC на компьютере | Admin |
|---|---|---|
| api-gateway | `127.0.0.1:9090` | `8081` |
| booking | `127.0.0.1:9091` | `8082` |
| check-in | `127.0.0.1:9092` | `8083` |
| review | `127.0.0.1:9093` | `8084` |
| notification | `127.0.0.1:9094` | `8085` |
| hotels | `127.0.0.1:9095` | `8086` |

HTTP gateway: `http://localhost:8080/v1/echo?message=Hello`, Swagger: `/docs/`.
Healthcheck: `http://localhost:<admin>/healthcheck/{live,ready,startup}`.

Порты доступны без `port-forward`; admin остаётся доступным при неготовности Pod.

[Сервисы и gRPC](docs/services.md) · [Все команды CLI](hotelctl/README.md) · [Архитектура](docs/cli-architecture.md) · [Проверки](docs/verification.md)

## Бронирование

Атомарное бронирование, идемпотентность, outbox, команды и примеры запросов: [docs/booking.md](docs/booking.md).

Заселение/отмена и Kafka request/reply: [Check-in](docs/check-in.md). Проверка реализации: [отчёт](docs/check-in-verification.md).

Каталог отелей и номеров, пользовательские рейтинги отелей: [Hotels / Review](docs/hotels-review.md).

## Runtime-конфигурация

В k3s разворачивается отдельный `config-etcd` с постоянным PVC. После `hotelctl up`
локальный адрес `http://127.0.0.1:2379` доступен без туннеля; внутри кластера — `http://config-etcd:2379`.
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

`stop` без имён по-прежнему останавливает весь k3s. `status` показывает все 13 компонентов.
PostgreSQL опубликованы постоянно на loopback: `booking-postgres:15432`,
`check-in-postgres:15433`, `hotels-postgres:15434`, `review-postgres:15435`.
После установки hosts имена разрешаются в `127.0.0.1`; до установки можно использовать IP.
`hotelctl up` обновляет публикацию портов существующего контейнера, сохраняя том k3s.

## Kafka UI

AKHQ: [http://localhost:8090](http://localhost:8090), доступен сразу после `hotelctl up`.
Отдельный `port-forward` не нужен. Выберите `hotelflow` → топик → Data → Oldest.
Видны payload, key, headers, partition и offset. Kafka внутри кластера: `kafka:9092`;
наружу опубликован UI, а не брокер с внутренним advertised address.

[Шпаргалка открытого урока: команды, БД и сценарии outbox/inbox](docs/demo-control.md).

Полный сценарий бронирования, заселения, отмены и уведомлений: `make smoke-business`.
[Что проверяет команда и какие данные оставляет](hotelctl/README.md#проверка-бизнес-сценария).

Согласованный сброс учебных БД, Kafka и восстановление seed: `make reset`.
Подробности и продолжение после прерывания — [hotelctl](hotelctl/README.md#сброс-учебных-данных).
