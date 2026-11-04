# Демо 3 · CB защищает зависимость, fallback — клиента · 15–18 минут

Терминалы: **1 — нагрузка, 2 — логи одного Hotels pod, 3 — fault, 4 — runtime config**.
Hotels остаётся в 3 репликах. Каждый pod имеет свой CB.

Grafana открыть в двух вкладках, Protocol `grpc`:
- `api-gateway`, Traffic handler `/api_gateway.hotels.v1.HotelsService/ListHotels`: ошибки клиента.
- `review`, Traffic handler `/review.review.v1.ReviewService/GetHotelRatings`: жёлтый RPS в Traffic и Errors.

График считает окно 1m: для сравнения дать минуту устойчивой нагрузки после перехода. Логи показывают момент перехода сразу.

## 1. Без защиты — здоровая цепочка

В Hotels YAML у GetHotelRatings `enabled: false`, `telemetry.log_level: info`. Retry для ListHotels выключен.

```bash
cd /Users/d.bulykin/Desktop/faut-tolerance-course/hotel
make build
./bin/hotelctl build hotels review
./bin/hotelctl deploy hotels review
./bin/hotelctl config set hotels review_fallback_enabled false
```

Терминал 1:

```bash
./bin/hotelctl load \
  --addr 127.0.0.1:9090 \
  --method api_gateway.hotels.v1.HotelsService/ListHotels \
  --data '{}' --rps 30 --duration 10m --clients 4
```

Показать 0 ошибок, Review получает около 30 RPS. Каждый ListHotels вызывает один batch RPC Review.

## 2. Review отвечает отказом

Терминал 3:

```bash
./bin/hotelctl fault hotels review \
  --unavailable --direction handler --duration 6m --timeout 7m
```

После `ACTIVE`: 100% ошибок у клиента; Review по-прежнему получает поток запросов.
Это имитация отказа **внутри Review**, а не доказательство настоящей перегрузки CPU.
Envoy `--direction request` здесь не подходит: он сам отсекает запросы до сервиса.

Вопрос: «Зависимость не справляется. Зачем продолжать отправлять весь поток?»

## 3. Включить только CB

**Ctrl+C** в терминалах 1 и 3, дождаться снятия fault. Флаг fallback остаётся false.
В `services/hotels/config/config.yaml`:

```yaml
circuit:
  __default__:
    enabled: false
    separate_circuit_per_handler_enabled: true
  handlers:
    - method: /review.review.v1.ReviewService/GetHotelRatings
      enabled: true
      max_requests: 1
      timeout: 5s
      interval: 20s
      bucket_period: 5s
      threshold_consecutive: 3
      threshold_percentage: 100
      failure_codes: [Unavailable, DeadlineExceeded]
```

```bash
./bin/hotelctl build hotels
./bin/hotelctl deploy hotels
./bin/hotelctl kubectl -n hotelflow get pods -l app.kubernetes.io/name=hotels
```

Терминал 1:

```bash
./bin/hotelctl load \
  --addr 127.0.0.1:9090 \
  --method api_gateway.hotels.v1.HotelsService/ListHotels \
  --data '{}' --rps 30 --duration 10m --clients 4
```

Показать здоровые ответы и RPS Review. CB закрыт.

## 4. Снова отказ — увидеть защиту Review

Терминал 3:

```bash
./bin/hotelctl fault hotels review \
  --unavailable --direction handler --duration 6m --timeout 7m
```

Терминал 2 — после первого сбоя найти pod, где есть переходы CB (gRPC может закрепиться за одной репликой):

```bash
./bin/hotelctl kubectl -n hotelflow logs deployment/hotels --all-pods=true --prefix --since=1m | \
  rg 'circuit state changed'
```

Заменить `HOTELS_POD` именем из префикса строки; затем смотреть только его:

```bash
./bin/hotelctl kubectl -n hotelflow logs pod/HOTELS_POD -f --since=1m | \
  jq --unbuffered -Rc 'fromjson? | select(.msg == "circuit state changed" or .msg == "circuit failure observed") | del(.source)'
```

В логах выбранного pod: ошибки → `from: closed, to: open`.
Перед открытием нужно минимум 11 завершённых наблюдений в окне; после здорового прогрева достаточно 3 последовательных ошибок.
Review RPS падает почти до нуля, клиент продолжает получать ошибки. В open вызовы отсекаются в Hotels.

Через 5s, при следующем запросе: `open → half-open → open`.
Проба получает UNAVAILABLE от Review; остальные запросы при открытом CB получают `circuit is open`.
Остаются редкие пробы: примерно один запрос за 5s на каждый CB, получающий нагрузку. Если активен один pod — ~0.2 RPS; три — ~0.6 RPS.

## 5. Восстановить зависимость

**Ctrl+C** только в терминале 3. Нагрузку оставить.
Следующая успешная проба: `open → half-open → closed`. Review снова получает полный поток, ошибки исчезают.

Вопрос: «CB дал зависимости передышку. А что получал клиент, пока она восстанавливалась?»

## 6. Fallback через etcd без redeploy

Терминал 3 — повторить отказ:

```bash
./bin/hotelctl fault hotels review \
  --unavailable --direction handler --duration 6m --timeout 7m
```

Дождаться `open`, показать ошибки клиента. Терминал 4:

```bash
./bin/hotelctl config set hotels review_fallback_enabled true
./bin/hotelctl config get hotels review_fallback_enabled
curl -sS http://localhost:8080/v1/hotels | jq '.hotels[] | {name, rating, ratings_degraded}'
```

После watch-обновления ошибки нагрузки исчезают. Ответ успешный, рейтинг отсутствует,
`ratings_degraded: true`. Pod не перезапускается; CB продолжает пробы, Review защищён.
Fallback применяется и к ошибке Review до открытия CB, но не скрывает отмену общего контекста.

Для обратного переключения под тем же сбоем:

```bash
./bin/hotelctl config set hotels review_fallback_enabled false
```

Ошибки клиента возвращаются. Вернуть деградацию:

```bash
./bin/hotelctl config set hotels review_fallback_enabled true
```

## 7. Завершить

**Ctrl+C** в терминале 3, дождаться восстановления CB. Рейтинги вернутся даже при включённом fallback.
Остановить нагрузку и логи. Вернуть флаг и `enabled: false` у CB в YAML:

```bash
./bin/hotelctl config set hotels review_fallback_enabled false
./bin/hotelctl build hotels
./bin/hotelctl deploy hotels
```

Если fault не снялся:

```bash
./bin/hotelctl fault hotels review --clear
```

Дополнительно: повторить этапы 3–5 с `timeout: 15s`. После redeploy выбрать новый pod;
паузы между пробами станут втрое длиннее. Это пауза открытого CB, не timeout RPC.
