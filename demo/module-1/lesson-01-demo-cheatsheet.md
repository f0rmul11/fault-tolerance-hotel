# Урок 1 — команды для демо

```bash
cd /Users/d.bulykin/Desktop/faut-tolerance-course/hotel
```

Запросы отправлять из Swagger после `ACTIVE`. `fault` снимается автоматически или по Ctrl+C.

Трейс запроса: скопировать `x-trace-id` из Response headers → открыть `http://localhost:16686/trace/<x-trace-id>`. Заголовок есть и при ошибке; экспорт трейса может занять несколько секунд.

## 1. Таймаут, но бронь создана

```bash
./bin/hotelctl fault api-gateway booking --direction response --slowdown 8s --duration 10s
```

Создать бронь → **504 через 5 с**, бронь сохранена. После снятия сбоя повторить запрос с тем же гостем, телом и `idempotency_key` → **200, та же бронь**. Показать трейс в Jaeger.

Для сравнения — задержка запроса: **504, новая бронь не создана**.

```bash
./bin/hotelctl fault api-gateway booking --slowdown 8s --duration 10s
```

## 2. Недоступен рейтинг

```bash
./bin/hotelctl fault hotels review --unavailable --duration 10s
```

GetHotel / непустой ListHotels → **503**. GetRoom → **200**. После снятия сбоя GetHotel снова работает.

## 3. Бронь есть, на ресепшене её нет

```bash
./bin/hotelctl stop kafka
```

Создать новую бронь → ожидается **200**, событие ждёт в outbox, брони нет в ListArrivals. В ListArrivals выбрать период, включающий начало брони.

```bash
./bin/hotelctl start kafka
```

Дождаться доставки: бронь появляется в Check-in. **Kafka сама не запустится.** Сценарий ещё нужно прогнать перед уроком.

## Если нужно снять всё вручную

```bash
./bin/hotelctl fault api-gateway booking --clear
./bin/hotelctl fault hotels review --clear
./bin/hotelctl start kafka
```

[Swagger](http://localhost:8080/docs/) · [Jaeger](http://localhost:16686) · [Grafana](http://localhost:3000) · [Kafka UI](http://localhost:8090)
