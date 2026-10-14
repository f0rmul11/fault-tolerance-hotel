# Ошибки и выдача наружу

## Точное соответствие образцу

В сервисах-владельцах копируй `error.go` и `option.go` из `/Users/d.bulykin/Desktop/fault-tolerant-stand/services/order-service/internal/pkg/ordererror`, меняя только package. Единственное расширение — необходимые значения `Code` в `types.go`. API Gateway использует [правила прокси](../../hotelflow-contracts-events/references/api-gateway.md) и не имеет собственного пакета ошибок.

Структура Error: закрытые `code Code`, `classification string`, `message string`, `base error`. Сигнатуры New/Option и всех конструкторов/методов сохраняются. Не добавляй Reason, BookingID, CheckinID, NewError, Normalize, defaults или новые поля/опции.

Базовые коды: External=0, NotFound=1, Conflict=2, FailedPrecondition=3, InvalidArgument=4, Internal=5. Расширения отражают различимые ситуации текущего сервиса; бизнес-коды не нужны каркасу без этих сценариев. Пользовательский текст всегда задаётся явно по-русски.

Семантика скопированного образца также сохраняется: WithBase принимает строку, Unwrap создаёт текстовую ошибку, BaseMessage требует ненулевой base. Не рассчитывай на errors.Is к исходной причине через этот Error и не вызывай BaseMessage вслепую. Для распознавания самого сервисного Error через внешнюю `%w` используй As/IsCode. Нетипизированные ошибки storage можно возвращать напрямую или с `%w`; context cancellation распознаётся транспортом до преобразования. Не добавляй повторное логирование LogWrap на каждом слое.

## Транспорт владельца

ErrorInterceptor находится в `internal/pkg/grpc/intercept` сервисов-владельцев и охватывает handler/auth ошибки. Он отображает внутренний Code в gRPC status, безопасное русское пояснение и машинный `x-error-code`. Число Code не является gRPC enum, прямой cast запрещён.

Исходящий gateway преобразует gRPC status и metadata в локальный Code. Уточняющий машинный код принимается только с ожидаемым gRPC status: Internal + INVALID_STATE не является доказанным бизнес-отказом. Не определяй причину по русскому тексту. Неизвестная техническая ошибка получает безопасный Internal; raw context.Canceled/DeadlineExceeded сохраняют семантику.

| Code / ситуация | gRPC | Публичный HTTP |
| --- | --- | --- |
| InvalidArgument, ошибка JSON | InvalidArgument | 422 |
| NotFound | NotFound | 404 |
| PermissionDenied | PermissionDenied | 403 |
| NewBusinessErr → FailedPrecondition | FailedPrecondition | 409 |
| Conflict, CAS | Aborted | 409 |
| Unavailable | Unavailable | 503 |
| DeadlineExceeded | DeadlineExceeded | 504 |
| Internal, External, DataIntegrity, OutcomeUnknown | Internal | 500 |

Явный External образца не переопределяет свою категорию из cause. Для распознанного downstream статуса исходящий адаптер сразу выбирает соответствующий Code. При потере ответа клиент повторяет создание с тем же idempotency_key. Сохраняемого UNCERTAIN нет.

Request/resource IDs принадлежат транспортным metadata, не Error. Create use case возвращает исходную ошибку; вместе с ней может вернуть объект попытки для известного ID. Handler помещает этот ID в `x-booking-id`, но не выдаёт объект как успешный результат. Не возвращай withID/error.go в use case. Полная потеря ответа может лишить клиента и metadata.

## HTTP и caller

API Gateway — Clay proxy со стандартным Status JSON, без Problem, gatewayerror и собственного business error mapper. Общие runtime options задают 422/409 и русские routing/parsing сообщения; подробности — в правилах шлюза.

Owner auth interceptor выбирает роли по generated full RPC method и доменным константам, проверяет request context перед handler. Use case не дублирует список ролей. Проверка владения конкретной бронью остаётся бизнес-сценарием. Gateway проверяет только роли и передаёт identity владельцу.

## Проверки

Тестируй поддержанные категории, внешний `%w`, неизвестные ошибки, raw отмену/deadline, безопасное сообщение, request ID и нужный resource ID в metadata. Проверь известный бизнес-код с правильным и неправильным gRPC status, отсутствие утечки SQL/адресов и HTTP 422/409. Не пиши тесты на nil-safe BaseMessage или сохранение identity исходной причины: это не поведение выбранного образца.

Неизвестный результат не превращается в подтверждённый отказ. Interceptor не изменяет состояние, не повторяет RPC и не выполняет компенсацию. Storage обрабатывает ожидаемые NotFound/CAS на месте, без универсального каталога SQLSTATE/сетевых ошибок.

Для нарушения бизнес-инварианта используй существующий NewBusinessErr: он возвращает FailedPrecondition (gRPC FailedPrecondition, HTTP 409). Новый Code Business не вводится. InvalidArgument (HTTP 422) — некорректные входные данные, Conflict (gRPC Aborted, HTTP 409) — CAS и конфликт ключа идемпотентности. Недопустимый статус, время заселения, длительность по условиям номера и пересечение брони — NewBusinessErr с явным русским пояснением. Не добавляй отдельные RoomUnavailable/DurationInvalid вместо этого конструктора. API Gateway передаёт ошибку владельца без своего бизнес-пакета.
