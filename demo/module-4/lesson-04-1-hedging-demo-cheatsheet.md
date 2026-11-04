# Демо 1 · Hedging · 15 минут

Терминалы: **1 — fault, 2 — нагрузка и deploy, 3 — логи**. Booking уже в 3 репликах.

Grafana → **Service Overview → Traffic → Service response time**:
`Service: api-gateway`, `Protocol: grpc`, `Traffic handler: /api_gateway.booking.v1.BookingService/ListBookings`.
Диапазон **Last 30 minutes**, refresh **10s**. Смотрим p50 / p95 / p99; жёлтая линия — RPS пользовательских запросов.

## 1. Без hedging

В `services/api-gateway/config/config.yaml` у ListBookings: `enabled: false`. Retry для этого метода выключен.

```bash
cd /Users/d.bulykin/Desktop/faut-tolerance-course/hotel
make build
./bin/hotelctl build api-gateway
./bin/hotelctl deploy api-gateway
```

Терминал 1:

```bash
./bin/hotelctl fault api-gateway booking \
  --delay-profile '8:150ms,2:800ms' --duration 5m --timeout 6m
```

8% попыток получают +150ms, ещё 2% — +800ms, остальные 90% без задержки. Группы не складываются.

После `ACTIVE`, терминал 2:

```bash
./bin/hotelctl load \
  --addr 127.0.0.1:9090 \
  --method api_gateway.booking.v1.BookingService/ListBookings \
  --data '{}' \
  --header x-demo-role:GUEST \
  --header x-demo-actor:eda02a57-8eb4-4d48-bc05-706c85747e9a \
  --rps 30 --duration 120s --clients 4
```

Последняя минута: p95 в средней группе, p99 — в длинном хвосте. Вопрос: «Когда стоит отправить дубль?»

## 2. Срезать длинный хвост

**Ctrl+C** в терминале 1, дождаться снятия fault. Конфиг Gateway:

```yaml
hedge:
  __default__:
    enabled: false
    separate_hr_per_handler_enabled: true
  handlers:
    - method: /booking.booking.v1.BookingService/ListBookings
      enabled: true
      window_size: 1000
      warmup: true
      percentile: 0.99
      percentile_offset: 0.06
      budget: 0.2
      reset_period: 30s
```

**Пояснить offset:** задержка перед дублем = квантиль `percentile − offset` текущего окна.
Выбрали `0.06` под профиль: 90% быстрых попыток, 8% с +150ms, 2% с +800ms.

- `0.99 − 0.06 = p93`: средняя группа, дубль примерно через 153ms.
- `0.95 − 0.06 = p89`: быстрая группа, дубль примерно через 3–5ms.

Один offset позволяет сменой percentile перейти между группами. Это не универсальная константа; больший offset запускает дубли раньше и увеличивает нагрузку.

**Почему хвост остаётся:** при первом конфиге уже ждём ~153ms до дубля; даже быстрый ответ за 3ms даст итог ~156ms. Если дубль тоже задержан, ждём дольше. Hedging сокращает хвост, но не гарантирует минимальную задержку.

Терминал 2:

```bash
./bin/hotelctl build api-gateway
./bin/hotelctl deploy api-gateway
```

Терминал 1 — тот же сбой:

```bash
./bin/hotelctl fault api-gateway booking \
  --delay-profile '8:150ms,2:800ms' --duration 5m --timeout 6m
```

После `ACTIVE`, терминал 2:

```bash
./bin/hotelctl load \
  --addr 127.0.0.1:9090 \
  --method api_gateway.booking.v1.BookingService/ListBookings \
  --data '{}' \
  --header x-demo-role:GUEST \
  --header x-demo-actor:eda02a57-8eb4-4d48-bc05-706c85747e9a \
  --rps 30 --duration 180s --clients 4
```

Первые ~34 секунды — заполнение окна **под сбоем**, без дублей. Затем порог по p93: дубль примерно через 150ms.
Сравнить последнюю минуту: p99 падает к средней группе, p95 остаётся около неё.

## 3. Снизить и p95

**Ctrl+C** в терминале 1. Изменить только `percentile: 0.95` — при прежнем offset порог считается по p89.

Терминал 2:

```bash
./bin/hotelctl build api-gateway
./bin/hotelctl deploy api-gateway
```

Терминал 1:

```bash
./bin/hotelctl fault api-gateway booking \
  --delay-profile '8:150ms,2:800ms' --duration 5m --timeout 6m
```

После `ACTIVE`, терминал 2:

```bash
./bin/hotelctl load \
  --addr 127.0.0.1:9090 \
  --method api_gateway.booking.v1.BookingService/ListBookings \
  --data '{}' \
  --header x-demo-role:GUEST \
  --header x-demo-actor:eda02a57-8eb4-4d48-bc05-706c85747e9a \
  --rps 30 --duration 180s --clients 4
```

После прогрева дубли стартуют раньше. На последней минуте сравнить p95 с предыдущим этапом.
`percentile` выбирает порог запуска дубля, а не гарантирует итоговый квантиль. Гистограмма Grafana даёт приближённые значения.
Обе попытки замедляются примерно в 1% случаев (10% × 10%): p99 может колебаться между быстрой и средней группой. Устойчивый эффект этого этапа — снижение p95. После остановки нагрузки квантиль по остаткам минутного окна не сравнивать.

## 4. Цена ускорения

Терминал 3 — запустить во время нагрузки:

```bash
./bin/hotelctl kubectl -n hotelflow logs deployment/api-gateway -f --tail=0 | \
  jq --unbuffered -R 'fromjson? | select((.msg // "") | startswith("execute hedged")) | .msg'
```

В Jaeger: ListBookings, перекрывающиеся попытки и отмена проигравшей. Другая реплика не гарантирована.
Вопрос: «Что будет, если обе попытки упрутся в одну перегруженную БД?»

## 5. Завершить

**Ctrl+C**, дождаться снятия fault. У ListBookings вернуть `enabled: false`.

```bash
./bin/hotelctl build api-gateway
./bin/hotelctl deploy api-gateway
```

Если fault не снялся:

```bash
./bin/hotelctl fault api-gateway booking --clear
```

Переход: «А сколько ждать, если быстрых ответов вообще нет?»
