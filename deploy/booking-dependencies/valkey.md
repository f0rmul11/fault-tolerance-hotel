# Valkey OSS Cluster для урока 9

Три шарда, у каждого primary и одна replica: шесть Pod StatefulSet `valkey` в namespace
`hotelflow` текущего стенда. Valkey 9.0.8 Alpine закреплён тегом и digest multiarch-образа.
Hotels создаёт cluster-aware клиент valkey-go с отключённым клиентским кешированием.

## Запуск

Valkey запускается штатной командой `hotelctl up` (или `make up`). Манифест
`deploy/booking-dependencies/valkey.yaml` входит в общую инфраструктуру стенда.
Init-контейнер `valkey-cluster` общего migration Job ждёт PING на всех шести узлах,
создаёт кластер и проверяет слоты и репликацию. Затем `hotelctl` ждёт Ready всех
Valkey Pod перед запуском приложений. Отдельный shell-скрипт не нужен.

Кластер создаётся только если все шесть узлов пусты и ещё не объединены.
Существующий кластер сохраняется; при выполнении init-контейнера адреса peers
обновляются через CLUSTER MEET. Частично созданный кластер требует диагностики.
Завершённый Job повторно не запускается; PVC сохраняют данные и nodes.conf.
Инициализация не выполняет RESET/FLUSH или smoke-записи.

## Топология и доступ

Первоначальное распределение:

| Primary | Слоты | Replica |
| --- | --- | --- |
| valkey-0 | 0–5460 | определяется CLUSTER NODES |
| valkey-1 | 5461–10922 | определяется CLUSTER NODES |
| valkey-2 | 10923–16383 | определяется CLUSTER NODES |

Реплики первоначально valkey-3, valkey-4, valkey-5. После failover роли могут меняться.
Каждый shard отвечает за часть общих 16384 hash slots, реплика не является четвёртым шардом.

Seed-адреса для cluster-aware клиента внутри k3s:

```text
valkey-0.valkey-headless.hotelflow.svc.cluster.local:6379
valkey-1.valkey-headless.hotelflow.svc.cluster.local:6379
valkey-2.valkey-headless.hotelflow.svc.cluster.local:6379
```

Service `valkey:6379` можно использовать как seed, но он не маршрутизирует команды по
ключам. Нужен cluster-aware клиент, обрабатывающий MOVED/ASK и узнающий всю топологию.
Порт 16379 — cluster bus, не клиентский протокол. Ноды объявляют pod IP для общения
между собой и стабильные pod DNS для клиентских redirects.

Для ручной работы `hotelctl kubectl -n hotelflow exec -it valkey-0 -- valkey-cli -c` запускает CLI внутри Pod. Одиночный
port-forward на localhost не делает доступными остальные адреса, возвращаемые кластером.
На этом шаге NodePort/внешние порты не открываются. NetworkPolicy разрешает 6379 из
namespace hotelflow и 16379 только между Valkey Pod. Auth/TLS не настроены для доверенного
локального учебного namespace; это не конфигурация публичного кластера.

В CLI:

```text
CLUSTER INFO
CLUSTER NODES
CLUSTER SHARDS
SET lesson9:{example}:view v1 EX 60
SET lesson9:{example}:view v2 IFEQ v1 EX 60
GET lesson9:{example}:view
DEL lesson9:{example}:view
```

Hash tag `{example}` позволяет разместить связанные ключи в одном слоте для
транзакции/Lua. Не помещайте все ключи приложения под один общий hash tag.

## Хранение и ресурсы

Каждому Pod выделен PVC `data-valkey-N` на 1 GiB через local-path. На томе лежат
AOF и nodes.conf. Appendonly включён, appendfsync everysec, RDB-снимки отключены.
Удаление/перезапуск Pod сохраняет PVC; удаление тома всего k3s уничтожает и эти данные.
AOF и асинхронная репликация не гарантируют отсутствие потери подтверждённых записей.

Requests: 50m CPU / 128 MiB на Pod, всего 300m / 768 MiB. Limits: 1 CPU / 512 MiB
на Pod. `maxmemory 128mb`, policy `noeviction`: базовые упражнения не будут случайно
терять маркеры CAS из-за eviction. При заполнении памяти запись может получить OOM;
это отдельный сценарий. Лимит контейнера оставляет место для репликации и AOF.

StatefulSet использует Parallel, чтобы initial readiness не блокировала запуск остальных
нод до bootstrap. Startup/liveness проверяют PING, readiness — cluster_state:ok.
Стратегия OnDelete исключает неявное одновременное обновление узлов при apply.
Изменение image/config требует контролируемого последовательного пересоздания Pod
с проверкой репликации между шагами; простой apply не перезапускает процессы.

Все шесть Pod размещены на одном k3s-узле. Можно исследовать отказ процесса/Pod,
но отказ узла/ноутбука остановит весь кластер. Масштабирование replicas StatefulSet
не добавляет shard и не перераспределяет slots автоматически.

## Проверки и диагностика

Init-контейнер проверяет состояние кластера, 16384 слота, три primary,
по одной replica на primary и готовность репликации. Для ручной диагностики:

```bash
./bin/hotelctl kubectl -n hotelflow get pods -l app.kubernetes.io/name=valkey -o wide
./bin/hotelctl kubectl -n hotelflow get pvc
./bin/hotelctl kubectl -n hotelflow logs valkey-0
./bin/hotelctl kubectl -n hotelflow exec valkey-0 -- valkey-cli cluster nodes
```

Основа конфигурации: [Valkey Cluster tutorial](https://valkey.io/topics/cluster-tutorial/),
[SET и IFEQ](https://valkey.io/commands/set/),
[Valkey 9.0.8](https://valkey.io/download/releases/v9-0-8/).
