# Серверный token bucket

`NewLimiter(Config)` и `UnaryServerInterceptor()` сохраняют способ конструирования исходного пакета ad-service.
Алгоритм — `golang.org/x/time/rate` (официальный дополнительный модуль Go, не пакет стандартной библиотеки).
Конструктор принимает конфигурацию без отдельного валидатора. Передавайте конечный
неотрицательный limit, неотрицательные burst и timeout; ceil(limit) должен помещаться в int.
В конфигурации стенда включены limit: 10, burst: 200, timeout: 0s для ListArrivals.

## Семантика правил

- Первое совпадение `clients` и полного gRPC method выигрывает. Пустой список внутри правила — wildcard.
- Один bucket на правило, общий для перечисленных клиентов/методов; отдельный bucket на каждую реплику.
- Пустые `rules` — bypass. Клиент, не совпавший ни с одним правилом, также проходит без ограничения.
- `limit` — токены/секунду, включая дробные значения; `burst` — максимальная ёмкость.
  При `burst: 0` используется `ceil(limit)`. Новый процесс начинает с полного bucket.
- `limit: 0` запрещает все совпавшие запросы даже при положительном burst.
- `timeout: 0` использует немедленный `Allow`, `timeout > 0` — `Wait` в пределах локального и входящего бюджета.
  Ожидание не ограничивает количество ожидающих goroutines: в демо используется fail-fast, а для ограничения
  параллелизма нужна отдельная bulkhead-защита. Отменённые ожидания освобождают reservations средствами x/time/rate.
- Исчерпание локального бюджета ожидания/невозможность получить токен в срок — `ResourceExhausted`.
  Уже отменённый/истёкший входящий context сохраняет `Canceled`/`DeadlineExceeded`.
- `x-client-name` задаётся исходящим interceptor Gateway. Это identity сервиса в доверенном учебном кластере,
  не защита от злонамеренного внутреннего клиента; для неё нужна аутентификация транспорта.

## Ответы

Заголовки выставляются на каждый совпавший запрос до handler — включая успешный допуск.
Gateway проксирует их и сохраняет HTTP 429 для gRPC ResourceExhausted.

| Header | Значение |
| --- | --- |
| X-RateLimit-Limit | Скорость пополнения, токены/секунду |
| X-RateLimit-Burst | Ёмкость bucket |
| X-RateLimit-Remaining | Целая неотрицательная часть текущего баланса |
| X-RateLimit-Reset | Unix timestamp прогнозируемого полного наполнения без новых запросов |
| Retry-After | Только на отказе: ceil времени до следующего доступного токена, минимум 1 секунда |

У token bucket нет фиксированного окна; Reset и Retry-After не обозначают одно и то же.
Это рекомендации, а не резервирование токена за клиентом. При нулевой скорости конечного восстановления нет:
Reset и Retry-After отсутствуют. Непредставимые конечным int64 интервалы также не публикуются.
Чтение баланса для headers не использует Reserve/Cancel и не меняет очередь ожидающих reservations.

## Метрики

`RegisterMetrics(prometheus.DefaultRegisterer)` вызывается в `initRateLimiter`; существующий OTLP bridge
собирает collector каждые 3s. Отдельный exporter, HTTP endpoint и фоновые goroutines не создаются.

- `ratelimit_decisions_total{rule,outcome}`: allowed/rejected/canceled. Allowed — допуск к handler,
  не успех бизнес-операции. Canceled включает истёкший входящий deadline до handler.
- `ratelimit_tokens{rule}`: актуальный баланс при сборе, включая idle refill. Отрицательное значение
  возможно при ожидающих reservations в режиме timeout > 0.
- `ratelimit_limit{rule}`, `ratelimit_burst{rule}`: параметры bucket.
- `ratelimit_wait_duration_seconds{rule}`: histogram ожидания токена при timeout > 0.
  При fail-fast нет наблюдений и p95 не определён.

Rule — индекс с нуля; labels не зависят от пользовательского ввода.
Непопавшие в правила запросы не учитываются в этих метриках, но остаются в обычных server metrics.
В Service Overview добавлена секция Rate limiter с rate[15s] и балансом токенов.

## Учебные реализации

В [algorithms](algorithms/) находятся отдельные пакеты tokenbucket,
leakybucket, fixedwindow и slidingwindowcounter с русскими комментариями
и табличными тестами. Они входят в модуль Check-in и проверяются `make test`.
Серверный interceptor использует x/time/rate, а не эти учебные реализации.
