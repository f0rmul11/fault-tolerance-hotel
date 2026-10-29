# Канареечный релиз Review

Стенд должен быть запущен через `hotelctl up`. Все команды выполняются из корня репозитория ветки `module-2/review-canary`.

## Подключить один раз

```bash
go build -o bin/hotelctl ./hotelctl/cmd/hotelctl
./bin/hotelctl canary setup review
```

Команда устанавливает Istio 1.31.3 (`minimal`) и Argo Rollouts v1.10.1, создаёт стабильную ревизию по `rollout.yaml`, используя **уже работающий образ Review** и переводит Hotels на новый адрес. Нужны Docker и работающий k3s. hotelctl применяет локальные YAML через Kubernetes API; интернет нужен для загрузки контейнерных образов. Повторный `setup` сохраняет текущий релиз и веса.

Istio sidecar добавляется только в Hotels и Review. Захватывается gRPC-порт 9090; PostgreSQL, etcd, телеметрия и остальные связи через существующий Envoy сохраняют свои маршруты. Argo CD, ingress gateway и дополнительные системы наблюдаемости не устанавливаются.

## Выпустить новую версию

```bash
# После изменения кода Review
./bin/hotelctl build review
./bin/hotelctl canary deploy review

# В отдельном терминале
./bin/hotelctl canary status review --watch

# После проверки на каждом шаге
./bin/hotelctl canary step review 10
./bin/hotelctl canary step review 25
./bin/hotelctl canary step review 50
./bin/hotelctl canary step review 100
./bin/hotelctl canary promote review
```

`deploy` создаёт новую ревизию с одной готовой репликой и весом **0%**. Для тренировки с тем же образом: `canary deploy review --restart`. Затем Argo увеличивает число canary-реплик вместе с весом; stable сохраняет три реплики до завершения релиза. Шаги выполняются последовательно. На 100% остаётся ручная пауза: `promote` делает новую ревизию стабильной, после чего старые Pod завершаются с задержкой 30 секунд.

Нагрузка должна идти **через API Gateway** на `ListHotels` или `GetHotel`: эти методы вызывают Review. `ListRooms` для этой проверки не подходит.

```bash
./bin/hotelctl load \
  --addr 127.0.0.1:9090 \
  --method api_gateway.hotels.v1.HotelsService/ListHotels \
  --data '{}' --rps 50 --duration 3m
```

Смотрите gRPC-коды и долю ошибок в выводе нагрузки, latency — в существующей Grafana. Переход на следующий шаг ручной: автоматического анализа метрик здесь нет. `status` показывает настроенные веса, ревизии и готовность реплик; фактическая доля RPC колеблется вокруг заданного процента. Она распределяется по запросам, включая запросы внутри постоянных gRPC-соединений.

## Откатить или выключить

```bash
# Вернуть новые запросы на stable
./bin/hotelctl canary abort review

# После исправления кода
./bin/hotelctl build review
./bin/hotelctl canary deploy review

# Вернуть обычный режим после abort или promote
./bin/hotelctl canary disable review
```

После `abort` статус `Degraded / RolloutAborted` ожидаем: трафик уже направлен на stable, а spec хранит отклонённую ревизию. Повторить учебный релиз того же образа можно с `--restart`.

`disable` запускает обычный Deployment с последним стабильным образом, возвращает Hotels на прежний Envoy и удаляет ресурсы канарейки. Контроллеры остаются для следующего `setup`. Во время канареечного режима обычные `deploy`, `rollout restart`, `start/stop/restart` для Hotels/Review и `reset` заблокированы, чтобы не обойти управление Argo. Для них сначала выполните `disable`.

## Что происходит внутри

```text
API Gateway → существующий Envoy → Hotels
                                    │ Istio sidecar: выбор на каждый RPC
                                    ├─ stable: 90% → Review, старая ревизия
                                    └─ canary: 10% → Review, новая ревизия
```

- `rollout.yaml` — полный Rollout: образ, probes, ресурсы, конфигурация, три реплики и ручные шаги. `setup` подставляет работающий образ, `deploy` — результат `hotelctl build review`. Изменения шаблона Pod из YAML также применяются при `deploy`; шаблон больше не копируется из Deployment.
- `service.yaml`, `virtual-service.yaml`, `destination-rule.yaml` — Service `review-rollout` и правила маршрутизации с двумя subsets. Argo добавляет к subsets метку `rollouts-pod-template-hash` соответствующего ReplicaSet и меняет веса при переходах.
- `deploy/istio` и `deploy/argo-rollouts` содержат локальные манифесты инфраструктуры. hotelctl сначала применяет CRD, ждёт их регистрации, затем запускает контроллеры.
- Обычный Deployment `review` остаётся с нулём реплик для обратимого переключения. Его Service и прямой NodePort во время канарейки не используются. Проверяйте трафик через Gateway, а состояние — через `canary status`; `logs review` относится к обычному Deployment.
- Сбой на связи Hotels → Review через прежние `hotelctl fault` в этом режиме не применяется: эта связь переведена в Istio. Другие связи остаются в существующем Envoy.
- Не применяйте весь каталог `canary` повторно во время релиза: весами и метками subsets управляет Argo. Kubernetes-команды вручную могут обойти защиту hotelctl.

Основа: [Argo Rollouts — Istio, subset-level splitting](https://argoproj.github.io/argo-rollouts/features/traffic-management/istio/), [Istio — управление перехватом трафика](https://istio.io/latest/docs/reference/config/annotations/).
