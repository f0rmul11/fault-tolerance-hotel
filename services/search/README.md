# search

Фундамент сервиса на Clay: gRPC Echo, reflection, admin healthcheck и graceful shutdown. Хранилищ нет.

Из корня проекта:

```bash
go run ./hotelctl/cmd/hotelctl up
./bin/hotelctl build search
./bin/hotelctl deploy search
./bin/hotelctl logs search -f
make -C services/search test
make -C services/search generate
grpcui --plaintext 127.0.0.1:9096
```

- RPC: `search.echo.v1.EchoService/Echo`, запрос `{"message":"Hello"}`.
- Ответ: `{"message":"Hello","service":"search"}`.
- В кластере: `search.hotelflow.svc.cluster.local:9090`.
- С компьютера: gRPC `127.0.0.1:9096`, admin `http://127.0.0.1:8087/healthcheck/{live,ready,startup}`.
- Admin запускается раньше основного сервера и доступен при неготовом Pod.
