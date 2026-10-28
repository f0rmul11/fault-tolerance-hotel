# Hotels

Каталог отелей и номеров. Own PostgreSQL `hotels`, отдельные сущности Hotel и Room. Четыре gRPC-метода: ListHotels, GetHotel, ListRooms, GetRoom. Публичный HTTP предоставляет Clay в API Gateway.

ListHotels/GetHotel обогащаются одним batch запросом Review; комнаты и min/max бронирования читаются без Review. Суммы звёзд и число оценок принадлежат Review, рейтинг устанавливается через Hotel.SetRating для ответа и не сохраняется в БД Hotels. Use case возвращает саму сущность; общий DTO — только Rating.

`make generate` выполняет Buf lint/build и генерацию собственных контрактов и внешнего клиента Review. `make test` запускает race-тесты. Миграции/seed: tools/migrations и tools/seed.

Hotels разворачивается в **3 реплики**, RollingUpdate: `maxUnavailable: 0`, `maxSurge: 1`.
Readiness допускает Pod после первой успешной загрузки конфигурации из прикладного etcd.
При последующей потере etcd сохраняется последнее валидное состояние; startup и liveness
не зависят от синхронизации. Полученный из etcd false не делает Pod неготовым.

`ListRooms` проверяет `rooms_list_enabled` через внедрённый `*config.Client` перед чтением БД.
При false возвращается gRPC FailedPrecondition / HTTP 409 «Получение списка номеров отключено».
Default — false. Остальные RPC этим флагом не управляются; обращений в Review у ListRooms нет.

Из корня проекта:

```sh
./bin/hotelctl config set hotels rooms_list_enabled true
./bin/hotelctl config get hotels rooms_list_enabled
```

Успешный set подтверждает запись в etcd; каждый Pod применяет обновление в фоне.
Старый ключ hotels_list_enabled больше не используется. Значения из etcd автоматически не переименовываются.
