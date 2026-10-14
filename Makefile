SERVICES := $(notdir $(patsubst %/go.mod,%,$(wildcard services/*/go.mod)))

.PHONY: up build stop status smoke smoke-business reset doctor test generate $(addprefix test-,$(SERVICES)) $(addprefix generate-,$(SERVICES))
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
smoke-business:
	go run ./hotelctl/cmd/hotelctl smoke --business
reset:
	go run ./hotelctl/cmd/hotelctl reset --yes
doctor:
	go run ./hotelctl/cmd/hotelctl doctor
test: $(addprefix test-,$(SERVICES))
	go test -race ./hotelctl/...
$(addprefix test-,$(SERVICES)): test-%:
	$(MAKE) -C services/$* test
generate: $(addprefix generate-,$(SERVICES))
$(addprefix generate-,$(SERVICES)): generate-%:
	$(MAKE) -C services/$* generate

# Service-owned contracts; generated output is checked in.
.PHONY: booking-tools generate-booking-api xo-booking
booking-tools:
	bash tools/install-xo.sh
	GOWORK=off go install github.com/pressly/goose/v3/cmd/goose@v3.26.0
generate-booking-api:
	$(MAKE) -C services/review generate
	$(MAKE) -C services/hotels generate
	$(MAKE) -C services/booking generate
	$(MAKE) -C services/check-in generate
	$(MAKE) -C services/api-gateway generate
xo-booking:
	bash tools/generate-xo.sh booking
.PHONY: migrate-booking
migrate-booking:
	@test -n "$$POSTGRES_DSN" || (echo 'Set POSTGRES_DSN for booking' >&2; exit 1)
	goose -dir services/booking/tools/migrations postgres "$$POSTGRES_DSN" up
.PHONY: xo-hotels migrate-hotels
xo-hotels:
	bash tools/generate-xo.sh hotels
migrate-hotels:
	@test -n "$$POSTGRES_DSN" || (echo 'Set POSTGRES_DSN for hotels' >&2; exit 1)
	goose -dir services/hotels/tools/migrations postgres "$$POSTGRES_DSN" up

.PHONY: xo-check-in migrate-check-in
xo-check-in:
	bash tools/generate-xo.sh check-in
migrate-check-in:
	@test -n "$$POSTGRES_DSN" || (echo 'Set POSTGRES_DSN for check-in' >&2; exit 1)
	goose -dir services/check-in/tools/migrations postgres "$$POSTGRES_DSN" up

.PHONY: xo-review migrate-review
xo-review:
	bash tools/generate-xo.sh review
migrate-review:
	@test -n "$$POSTGRES_DSN" || (echo 'Set POSTGRES_DSN for review' >&2; exit 1)
	goose -dir services/review/tools/migrations postgres "$$POSTGRES_DSN" up

# AKHQ is deployed beside Kafka; the local tunnel avoids restarting the k3s node.
HOTEL_KUBECONFIG ?= .local/hotelctl/kubeconfig
.PHONY: kafka-ui kafka-ui-deploy
kafka-ui-deploy:
	kubectl --kubeconfig "$(HOTEL_KUBECONFIG)" -n hotelflow apply -f deploy/booking-dependencies/akhq.yaml
	kubectl --kubeconfig "$(HOTEL_KUBECONFIG)" -n hotelflow rollout status deployment/akhq --timeout=180s
kafka-ui: kafka-ui-deploy
	kubectl --kubeconfig "$(HOTEL_KUBECONFIG)" -n hotelflow port-forward --address 127.0.0.1 service/akhq 8090:8080
