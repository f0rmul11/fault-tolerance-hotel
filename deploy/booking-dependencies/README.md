# Зависимости бронирования

`hotelctl up` и `hotelctl deploy` для Booking, Check-in, Hotels, Review, Notification или gateway применяют этот Kustomize-набор, ждут PostgreSQL/Kafka, затем выполняют миграции и seed до rollout сервисов. При первом запуске контейнерные образы скачиваются из registry.

- PostgreSQL `16.10-alpine`: четыре отдельных Deployment/Service — `booking-postgres`, `check-in-postgres`, `hotels-postgres`, `review-postgres`. Каждый использует собственный PVC `<сервис>-db-data`, Secret `<сервис>-database` и единственную прикладную БД. `POSTGRES_USER`/`POSTGRES_DB` создают владельца при первом запуске. Миграции, seed и приложение получают только его DSN. Все таблицы — в `public`, без создания схем сервиса.
- Notification и API Gateway не получают PostgreSQL DSN. Notification использует общий брокер напрямую без inbox.
- Apache Kafka `3.9.1`: один узел KRaft, одна реплика; адрес внутри namespace — `kafka:9092`. Job явно создаёт `booking.events.v1` и `checkin.events.v1` через `--if-not-exists` до запуска outbox relay.
- Goose `3.26.0`: закреплённый multiarch wrapper `ghcr.io/kukymbr/goose-docker`, запускающий `/bin/goose`. Оригинальные SQL из `services/*/tools/migrations` монтируются в Job через ConfigMap; копий DDL здесь нет.
- После миграций выполняются seeds из `services/hotels/tools/seed/20261007.sql` и `services/review/tools/seed/20261007.sql`. Каталог содержит три отеля и 11 номеров, рейтинги двух отелей.

Имя Job зависит от SQL и версии Goose. Повторный `up` использует завершённый Job; новые SQL создают новый Job и применяются через стандартную таблицу версий Goose. Текущая смена модели выполнена inplace по решению пользователя: production-данных нет, старый учебный стенд пересоздаётся. `goose up` поверх БД старой модели не выполняет её преобразование; обычный повторный запуск актуальной схемы сохраняет данные.

При ошибке посмотрите `hotelctl kubectl -n hotelflow logs job/ИМЯ -c booking` (либо `hotels`, `review`, `check-in`, `hotels-seed`, `review-seed`). После исправления причины можно явно удалить только неуспешный Job и повторить `up`; CLI не удаляет данные и не повторяет неуспешную миграцию автоматически.

Данные PostgreSQL/Kafka находятся в PVC внутри постоянного тома k3s. `stop` сохраняет их, `destroy` удаляет весь стенд. Учётные данные синтетические; внешних портов для PostgreSQL и Kafka этот набор не открывает.

Прежний общий `postgres` и `postgres-init` исключены из манифестов. Новые PVC `<сервис>-db-data` не используют старый общий `booking-postgres-data`. Изменение исходников не удаляет существующие Kubernetes-объекты/тома: обычный apply не выполняет prune. Старый учебный стенд можно пересоздать явно; автоматического удаления данных или переноса старых схем нет.

## Просмотр Kafka через AKHQ

`akhq.yaml` входит в общий Kustomize-набор. Deployment использует `tchiotludo/akhq:dev`, конфигурация `AKHQ_CONFIGURATION` задаёт соединение `hotelflow` → `kafka:9092`. Данные брокера остаются в существующем Kafka; AKHQ не требует собственной БД или PVC.

Для отдельного развёртывания UI: `make kafka-ui-deploy`. Для доступа с компьютера: `make kafka-ui`, затем http://localhost:8090. Туннель работает до Ctrl+C/завершения процесса; после остановки или пересоздания Pod его можно запустить той же командой. Публичные порты k3s менять не нужно. Broker bootstrap задаётся по [схеме AKHQ](https://akhq.io/docs/configuration/brokers.html).
