.PHONY: all build test test-race run-proxy run-upstream clean

all: build test

build:
	go build ./cmd/...

test:
	go test -v ./...

test-race:
	go test -v -race ./...

run-proxy:
	go run ./cmd/proxy

run-upstream:
	go run ./cmd/mock_upstream

clean:
	rm -rf bin/ coverage.out

