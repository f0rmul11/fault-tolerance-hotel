# Урок 3 · демо 1: ретраи

**Показываем:** один запрос пользователя → три попытки Gateway → Booking с паузами.

Перед новой бронью в UI: GET `/v1/bookings` → перенести `bookings_version` в POST. Для повтора сохранить исходные тело и ключ.

## 1. Подготовить вариант без ретраев

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
      enabled: false
```

```bash
./bin/hotelctl build api-gateway
./bin/hotelctl deploy api-gateway
```

Открыть UI бронирования и подготовить запрос на свободный номер. Адреса стенда:

```bash
./bin/hotelctl access
```

## 2. Включить сбой — терминал 1

```bash
./bin/hotelctl fault api-gateway booking --unavailable --duration 2m
```

Дождаться `ACTIVE`. В UI **один раз** нажать «Забронировать».

**Показать:** пользователь получает ошибку; повторов нет.

В терминале 1 нажать **Ctrl+C**, дождаться снятия сбоя.

## 3. Включить ретраи

В YAML изменить только `retry.handlers[0].enabled` на `true`.

```bash
./bin/hotelctl build api-gateway
./bin/hotelctl deploy api-gateway
```

Открыть логи в терминале 2:

```bash
./bin/hotelctl logs api-gateway -f --tail 0
```

## 4. Повторить запрос со сбоем — терминал 1

```bash
./bin/hotelctl fault api-gateway booking --unavailable --duration 2m
```

Дождаться `ACTIVE`. В UI **один раз** повторить запрос.

**Показать в логах:** три `retry: invoke`, между ними `retry: wait` примерно 200 и 400 мс. В UI — одна итоговая ошибка.

## 5. Снять сбой и показать успех

В терминале 1 нажать **Ctrl+C**, дождаться снятия сбоя. Повторить запрос в UI.

**Показать:** бронь создана. Это новый запрос пользователя после снятия сбоя.
