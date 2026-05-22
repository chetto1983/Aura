.PHONY: all build test run fmt vet clean web web-build compose-up compose-test download-models file-size-check registry-diff

all: test build

# download-models — optional offline-prep helper for first-run installs.
# Normally the aura-init-models compose service handles this automatically
# when `docker compose up` runs. This target is for operators who want to
# fetch the file ahead of time over a stable connection. The SHA-256 is
# kept in sync with internal/install/embedding.go; bump both together.
EMBEDDING_MODEL_FILE := data/embeddinggemma-300m-Q4_0.gguf
EMBEDDING_MODEL_URL  := https://huggingface.co/unsloth/embeddinggemma-300m-GGUF/resolve/main/embeddinggemma-300m-Q4_0.gguf
EMBEDDING_MODEL_SHA  := edc6015cb15694c27be7d1d33f1bc015db9a358ff51ed524628c027504907ba9

download-models: $(EMBEDDING_MODEL_FILE)
$(EMBEDDING_MODEL_FILE):
	@mkdir -p data
	@echo "Fetching $(EMBEDDING_MODEL_URL) (~265 MB)…"
	curl -fL --retry 3 -o "$@.partial" "$(EMBEDDING_MODEL_URL)"
	@echo "$(EMBEDDING_MODEL_SHA)  $@.partial" | sha256sum -c -
	mv "$@.partial" "$@"
	@echo "✓ $(EMBEDDING_MODEL_FILE) ready (sha256 verified)"

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

file-size-check:
	bash scripts/check-file-size.sh .file-size-baseline.txt

registry-diff:
	bash scripts/registry-diff.sh
