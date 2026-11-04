# HTTP timeout propagation

Учебное соглашение: `X-Request-Timeout: 3s` передаёт **оставшийся** бюджет.
Это не стандартный HTTP-заголовок. Входящий `Middleware` уже используется Gateway;
исходящий `Transport` подключается явно и не меняет рабочие gRPC-соединения.

```go
// Один client на приложение. Base можно заменить своим транспортом.
client := &http.Client{Transport: requesttimeout.Transport{Base: http.DefaultTransport}}

// На внутреннем принимающем HTTP-сервисе — только перенос бюджета:
handler := requesttimeout.Middleware(http.HandlerFunc(handle))

// Только на входе Gateway — дополнительно локальная политика 5 секунд:
entry := requesttimeout.Limit(5 * time.Second)(handler)

// В handle: передаём контекст входящего запроса, а не context.Background().
request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, downstreamURL, nil)
if err != nil {
    // Вернуть ошибку принятой в сервисе схемой.
    return
}
response, err := client.Do(request)
if err != nil {
    // DeadlineExceeded отображаем в 504; отмену вызывающей стороны различаем отдельно.
    return
}
defer response.Body.Close()
```

Путь бюджета: вход → deadline через 5s → локальная работа 2s → Transport отправляет
`X-Request-Timeout: 3s` → downstream Middleware создаёт контекст максимум на 3s.
Существующий более ранний deadline никогда не продлевается. На следующем HTTP-переходе
Transport снова вычисляет остаток. Уже истёкший контекст не отправляется в сеть.

`Limit(5s)` и `Middleware` имеют разные роли:

- `Limit` устанавливает локальный максимум; уже более ранний deadline сохраняется.
- `Middleware` без заголовка оставляет контекст без изменений — новый timeout не появляется.
- Положительный бюджет превращается в deadline без ограничения 5 секундами.
- `0s` создаёт сразу истёкший контекст; обработчик не получает новый бюджет.
- Некорректный, отрицательный, пустой, переполненный или повторный заголовок отклоняется HTTP 400 до handler.

На входе Gateway композиция ограничивает и бюджет `20s` пятью секундами; внутренний
сервис с одним Middleware примет `20s`. Это политика точки входа, не свойство propagation.
Transport заменяет старый заголовок остатком из контекста; без deadline удаляет его.
Исходный запрос и его заголовки не изменяются.

Handler обязан реагировать на `ctx.Done()` и передавать ctx в БД/следующие вызовы.
Middleware не останавливает произвольный код принудительно и сам не пишет 504.

Относительный бюджет не требует синхронных часов, но время доставки заголовка не
вычитается на получателе. Исходный клиент всё равно ограничен своим deadline;
отмена распространяется через транспорт, с задержкой сети/прокси. Это не гарантия
одновременной остановки всех серверов. Клиент предназначен для доверенных внутренних
связей; при redirects на другие хосты отдельно задаётся политика переноса заголовков.

## Показать на уроке

Из `services/api-gateway`:

```bash
go test -v -count=1 ./internal/pkg/requesttimeout -run 'TestHTTPPropagation'
```

`TestHTTPPropagationSpendsOneBudget` показывает 5s → 2s работы → 3s downstream:
виртуальное время synctest и отдельный серверный контекст, восстановленный из заголовка.
`TestHTTPPropagationCancellationOverNetwork` проверяет передачу бюджета и отмену
через настоящий HTTP с `httptest.Server`.

Семантика положительного/нулевого/отсутствующего бюджета соответствует подходу grpc-go.
Wire-совместимости с gRPC нет: используется Go duration вместо восьмизначного числа
с единицей `H/M/S/m/u/n`, а неправильный заголовок даёт HTTP 400 вместо gRPC INTERNAL.
Клиентская ошибка deadline и серверный HTTP-ответ — разные события: клиент может
уже прекратить ожидание, когда сервер пишет 504.

Сначала показать `Limit` и `Middleware` в `middleware.go`, затем `transport.go`, затем тесты.
Документация: [Go RoundTripper](https://pkg.go.dev/net/http#RoundTripper),
[gRPC deadlines](https://grpc.io/docs/guides/deadlines/).
