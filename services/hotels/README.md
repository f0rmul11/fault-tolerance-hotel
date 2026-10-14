# Hotels

Каталог отелей и номеров. Own PostgreSQL `hotels`, отдельные сущности Hotel и Room. Четыре gRPC-метода: ListHotels, GetHotel, ListRooms, GetRoom. Публичный HTTP предоставляет Clay в API Gateway.

ListHotels/GetHotel обогащаются одним batch запросом Review; комнаты и min/max бронирования читаются без Review. Суммы звёзд и число оценок принадлежат Review, рейтинг устанавливается через Hotel.SetRating для ответа и не сохраняется в БД Hotels. Use case возвращает саму сущность; общий DTO — только Rating.

`make generate` выполняет Buf lint/build и генерацию собственных контрактов и внешнего клиента Review. `make test` запускает race-тесты. Миграции/seed: tools/migrations и tools/seed. Подробности: [каталог](../../docs/hotels-review.md).
