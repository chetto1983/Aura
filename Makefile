.PHONY: all build test run debug-llm fmt vet clean

all: test build

build:
	go build ./...

test:
	go test ./...

run:
	go run ./cmd/aura

debug-llm:
	go run ./cmd/debug_llm

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	go clean ./...
