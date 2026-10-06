# room

Фундамент сервиса на Clay: gRPC Echo, reflection, admin healthcheck и graceful shutdown. Хранилищ нет.

Из корня проекта:

```bash
go run ./hotelctl/cmd/hotelctl up
./bin/hotelctl build room
./bin/hotelctl deploy room
./bin/hotelctl logs room -f
make -C services/room test
make -C services/room generate
grpcui --plaintext 127.0.0.1:9094
```

- RPC: `room.echo.v1.EchoService/Echo`, запрос `{"message":"Hello"}`.
- Ответ: `{"message":"Hello","service":"room"}`.
- В кластере: `room.hotelflow.svc.cluster.local:9090`.
- С компьютера: gRPC `127.0.0.1:9094`, admin `http://127.0.0.1:8085/healthcheck/{live,ready,startup}`.
- Admin запускается раньше основного сервера и доступен при неготовом Pod.
