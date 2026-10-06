# availability

Фундамент сервиса на Clay: gRPC Echo, reflection, admin healthcheck и graceful shutdown. Хранилищ нет.

Из корня проекта:

```bash
go run ./hotelctl/cmd/hotelctl up
./bin/hotelctl build availability
./bin/hotelctl deploy availability
./bin/hotelctl logs availability -f
make -C services/availability test
make -C services/availability generate
grpcui --plaintext 127.0.0.1:9091
```

- RPC: `availability.echo.v1.EchoService/Echo`, запрос `{"message":"Hello"}`.
- Ответ: `{"message":"Hello","service":"availability"}`.
- В кластере: `availability.hotelflow.svc.cluster.local:9090`.
- С компьютера: gRPC `127.0.0.1:9091`, admin `http://127.0.0.1:8082/healthcheck/{live,ready,startup}`.
- Admin запускается раньше основного сервера и доступен при неготовом Pod.
