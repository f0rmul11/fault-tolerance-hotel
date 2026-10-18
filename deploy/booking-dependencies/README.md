# Зависимости стенда

`hotelctl up` и `hotelctl deploy` для Booking, Check-in, Hotels, Review, Notification или gateway применяют этот Kustomize-набор, ждут PostgreSQL/Kafka/config-etcd, затем выполняют миграции и seed до rollout сервисов. При первом запуске контейнерные образы скачиваются из registry.

- PostgreSQL `16.10-alpine`: четыре отдельных Deployment/Service — `booking-postgres`, `check-in-postgres`, `hotels-postgres`, `review-postgres`. Каждый использует собственный PVC `<сервис>-db-data`, Secret `<сервис>-database` и единственную прикладную БД. `POSTGRES_USER`/`POSTGRES_DB` создают владельца при первом запуске. Миграции, seed и приложение получают только его DSN. Все таблицы — в `public`, без создания схем сервиса.
- Notification и API Gateway не получают PostgreSQL DSN. Notification использует общий брокер напрямую без inbox.
- Apache Kafka `3.9.1`: один узел KRaft, одна реплика; адрес внутри namespace — `kafka:9092`. Job явно создаёт `booking.events.v1` и `checkin.events.v1` через `--if-not-exists` до запуска outbox relay.
- Goose `3.26.0`: закреплённый multiarch wrapper `ghcr.io/kukymbr/goose-docker`, запускающий `/bin/goose`. Оригинальные SQL из `services/*/tools/migrations` монтируются в Job через ConfigMap; копий DDL здесь нет.
- После миграций выполняются seeds из `services/hotels/tools/seed/20261007.sql` и `services/review/tools/seed/20261007.sql`. Каталог содержит три отеля и 11 номеров, рейтинги двух отелей.

Имя Job зависит от SQL и версии Goose. Повторный `up` использует завершённый Job; новые SQL создают новый Job и применяются через стандартную таблицу версий Goose. Текущая смена модели выполнена inplace по решению пользователя: production-данных нет, старый учебный стенд пересоздаётся. `goose up` поверх БД старой модели не выполняет её преобразование; обычный повторный запуск актуальной схемы сохраняет данные.

При ошибке посмотрите `hotelctl kubectl -n hotelflow logs job/ИМЯ -c booking` (либо `hotels`, `review`, `check-in`, `hotels-seed`, `review-seed`). После исправления причины можно явно удалить только неуспешный Job и повторить `up`; CLI не удаляет данные и не повторяет неуспешную миграцию автоматически.

Данные PostgreSQL/Kafka находятся в PVC внутри постоянного тома k3s. `stop` сохраняет их, `destroy` удаляет весь стенд. Учётные данные синтетические; PostgreSQL имеют NodePort и локальные порты 15432–15435 через контейнер k3s. Kafka напрямую наружу не публикуется.

Прежний общий `postgres` и `postgres-init` исключены из манифестов. Новые PVC `<сервис>-db-data` не используют старый общий `booking-postgres-data`. Изменение исходников не удаляет существующие Kubernetes-объекты/тома: обычный apply не выполняет prune. Старый учебный стенд можно пересоздать явно; автоматического удаления данных или переноса старых схем нет.

## Просмотр Kafka через AKHQ

`akhq.yaml` входит в общий Kustomize-набор. Deployment использует `tchiotludo/akhq:dev`, конфигурация `AKHQ_CONFIGURATION` задаёт соединение `hotelflow` → `kafka:9092`. Данные брокера остаются в существующем Kafka; AKHQ не требует собственной БД или PVC.

После `hotelctl up` UI постоянно доступен на http://localhost:8090; туннель не нужен.
Service AKHQ использует NodePort 30089, Docker публикует его на loopback:8090.
`make kafka-ui-deploy` отдельно применяет манифест, `make kafka-ui` дополнительно печатает URL.
Для старого контейнера сначала выполните `hotelctl up`, чтобы обновить публикацию портов.

## Runtime-конфигурация в отдельном etcd

`config-etcd.yaml` задаёт отдельный etcd `3.6.15`: Deployment `config-etcd` с одной репликой и стратегией `Recreate`, Service `config-etcd` и PVC `config-etcd-data` на 1 GiB. Он хранит только прикладную runtime-конфигурацию и не использует данные control plane Kubernetes. Стабильный адрес для сервисов — `http://config-etcd.hotelflow.svc.cluster.local:2379` (в namespace `hotelflow` достаточно `http://config-etcd:2379`).

Это одноузловая учебная установка без HA. При перезапуске Pod хранилище временно недоступно, значения сохраняются на PVC. Увеличивать `replicas` этого Deployment нельзя: дополнительные члены etcd требуют отдельных томов и настройки membership. `stop` сохраняет том k3s, `reset` не очищает runtime overrides, `destroy` удаляет весь стенд вместе с данными.

Для локального доверенного стенда используются HTTP и доступ без логина. Клиентский Service использует NodePort 32379, Docker публикует его на `127.0.0.1:2379`; peer-порт слушает loopback внутри Pod. Это не конфигурация для общего или публичного кластера.

Запуск из корня проекта:

```bash
# После hotelctl up etcd уже развёрнут. Добавить его в существующий стенд отдельно:
make config-etcd-deploy
make config-etcd-health

# После hotelctl up доступ уже открыт; отдельный терминал не нужен:
./bin/hotelctl config get api-gateway hotels_list_enabled
./bin/hotelctl config set api-gateway hotels_list_enabled true
```

`HOTEL_KUBECONFIG` по умолчанию равен `.local/hotelctl/kubeconfig` и нужен только Makefile-командам,
которые вызывают kubectl. Локальный клиентский порт etcd — 2379. Он сохраняется при пересоздании Pod;
после перезапуска компонента переподключается сам клиент. `hotelctl access` показывает все адреса.

Startup/readiness используют `/readyz`, liveness — `/livez`. Backend quota — 128 MiB, история revisions автоматически compact-ится с retention 1 час; отставший watch должен выполнить полный resync. Compaction не заменяет defrag и резервные копии. Параметры: [официальная документация etcd](https://etcd.io/docs/v3.6/op-guide/configuration/), [семантика probes](https://etcd.io/docs/v3.6/op-guide/monitoring/).

Reader запускается в API Gateway автоматически; Deployment задаёт внутренний endpoint etcd и префикс сервиса. `GET http://127.0.0.1:8081/healthcheck/ready` содержит проверку `runtime_config` — готовность локального клиента, без проверки доступности etcd или конкретной revision. При недоступном etcd доступны defaults или последнее валидное состояние в памяти. Проверка флага в HTTP/gRPC обработчиках пока не добавлена: успешный `hotelctl config set` подтверждает запись в etcd, reader применяет её асинхронно.

### Обновление старого стенда

Если первый `up` сообщает конфликт `.spec.type` у `Service/akhq` с
`kubectl-client-side-apply`, старый UI создавался через прежний Make target.
Однократно переключите тип существующего Service, затем повторите `up`:

```bash
./bin/hotelctl kubectl -n hotelflow patch service akhq --type=merge --field-manager=hotelctl -p '{"spec":{"type":"NodePort"}}'
./bin/hotelctl up
```

PVC и данные при этом не удаляются. На свежем стенде этот шаг не нужен.
