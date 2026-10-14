PROTO_IMAGE := hotelflow/proto-tools-grpc:1
PROTO_RUN = docker run --rm --user "$$(id -u):$$(id -g)" -e HOME=/tmp -v "$(abspath ..):/services" -w "/services/$(notdir $(CURDIR))" $(PROTO_IMAGE)

.PHONY: run test generate generate-local proto-tools
run:
	go run ./cmd
test:
	go test -race ./...
proto-tools:
	docker build -f build/Dockerfile.codegen -t $(PROTO_IMAGE) .
generate: proto-tools
	$(PROTO_RUN) buf format -w api
	$(PROTO_RUN) buf lint api
	$(PROTO_RUN) buf build
	$(PROTO_RUN) buf generate --template buf.gen.local.yaml
	$(PROTO_RUN) buf generate --template buf.gen.external.yaml

# Uses the same Buf templates with plugins already installed on PATH.
generate-local:
	buf format -w api
	buf lint api
	buf build
	buf generate --template buf.gen.local.yaml
	buf generate --template buf.gen.external.yaml
