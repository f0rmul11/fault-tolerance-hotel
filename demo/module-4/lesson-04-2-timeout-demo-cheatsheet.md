# Демо 2 · Общий timeout 5 секунд · 10–12 минут

Gateway → Hotels → Review. В каталоге есть отель с рейтингом. Jaeger: `localhost:16686`.

До лекции обновить CLI, Review и Envoy:

```bash
cd /Users/d.bulykin/Desktop/faut-tolerance-course/hotel
make build
./bin/hotelctl build api-gateway hotels review
./bin/hotelctl deploy api-gateway hotels review
```

## 1. Здоровая цепочка

Retry и CB выключены; fallback выключить командой `./bin/hotelctl config set hotels review_fallback_enabled false`. Общий timeout Gateway — **5s**. Запрос без дополнительных заголовков и клиентского timeout.

```bash
cd /Users/d.bulykin/Desktop/faut-tolerance-course/hotel
./bin/hotelctl config set hotels review_fallback_enabled false
curl -si -w '\nHTTP=%{http_code} time=%{time_total}s\n' \
  http://localhost:8080/v1/hotels
```

Показать 200. По `X-Trace-Id` открыть здоровый trace: Gateway → Hotels → Review.

## 2. Замедлить вызов Review

Терминал 1:

```bash
./bin/hotelctl fault hotels review \
  --slowdown 8s --direction handler --duration 3m
```

После `ACTIVE` — терминал 2, тот же запрос:

```bash
curl -si -w '\nHTTP=%{http_code} time=%{time_total}s\n' \
  http://localhost:8080/v1/hotels
```

Ожидаем **504 примерно через 5s**, хотя задержка — 8s. Сохранить `X-Trace-Id`.

## 3. Разобрать ожидание в Jaeger

Открыть trace по `X-Trace-Id`:

- Входящий запрос Gateway длится около **5s**.
- **Server span Review / GetHotelRatings длится около 5s**. Внутри: `demo.handler_delay_ms=8000`, события `demo.delay.started` и `demo.delay.canceled`.
- Client span Hotels → Review ожидает столько же; времена вложенных spans не складываются.
- В цепочке видны `DeadlineExceeded` / `Canceled`: общий бюджет закончился, ожидание оборвалось.

Задержка выполняется внутри Review до чтения БД. Таймер задан на 8s, но ожидание прерывается по контексту примерно через 5s; SQL не запускается.

В коде показать: Gateway задаёт deadline → Hotels передаёт `ctx` в Review → gRPC переносит оставшийся бюджет.

Вопрос: «Где потратили 5 секунд — в собственной работе Hotels или в ожидании Review?»

## 4. Снять сбой

Терминал 1: **Ctrl+C**, дождаться снятия fault. Терминал 2:

```bash
curl -si -w '\nHTTP=%{http_code} time=%{time_total}s\n' \
  http://localhost:8080/v1/hotels
```

Показать быстрый 200. Если fault не снялся:

```bash
./bin/hotelctl fault hotels review --clear
```

## 5. От gRPC к HTTP — код, 3–4 минуты

Переход: «В gRPC перенос бюджета делает библиотека. Как повторить этот принцип для HTTP?»

В `services/api-gateway/internal/pkg/requesttimeout` показать по порядку:

1. `middleware.go`, **Limit**: на входе Gateway создаём deadline через 5s — это наша политика.
2. `transport.go`, **RoundTrip**: вызывается через `client.Do(req)`, берёт deadline из контекста и передаёт `time.Until(deadline)` в `X-Request-Timeout`.
3. `middleware.go`, **Middleware**: downstream восстанавливает контекст из остатка. Без заголовка deadline не добавляется, `0s` сразу истекает, неправильный заголовок даёт 400. Более ранний deadline не продлевается.

Показать подключение:

```go
entry := requesttimeout.Limit(5 * time.Second)(requesttimeout.Middleware(handler))
client := &http.Client{Transport: requesttimeout.Transport{Base: http.DefaultTransport}}
// В обработчике — обязательно входящий контекст.
req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
// Проверить err, затем client.Do(req); закрыть response.Body.
```

**Проговорить:** получили 5s → потратили 2s → client.Do вызвал RoundTrip → передали 3s.
Отдельный секундомер не нужен: конец бюджета хранится в ctx. `context.Background()` разорвёт передачу.
Handler и зависимости обязаны учитывать отмену; middleware сам не останавливает произвольный код и не пишет 504.

**Вопрос:** «А если часы downstream спешат на минуту?»
Передаём длительность 3s, не абсолютную дату. Получатель отсчитывает её своими часами — тот же принцип, что у gRPC.
Внутри процесса Go использует монотонное время. Доставка после вычисления остатка не вычитается у получателя; исходный клиент соблюдает свой deadline и отменяет вызов. Остановка всех сервисов не одновременная.

Показать `propagation_test.go`, затем запустить:

```bash
cd /Users/d.bulykin/Desktop/faut-tolerance-course/hotel/services/api-gateway
go test -v -count=1 ./internal/pkg/requesttimeout -run 'TestHTTPPropagationSpendsOneBudget|TestHTTPPropagationCancellationOverNetwork'
cd /Users/d.bulykin/Desktop/faut-tolerance-course/hotel
```

Первый тест: 2s локальной работы + 3s downstream = 5s на виртуальных часах. Второй: бюджет и отмена через реальное HTTP-соединение.
Это HTTP-аналогия: собственный формат заголовка и HTTP-коды. Рабочая цепочка стенда остаётся gRPC.

Переход к CB: «Ожидание ограничили. Но каждый следующий запрос снова идёт к неисправной зависимости. Поставим предохранитель».
