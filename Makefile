SERVICES := $(notdir $(patsubst %/go.mod,%,$(wildcard services/*/go.mod)))

.PHONY: up build stop status smoke doctor test generate $(addprefix test-,$(SERVICES)) $(addprefix generate-,$(SERVICES))
up:
	go run ./hotelctl/cmd/hotelctl up
build:
	go build -trimpath -o bin/hotelctl ./hotelctl/cmd/hotelctl
stop:
	go run ./hotelctl/cmd/hotelctl stop
status:
	go run ./hotelctl/cmd/hotelctl status
smoke:
	go run ./hotelctl/cmd/hotelctl smoke
doctor:
	go run ./hotelctl/cmd/hotelctl doctor
test: $(addprefix test-,$(SERVICES))
	go test -race ./hotelctl/...
$(addprefix test-,$(SERVICES)): test-%:
	$(MAKE) -C services/$* test
generate: $(addprefix generate-,$(SERVICES))
$(addprefix generate-,$(SERVICES)): generate-%:
	$(MAKE) -C services/$* generate
