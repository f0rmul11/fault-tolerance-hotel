.PHONY: up stop status smoke kubectl test generate
up:
	./hotelctl up
stop:
	./hotelctl stop
status:
	./hotelctl status
smoke:
	./hotelctl smoke
test:
	$(MAKE) -C services/api-gateway test
generate:
	$(MAKE) -C services/api-gateway generate
