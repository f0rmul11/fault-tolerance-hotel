PROTO_IMAGE := hotelflow/proto-tools:1
PROTO_RUN = docker run --rm --user "$$(id -u):$$(id -g)" -e HOME=/tmp -v "$(abspath ..):/services" -w "/services/$(notdir $(CURDIR))" $(PROTO_IMAGE)

.PHONY: run test generate generate-local generate-offline proto-tools
run:
	go run ./cmd
test:
	go test -race ./...
proto-tools:
	docker build -f build/Dockerfile.codegen -t $(PROTO_IMAGE) .
generate: proto-tools
	$(PROTO_RUN) buf dep update
	$(PROTO_RUN) buf format -w api
	$(PROTO_RUN) buf lint api
	$(PROTO_RUN) buf build
	$(PROTO_RUN) buf generate --template buf.gen.local.yaml
	$(PROTO_RUN) buf generate --template buf.gen.external.yaml

# Uses the same Buf templates with plugins already installed on PATH.
generate-local:
	buf dep update
	buf format -w api
	buf lint api
	buf build
	buf generate --template buf.gen.local.yaml
	buf generate --template buf.gen.external.yaml

# Explicit cache outside the repository, for environments where BSR is unavailable.
generate-offline: proto-tools
	@test -n "$(PROTO_IMPORTS)" || (echo "Set PROTO_IMPORTS to an external protobuf imports directory" >&2; exit 1)
	docker run --rm --user "$$(id -u):$$(id -g)" -e HOME=/tmp \
		-v "$(abspath ..):/services" -v "$(abspath $(PROTO_IMPORTS)):/proto-imports:ro" \
		-w "/services/$(notdir $(CURDIR))" $(PROTO_IMAGE) sh tools/generate-offline.sh
