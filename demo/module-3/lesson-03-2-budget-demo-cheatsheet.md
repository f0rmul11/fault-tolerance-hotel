# Урок 3 · демо 2: retry budget под нагрузкой

**Показываем:** без бюджета 10 запросов/с превращаются примерно в 30 попыток/с; с исчерпанным бюджетом остаются исходные 10 запросов/с.

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

Оставить **одну реплику Gateway**. Открыть логи в терминале 3:

```bash
./bin/hotelctl logs api-gateway -f --tail 0
```

## 2. Включить сбой — терминал 1

```bash
./bin/hotelctl fault api-gateway booking --unavailable --duration 2m
```

Дождаться `ACTIVE`.

## 3. Запустить нагрузку — терминал 2

```bash
./bin/hotelctl load \
  --addr 127.0.0.1:9090 \
  --method api_gateway.booking.v1.BookingService/CreateBooking \
  --data-file demo/module-3/booking-request.json \
  --header x-demo-role:GUEST \
  --header x-demo-actor:eda02a57-8eb4-4d48-bc05-706c85747e9a \
  --rps 10 --duration 30s --clients 2
```

**Показать:** генератор отправляет около 300 запросов; в логах Gateway на каждый запрос идут попытки 1, 2, 3 — суммарно около 900 попыток.

Дождаться завершения нагрузки. Затем **Ctrl+C** в терминале 1 и дождаться снятия сбоя. Нагрузку запускать только при активном сбое.

## 4. Включить бюджет

В `retry.__default__.throttler` поменять только `throttling_enabled: true`. Оставить `throttle_max_tokens: 10`, `throttle_token_ratio: 1`.

```bash
./bin/hotelctl build api-gateway
./bin/hotelctl deploy api-gateway
```

В терминале 3 остановить старые логи через **Ctrl+C** и открыть новые:

```bash
./bin/hotelctl logs api-gateway -f --tail 0
```

## 5. Повторить ту же нагрузку

Повторить **шаги 2–3** с теми же командами.

**Показать в логах:** `reason=budget`, токены падают до 0. После первых нескольких повторов на каждый запрос остаётся одна попытка — суммарно около 300 плюс несколько дополнительных.

Дождаться завершения нагрузки, затем снять сбой через **Ctrl+C** в терминале 1.

## 6. Показать пополнение бюджета

**Не перезапускать Gateway.** В UI создать одну бронь свободного номера с новым ключом.

**Показать:** запрос успешен; в логе `throttler: successful rpc` токены выросли с 0 до 1. Одного успеха ещё недостаточно для возобновления ретраев.
