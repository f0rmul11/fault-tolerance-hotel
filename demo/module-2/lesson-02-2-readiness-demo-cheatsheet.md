# Урок 2 — демо readiness

**Показываем:** без проверки RTConfig новые Pod принимают запросы с default-флагом `false`. С проверкой — ждут конфигурацию, а трафик обслуживают старые Pod.

Все команды из корня проекта:

```bash
cd /Users/d.bulykin/Desktop/faut-tolerance-course/hotel
```

Работаем в `module-2/demo`, развёрнутой перед первым показом. После демо Recreate вернуть Review на RollingUpdate. Graceful shutdown с паузой оставляем включённым после подготовки первого демо. Отключать его будем только перед третьим.

## 1. Подготовить уже запущенный стенд

```bash
./bin/hotelctl config set hotels rooms_list_enabled true
./bin/hotelctl status hotels
```

**Результат:** 3 Pod, `READY=true`. Стратегия Hotels: `maxUnavailable: 0`, `maxSurge: 1`.

## 2. Открыть наблюдение — терминал 1

```bash
./bin/hotelctl status hotels --watch
```

Показывает замену Pod, готовность и результат проверки RTConfig. Дальнейшие управляющие команды выполнять в терминале 3.

## 3. Показать исходный код — проверка отсутствует

В `services/hotels/internal/init.go`, функция `initHealthCheck`, проверка `runtime_config` отсутствует: readiness не ждёт начальной загрузки RTConfig.

Саму Kubernetes readinessProbe и graceful shutdown не менять.

Перед показом развернуть именно этот код при работающем etcd:

```bash
./bin/hotelctl build hotels
./bin/hotelctl deploy hotels
```

**Результат:** 3 Pod, `READY=true`, `RTCONFIG=не подключена`. Kubernetes readinessProbe есть, но загрузку конфигурации не проверяет.

## 4. Запустить нагрузку — терминал 2

```bash
./bin/hotelctl load \
  --addr 127.0.0.1:9090 \
  --method api_gateway.hotels.v1.HotelsService/ListRooms \
  --data '{"hotel_id":"cdd4685e-c610-4e7e-95ee-c51fb6807400"}' \
  --rps 20 --duration 5m
```

Запросы идут через Gateway в Hotels, без Review. **Перед продолжением убедиться: ошибок нет.** ID отеля — из seed стенда.

## 5. Отключить источник конфигурации

```bash
./bin/hotelctl stop config-etcd
```

Подождать 10 секунд. **Запросы успешны:** работающие Pod сохранили ранее загруженный `true`.

**Вопрос аудитории:** «Что произойдёт, если сейчас заменить Pod?»

## 6. Запустить замену Pod

```bash
./bin/hotelctl rollout restart hotels
```

**Результат:** новые Pod становятся Ready, старые удаляются. Новые не загрузили конфигурацию, используют default `false` и возвращают `FailedPrecondition` (`grpc_9`).

Дождаться замены всех старых Pod. Не ожидать ровно 33/66% ошибок: gRPC-соединение Gateway может удерживать трафик на одном экземпляре.

## 7. Восстановить конфигурацию

```bash
./bin/hotelctl start config-etcd
```

**Дождаться успешных запросов:** новые Pod повторно подключаются к etcd и загружают `true` без перезапуска. Это асинхронно: в повторной проверке восстановление заняло около 12 секунд после готовности etcd; время может отличаться. Не останавливать нагрузку сразу после `start`. После восстановления остановить её через Ctrl+C.

## 8. Добавить проверку и развернуть исправление

Добавить в `initHealthCheck` после `healthcheck.NewHandler()`:

```go
a.healthCheck.AddReadinessCheck("runtime_config", func() error {
    return a.runtimeConfig.Load().CheckInitialSync()
})
```

```bash
./bin/hotelctl build hotels
./bin/hotelctl deploy hotels
```

**Дождаться:** 3 Pod, `READY=true`, `RTCONFIG=success`.

## 9. Повторить тот же сбой

В терминале 2 снова запустить нагрузку из шага 4. Убедиться, что запросы успешны. В терминале 3:

```bash
./bin/hotelctl stop config-etcd
./bin/hotelctl rollout restart hotels
```

**Показать в течение 15–30 секунд:**

| Что видно | Почему |
| --- | --- |
| 3 старых Pod Ready | Конфигурация уже загружена |
| 1 новый Pod NotReady | Первого snapshot ещё нет |
| Rollout остановился | Нельзя убрать старый Pod до готовности замены |
| Запросы успешны | Новый Pod не получает трафик; обслуживают старые |

## 10. Вернуть etcd и завершить показ

```bash
./bin/hotelctl start config-etcd
```

**Результат:** новый Pod загружает конфигурацию → становится Ready → rollout продолжается. Дождаться трёх готовых Pod одной новой ревизии. Остановить нагрузку и watch через Ctrl+C.

**Критерий: 0 ошибок за весь исправленный прогон**, включая завершение rollout после возврата etcd. Readiness и graceful shutdown с паузой остаются включены. Проверку readiness больше не удаляем — она нужна и в третьем демо.

**Вывод:** «Процесс запущен — ещё не значит, что он готов обслуживать запросы. Правильная readiness удерживает рабочие Pod, пока замена не готова».

---

**Важно:** при выключенном etcd использовать только `rollout restart`, а не `deploy/up`: последние могут снова поднять etcd. `build/deploy` выполняем между прогонами при работающем etcd.

**Если нужно прервать демо:** `start config-etcd` → добавить блок проверки → `build hotels` → `deploy hotels`. Итог: проверка в коде, флаг `true`, 3 готовых Pod.

После показа при сохранении исправления обновить ожидание readiness в `services/hotels/internal/app_test.go`: при недоступном etcd — HTTP 503 и `runtime_config=rtconfig.ErrNotSynced.Error()` вместо HTTP 200.
В `TestHealthLifecycle` после старта приложения и до завершения без синхронизированного RTConfig также ожидать HTTP 503 вместо HTTP 200.
