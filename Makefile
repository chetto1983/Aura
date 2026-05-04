.PHONY: all build test run debug-llm fmt vet clean web web-build ui-dev

all: test build

build:
	go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest -icon internal/tray/icon_app.ico -o cmd/aura/resource.syso cmd/aura/versioninfo.json
	go build -o aura.exe ./cmd/aura

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
	go run ./cmd/build_icon && cd web && npm install && npm run build

ui-dev:
	$(MAKE) -j2 web run
