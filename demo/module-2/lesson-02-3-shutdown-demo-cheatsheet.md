# Урок 2 · демо 3: graceful shutdown

**Показываем:** RollingUpdate сохраняет реплики, но без shutdown активные запросы обрываются. Исправление: снять readiness → пауза 3 с → завершить активные запросы.

## 1. Подготовить плохой вариант

```bash
cd /Users/d.bulykin/Desktop/faut-tolerance-course/hotel
git switch module-2/demo
```

В `services/hotels/internal/init.go`, **только внутри `initMainServer`**, удалить целиком callback `a.publicCloser.Add(func() error { … })`, содержащий `a.mainServer.Stop(gracefulCtx)`: от регистрации callback до закрывающего `})`. Вместе с ним удаляются пауза и ожидание завершения запросов. Удалить неиспользуемый импорт `"time"`.

**В ветке демо этот блок уже удалён.** Проверку readiness и остальные closers не менять.

```bash
gofmt -w services/hotels/internal/init.go
./bin/hotelctl build hotels
./bin/hotelctl deploy hotels
./bin/hotelctl status hotels
```

Перед показом: Hotels **3/3 Ready**, RollingUpdate `maxUnavailable: 0`, `maxSurge: 1`; Review и etcd доступны. `preStop` не нужен.

## 2. Создать долгие запросы · терминал 1

```bash
./bin/hotelctl fault hotels review \
  --direction response --slowdown 3s --duration 3m
```

Дождаться `ACTIVE`. Hotels будет ждать ответ Review ещё 3 секунды.

## 3. Дать нагрузку · терминал 2

```bash
./bin/hotelctl load \
  --addr 127.0.0.1:9090 \
  --method api_gateway.hotels.v1.HotelsService/GetHotel \
  --data '{"hotel_id":"cdd4685e-c610-4e7e-95ee-c51fb6807400"}' \
  --rps 20 --duration 70s
```

Подождать **15 секунд**: запросы медленные, но успешные. Если ошибки есть до rollout — сначала устранить их.

## 4. Перезапустить Hotels · терминал 3

```bash
./bin/hotelctl kubectl -n hotelflow rollout restart deployment/hotels
./bin/hotelctl status hotels --watch
```

Через `kubectl`, потому что активный `fault` удерживает блокировку обычной команды `hotelctl rollout restart`.

**Показать:** при остановке старых pod появляются ошибки, в том числе `Canceled`. Новая реплика не может закончить запрос старой.

## 5. Закончить прогон

Дождаться итоговой статистики нагрузки. Остановить watch и fault через **Ctrl+C** в их терминалах; дождаться снятия fault.

```bash
./bin/hotelctl fault hotels review --clear
```

Сохранить число ошибок. Сборку и деплой делать только после остановки fault.

## 6. Вернуть исправленный shutdown

```bash
git show master:services/hotels/internal/init.go
```

В редакторе скопировать из master **только функцию `initMainServer` целиком**, вернуть импорт `"time"`. Остальной файл не заменять — иначе изменится readiness из второго демо.

Вернувшийся callback выполняет:

```go
atomic.StoreInt32(&a.terminated, 1)
gracefulCtx, cancel := context.WithTimeout(context.Background(), config.Instance().Graceful.Timeout)
defer cancel()
// Сервер пока продолжает принимать запросы.
time.Sleep(config.Instance().Graceful.Delay)
// Далее — существующий блок Stop и ожидания завершения.
```

**Вернуть весь callback, а не только этот фрагмент.** В `config/config.yaml`: `graceful.delay: 3s`, `graceful.timeout: 10s`; окружение — `GRACEFUL_DELAY`, `GRACEFUL_TIMEOUT`. Пауза входит в 10 секунд; Kubernetes даёт 30 секунд на завершение pod.

```bash
gofmt -w services/hotels/internal/init.go
./bin/hotelctl build hotels
./bin/hotelctl deploy hotels
./bin/hotelctl status hotels
```

Дождаться **3/3 Ready и удаления старых pod**.

## 7. Сравнить

Повторить шаги **2–5** с теми же параметрами. Ожидаем **0 ошибок**: пауза даёт маршрутизации обновиться, затем сервер заканчивает активные запросы.

Проверено на стенде с паузой в коде, без `preStop`: **два rollout Hotels по 1400 запросов — 0 ошибок**, задержка Review 3 с, нагрузка 20 RPS.

После показа: fault снят, Hotels 3/3 Ready. Исправленный код оставить локально; не коммитить его поверх намеренно плохой версии демо.
