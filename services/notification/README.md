# notification

Фундамент сервиса на Clay: gRPC Echo, reflection, admin healthcheck и graceful shutdown. Хранилищ нет.

Из корня проекта:

```bash
go run ./hotelctl/cmd/hotelctl up
./bin/hotelctl build notification
./bin/hotelctl deploy notification
./bin/hotelctl logs notification -f
make -C services/notification test
make -C services/notification generate
grpcui --plaintext 127.0.0.1:9095
```

- RPC: `notification.echo.v1.EchoService/Echo`, запрос `{"message":"Hello"}`.
- Ответ: `{"message":"Hello","service":"notification"}`.
- В кластере: `notification.hotelflow.svc.cluster.local:9090`.
- С компьютера: gRPC `127.0.0.1:9095`, admin `http://127.0.0.1:8086/healthcheck/{live,ready,startup}`.
- Admin запускается раньше основного сервера и доступен при неготовом Pod.
