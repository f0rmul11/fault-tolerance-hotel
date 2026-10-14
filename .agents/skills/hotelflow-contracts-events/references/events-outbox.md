# События и outbox

## Обязательный образец

Источник — `/Users/d.bulykin/Desktop/fault-tolerant-stand/services/order-service` (ветка `module-4/async`). Используй существующие `internal/pkg/{event,pipe,outbox,worker}`, connectors и messagebus producer/handler; копируй только необходимые зависимости и адаптируй module paths. Самодельная замена outbox/relay или API `Enqueue([]outbox.Message)` не допускается. Не подключай метрики: прежний запрос пользователя остаётся в силе.

Исходящие события находятся в `internal/events/<событие>`, envelope `Base[T]` — в `internal/events`. JSON-теги принадлежат event envelope/payload; бизнес-DTO и entity их не содержат. Domain не импортирует events или pkg/event. Направление зависимостей: use case → converter → domain; общий events не импортирует дочерние события.

Конвертер имеет **именно** форму `New(entity) pipe.Func[event.Events]`: callback принимает context и batch, создаёт `events.Base[Payload]`, сериализует тело, формирует headers и добавляет `event.Event`. Не возвращай из `New` готовый outbox.Message и не передавай ID события из use case вместо этого подхода.

```go
func New(booking *entity.Booking) pipe.Func[event.Events] {
    return func(ctx context.Context, batch event.Events) (event.Events, error) {
        baseEvent := events.Base[BookingConfirmedEvent]{
            EventType: "booking.confirmed.v1",
            EntityID:  booking.ID().String(),
            Payload: BookingConfirmedEvent{
                ID:     booking.ID().String(),
                Status: string(booking.Status()),
                // Остальные поля snapshot из getters сущности.
            },
            // Остальные обязательные поля envelope HotelFlow.
        }

        body, err := json.Marshal(baseEvent)
        if err != nil {
            return nil, err
        }

        correlationID, err := uuid.NewV7()
        if err != nil {
            return nil, err
        }

        headersRaw, err := json.Marshal(map[string]string{
            "x-correlation-id": correlationID.String(),
            "x-app-name":       "booking",
            "x-event-type":     "booking.confirmed.v1",
        })
        if err != nil {
            return nil, err
        }

        return append(batch, event.Event{
            EntityID: booking.ID().String(),
            Key:      event.Raw(booking.ID().String()),
            Body:     body,
            Headers:  headersRaw,
            Schema:   config.BookingEventsTopic,
        }), nil
    }
}
```

Это форма построения, а не полный wire-контракт. Реализация заполняет **все** поля текущего AsyncAPI: `correlation_id`, `event_type`, `aggregate_id`, `aggregate_version`, `occurred_at`, `request_id`, `payload`. Go-поле EntityID сериализуется как aggregate_id для HotelFlow. Один correlation-id создаётся при формировании события и хранится в body и x-correlation-id; повторная доставка сохранённой строки сохраняет этот ID. Он является ключом идемпотентности события. Request ID наследуется от запроса. Имена топиков задаются константами config; значения полей не заменяются названиями из order-service.

## Сбор и сохранение

Use case или прикладной сервис принимает зависимость `flusher event.Flusher` через конструктор и создаёт request-scoped `event.Buffer` через `event.New(flusher)` либо `event.WithContext`. Из init/registry передаётся сам Outbox, без анонимной функции-обёртки над Flush. После успешного доменного перехода добавляет конвертер:

```go
buffer := event.New(outbox)
buffer.Add(bookingconfirmed.New(booking))

err := transaction.Exec(ctx, func(txCtx context.Context) error {
    if err := storage.Update(txCtx, booking); err != nil {
        return err
    }
    return buffer.Flush(txCtx)
})
```

Сохранение сущности и Flush используют один txCtx собственной БД. Flush записывает сообщения в outbox, а не вызывает Kafka. После rollback экземпляр сущности и буфер не продолжают использовать как сохранённые. Сущность после добавления callback не меняется до Flush; проверяй snapshot и итоговую версию тестом.

Не добавляй преобразования через общие DTO с json-тегами: отдельный event payload строится из entity. Методы именованных event-типов и общие функции сериализации headers допустимы, если убирают повторение, но сохраняют этот способ сборки.

## Копирование пакетов и relay

Сохраняй API и устройство образца: `NewOutbox`, `RegisterHandler`, `Flush`, `HandlePendingMessages`, `outbox/storage`, `outbox/message`, topic handler, producer, worker. Регистрация и запуск располагаются в отдельных init-методах composition root. Не подменяй их альтернативным Store/Relay.

Допустимы локальные исправления доказанных дефектов: буфер не очищается до успешного Flush, worker корректно останавливается без гонки/deadlock, ошибки commit не теряются. Не переписывай алгоритм ради другой архитектуры. Скопированные зависимости не создают циклы.

Relay **может держать собственную транзакцию БД во время отправки в Kafka**, как потребовал пользователь. Исключение не относится к бизнес-транзакциям use case. Успешные записи получают processed_at; ошибочные остаются pending с диагностикой. Потеря подтверждения Kafka или ошибка commit после send допускает дубль: это at-least-once, не exactly-once. Счётчик ошибок не является DLQ и не гарантирует остановку. Порядок между чанками/версиями и partitioner описывай по фактической конфигурации, а не по наличию key.

DDL адаптируется под уже принятое правило: PK/UNIQUE/index разрешены, явный NOT NULL/FK/CHECK запрещены. На текущем этапе пользователь разрешил inplace без совместимости: исходная миграция создаёт только актуальную схему outbox. Не добавляй legacy-таблицы и handlers старых событий. Генерация кода не сбрасывает БД автоматически.

