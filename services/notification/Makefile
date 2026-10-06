.PHONY: run test generate proto-deps proto-tools
run:
	go run ./cmd
test:
	go test -race ./...
proto-tools:
	docker build -f build/Dockerfile.codegen -t hotelflow/proto-tools-grpc:1 .
proto-deps: proto-tools
	docker run --rm --user "$$(id -u):$$(id -g)" -e HOME=/tmp -v "$(CURDIR):/workspace" hotelflow/proto-tools-grpc:1 buf dep update
generate: proto-tools
	docker run --rm --user "$$(id -u):$$(id -g)" -e HOME=/tmp -v "$(CURDIR):/workspace" hotelflow/proto-tools-grpc:1 buf lint
	docker run --rm --user "$$(id -u):$$(id -g)" -e HOME=/tmp -v "$(CURDIR):/workspace" hotelflow/proto-tools-grpc:1 buf build
	docker run --rm --user "$$(id -u):$$(id -g)" -e HOME=/tmp -v "$(CURDIR):/workspace" hotelflow/proto-tools-grpc:1 buf generate --template buf.gen.local.yaml
