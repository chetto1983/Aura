# Source: PRD §Slice 0.5 file targets; sqlc CLI docs; golang-migrate v4 CLI docs.
# Windows operators: run from PowerShell, OR prefix `docker compose run` calls with
# MSYS_NO_PATHCONV=1 in Git Bash (Pitfall #7 — feedback_docker_compose_run_msys_path_mangling).
# Phase 1 targets do not use `docker compose run`; this is mostly informational.
#
# sqlc CLI: install with `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`
# (v1.27.0 panics on Windows hosts via wazero out-of-bounds; v1.31.1 verified clean).

.PHONY: help sqlc lint test test-race file-size db-up db-migrate db-status db-reset neo4j-up neo4j-migrate neo4j-status neo4j-reset smoke restore-drill

help:
	@echo "make sqlc          — regenerate internal/db/sqlc/ from queries/"
	@echo "make lint          — golangci-lint run ./..."
	@echo "make test          — go test ./... (unit tier, no build tags)"
	@echo "make test-race     — go test -race ./... (unit tier with race detector)"
	@echo "make file-size     — enforce 600-LOC cap via scripts/check-file-size.sh"
	@echo "make db-up         — docker compose up -d postgres (waits healthy)"
	@echo "make db-migrate    — aura db migrate (role aura_migrate)"
	@echo "make db-status     — aura db status"
	@echo "make db-reset      — DESTRUCTIVE: drop+recreate schema aura (dev only, requires AURA_RESET_YES=1)"
	@echo "make neo4j-up      — docker compose up -d neo4j aura-llama-embed (waits healthy)"
	@echo "make neo4j-migrate — aura neo4j migrate (applies internal/knowledge/migrations/*.cypher)"
	@echo "make neo4j-status  — aura neo4j status"
	@echo "make neo4j-reset   — DESTRUCTIVE: drop all indexes + MATCH (n) DETACH DELETE (dev only, AURA_RESET_YES=1)"
	@echo "make smoke         — scripts/neo4j_smoke.sh (Italian recall@5 5/5 + p95 <= 30ms)"
	@echo "make restore-drill — scripts/restore_drill.sh (pg_dump -> pg_restore, asserts < 90s)"

sqlc:
	sqlc generate

lint:
	golangci-lint run ./...

test:
	go test ./... -count=1

test-race:
	go test -race -count=1 ./...

file-size:
	bash scripts/check-file-size.sh

db-up:
	docker compose up -d postgres
	@echo "Waiting for postgres healthy..."
	@until docker compose ps --format json postgres | grep -q '"Health":"healthy"'; do sleep 1; done
	@echo "ok"

db-migrate: db-up
	go run ./cmd/aura db migrate

db-status:
	go run ./cmd/aura db status

db-reset:
	@[ "$$AURA_RESET_YES" = "1" ] || { echo "refusing — set AURA_RESET_YES=1 to confirm destructive reset"; exit 1; }
	go run ./cmd/aura db reset --yes

# ↓↓ Slice 0.7 — Neo4j + embed sidecar + Italian smoke ↓↓
neo4j-up:
	docker compose up -d neo4j aura-llama-embed
	@echo "Waiting for neo4j healthy..."
	@until docker compose ps --format json neo4j | grep -q '"Health":"healthy"'; do sleep 1; done
	@echo "Waiting for aura-llama-embed healthy..."
	@until docker compose ps --format json aura-llama-embed | grep -q '"Health":"healthy"'; do sleep 1; done
	@echo "ok"

neo4j-migrate: db-migrate neo4j-up
	go run ./cmd/aura neo4j migrate

neo4j-status:
	go run ./cmd/aura neo4j status

neo4j-reset:
	@[ "$$AURA_RESET_YES" = "1" ] || { echo "refusing — set AURA_RESET_YES=1 to confirm destructive reset"; exit 1; }
	go run ./cmd/aura neo4j reset --yes

smoke: neo4j-migrate
	bash scripts/neo4j_smoke.sh

restore-drill: db-up
	bash scripts/restore_drill.sh
