# booking

Фундамент сервиса на Clay: gRPC Echo, reflection, admin healthcheck и graceful shutdown. Хранилищ нет.

Из корня проекта:

```bash
go run ./hotelctl/cmd/hotelctl up
./bin/hotelctl build booking
./bin/hotelctl deploy booking
./bin/hotelctl logs booking -f
make -C services/booking test
make -C services/booking generate
grpcui --plaintext 127.0.0.1:9092
```

- RPC: `booking.echo.v1.EchoService/Echo`, запрос `{"message":"Hello"}`.
- Ответ: `{"message":"Hello","service":"booking"}`.
- В кластере: `booking.hotelflow.svc.cluster.local:9090`.
- С компьютера: gRPC `127.0.0.1:9092`, admin `http://127.0.0.1:8083/healthcheck/{live,ready,startup}`.
- Admin запускается раньше основного сервера и доступен при неготовом Pod.
