GO ?= go
BIN := bin/sitka

.PHONY: build test lint fmt clean

build:
	$(GO) build -o $(BIN) ./cmd/sitka

test:
	$(GO) test ./...

lint:
	golangci-lint run

fmt:
	$(GO) fmt ./...

clean:
	rm -rf bin
