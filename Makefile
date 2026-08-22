.PHONY: build test test-race vet lint mocks generate run clean

BINARY := forge
MOCKERY ?= $(shell which mockery 2>/dev/null || echo $(HOME)/Go/bin/mockery)

build:
	go build -o bin/$(BINARY) ./app

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

generate:
	$(MOCKERY)

mocks:
	$(MOCKERY)

run:
	go run ./app

clean:
	rm -rf bin/