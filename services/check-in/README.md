# check-in

Фундамент сервиса на Clay: gRPC Echo, reflection, admin healthcheck и graceful shutdown. Хранилищ нет.

Из корня проекта:

```bash
go run ./hotelctl/cmd/hotelctl up
./bin/hotelctl build check-in
./bin/hotelctl deploy check-in
./bin/hotelctl logs check-in -f
make -C services/check-in test
make -C services/check-in generate
grpcui --plaintext 127.0.0.1:9093
```

- RPC: `check_in.echo.v1.EchoService/Echo`, запрос `{"message":"Hello"}`.
- Ответ: `{"message":"Hello","service":"check-in"}`.
- В кластере: `check-in.hotelflow.svc.cluster.local:9090`.
- С компьютера: gRPC `127.0.0.1:9093`, admin `http://127.0.0.1:8084/healthcheck/{live,ready,startup}`.
- Admin запускается раньше основного сервера и доступен при неготовом Pod.
