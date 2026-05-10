.PHONY: all build test run fmt vet clean web web-build compose-up compose-test

all: test build

build:
	go build -o aura ./cmd/aura

test:
	go test ./...

run:
	go run ./cmd/aura

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	go clean ./...

web:
	cd web && npm run dev

web-build:
	cd web && npm install && npm run build

compose-up:
	docker compose up -d --build

compose-test:
	docker compose --profile test run --rm test