Проверяй rollback бизнес-данных вместе с outbox, неуспешный Flush, headers/body/topic, отправку и processed_at, повтор после ошибки и остановку worker. Domain/use case не получают Kafka producer.

## Входящие события и request/reply

Booking владеет бронью, её статусом и занятостью номера. Check-in владеет сущностью `CheckIn`: ожидаемым прибытием и его оформлением. Связь — `booking_id`; сущности Booking/Operation, копии статусов/версий Booking и сервисы `booking/request` в Check-in не нужны. Данные о госте, номере и периоде поступают из события создания для оформления заселения. Сервисы не читают чужую БД. Подробности — [Check-in](../../../../docs/check-in.md).

- Каждый RPC реализуется в отдельном файле `internal/app/<область>/v1/<метод>.go`. `service.go` содержит структуру и конструктор с конкретным `*usecase.Registry`. Не объявляй интерфейсы use case на уровне gRPC-хендлеров. В API Gateway остаются generated downstream-клиенты.
- UUID и response mapping находятся в самом методе, как в ad-service: `uuid.FromStringOrNil`, `lo.Map` либо простой цикл. Отдельные `parseID`, `bookingView`, `operationView`, `response.go` и универсальные мапперы не нужны. Обязательные ID проверяет use case/сущность; общую identity и роли — interceptor.
- Один use case на RPC **и один use case на сценарий входящего события**. Kafka-хендлеры вызывают именно use case. Вход — локальный `types.go`; optional поля — `mo.Option[T]`. Не создавай дополнительные use case для отдельных шагов одного сценария.
- `booking.confirmed.v1` регистрирует ожидаемое заселение. Check-in отправляет `checkin.arrival-requested.v1` или `checkin.cancellation-requested.v1`. Booking отвечает `booking.activated.v1` / `booking.canceled.v1` (пакеты `booking_activated` / `booking_canceled`); для бизнес-отказа — `booking.checkin-rejected.v1`. Обобщённого changed нет.
- **Ключ идемпотентности события — `correlation-id`**: один UUID создаётся при формировании события до записи outbox, сохраняется в `x-correlation-id` и `correlation_id` envelope. Relay при повторе не создаёт новый ID. Ответ — самостоятельное событие со своим ID; `causation_id` указывает ID запроса. HTTP `request_id` служит трассировке. Ни event_id, ни Kafka offset, ни request_id не подменяют ключ события.
- Подписчик проверяет заголовок и envelope, сохраняет inbox с этим correlation-id и только затем подтверждает offset. В inbox уникален `(correlation_id, topic)`. Реализация пакета inbox — копия payment-service с адаптацией импортов/форматирования; уникальность ключа усиливает DDL, не меняя алгоритм копии.
- Booking сохраняет `processed_events.correlation_id`, CAS-переход и reply outbox одной транзакцией. Повтор после commit до отметки inbox не выполняет переход и не создаёт второй reply. Техническая ошибка откатывает всё и возвращается inbox; доменный отказ получает отдельный reply.
- Check-in сохраняет HTTP-ключ `(actor_id, key)`, своё состояние и outbox одной транзакцией. `insert ... on conflict` и CAS обеспечивают конкуренцию без явных advisory/row locks в этом сценарии. Таблица requests — технические ключи, без отдельной сущности/жизненного цикла Operation. Повтор того же ключа и booking_id/action возвращает текущее состояние того же CheckIn, другой состав — Conflict.
- CheckIn: EXPECTED → ARRIVING → CHECKED_IN; отмена EXPECTED/CHECKED_IN → CANCELING → CANCELED. Ошибка Booking возвращает предыдущее состояние и русское пояснение. Ответ применяется только к ожидаемой версии собственного CheckIn; поздний ответ не меняет новое состояние. Это корреляция ответа и защита от устаревшего действия, не второй ключ дедупликации события.
- Booking: CONFIRMED → ACTIVE, отмена CONFIRMED/ACTIVE → CANCELED. Активация проверяет время исходного действия в пределах периода; задержка Kafka не меняет решение. Только CANCELED исключается из EXCLUDE и запроса занятости.
- Список Check-in: starts_at в `[from,to)` и статус EXPECTED/ARRIVING. По умолчанию — текущие сутки Europe/Moscow из конфига; CHECKED_IN/CANCELING/CANCELED и заезды с началом вчера не входят.
- Kafka key = booking_id, hash partitioner, раздельные consumer groups, чтение новых групп с oldest. Normal/error workers могут пересекаться: правильность обеспечивает идемпотентность и CAS. Доставка at-least-once; inbox не обещает exactly-once, счётчик ошибок не является DLQ или пределом повторов. Метрики копии не подключаются.

## Notification

Notification подписан на booking.events.v1 отдельной consumer group notification-booking-events. Он обрабатывает booking.confirmed.v1, booking.activated.v1 и booking.canceled.v1; другие корректные события пропускает. Путь: Kafka subscriber → event router → отдельный use case события → infrastructure/gateway/push. Gateway-заглушка выводит структурированный JSON в stdout с русским заголовком и текстом, booking_id и correlation_id. Внешнего SMS/push-провайдера и регистрации устройств пока нет.

По решению пользователя здесь **нет inbox, БД, outbox, проекции и дедупликации**. Скопированный Kafka consumer подтверждает offset после успешного handler. Повторная доставка может повторить лог с тем же correlation-id; exactly-once не обещается. Новая consumer group читает с oldest. Ошибка handler не подтверждает сообщение; некорректное сообщение требует исправления/ручного решения, DLQ и пропуск ошибок автоматически не добавляются. Из RPC есть только Echo, также сохранены служебные healthcheck/reflection.
