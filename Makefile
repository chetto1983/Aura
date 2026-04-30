.PHONY: all build test run debug-llm fmt vet clean web web-build ui-dev

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

# Slice 10b — frontend dashboard
web:
	cd web && npm run dev

web-build:
	cd web && npm install && npm run build

ui-dev:
	$(MAKE) -j2 web run
