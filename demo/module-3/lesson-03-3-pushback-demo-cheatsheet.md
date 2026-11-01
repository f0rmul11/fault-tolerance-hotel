# Урок 3 · демо 3: серверный pushback

**Показываем:** сервер задаёт паузу между попытками или запрещает повтор.

Перед новой бронью в UI: GET `/v1/bookings` → перенести `bookings_version` в POST. Для повтора сохранить исходные тело и ключ.

## 1. Подготовить ретраи без бюджета

```bash
cd /Users/d.bulykin/Desktop/faut-tolerance-course/hotel
```

В `services/api-gateway/config/config.yaml` поставить `telemetry.log_level: debug` и заменить секцию `retry`:

```yaml
retry:
  __default__:
    enabled: false
    separate_retry_per_handler_enabled: false
    max_attempts: 3
    initial_backoff: 200ms
    max_backoff: 1s
    backoff_multiplier: 2
    retryable_codes: [Unavailable]
    throttler:
      throttling_enabled: false
      throttle_max_tokens: 10
      throttle_token_ratio: 1
  handlers:
    - method: /booking.booking.v1.BookingService/CreateBooking
      enabled: true
```

```bash
./bin/hotelctl build api-gateway
./bin/hotelctl deploy api-gateway
```

Открыть UI бронирования и подготовить запрос. В терминале 2 открыть логи:

```bash
./bin/hotelctl logs api-gateway -f --tail 0
```

## 2. Задать серверную паузу — терминал 1

```bash
./bin/hotelctl fault api-gateway booking \
  --unavailable --pushback-ms 1500 --duration 1m
```

Дождаться `ACTIVE`. В UI **один раз** нажать «Забронировать».

**Показать в логах:** три попытки, две паузы по 1,5 с вместо клиентских 200 и 400 мс. В UI — ошибка примерно через 3 с.

В терминале 1 нажать **Ctrl+C**, дождаться снятия сбоя.

## 3. Запретить повтор — терминал 1

```bash
./bin/hotelctl fault api-gateway booking \
  --unavailable --pushback-ms=-1 --duration 1m
```

Дождаться `ACTIVE`. В UI **один раз** повторить запрос.

**Показать в логах:** одна попытка и `reason=pushback`, хотя в YAML разрешено три попытки.

## 4. Завершить показ

В терминале 1 нажать **Ctrl+C**, дождаться снятия сбоя.

```bash
./bin/hotelctl fault api-gateway booking --clear
```
