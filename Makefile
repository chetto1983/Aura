# Source: PRD §Slice 0.5 file targets; sqlc CLI docs; golang-migrate v4 CLI docs.
# Windows operators: run from PowerShell, OR prefix `docker compose run` calls with
# MSYS_NO_PATHCONV=1 in Git Bash (Pitfall #7 — feedback_docker_compose_run_msys_path_mangling).
# Phase 1 targets do not use `docker compose run`; this is mostly informational.
#
# sqlc CLI: install with `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`
# (v1.27.0 panics on Windows hosts via wazero out-of-bounds; v1.31.1 verified clean).

.PHONY: help tools sqlc lint vet vuln coverage quality quality-full test test-race file-size db-up db-migrate db-status db-reset neo4j-up neo4j-migrate neo4j-status neo4j-reset smoke restore-drill sandbox-up

# Resolve go-installed tool binaries even when $GOPATH/bin is not on PATH
# (common in a fresh WSL login shell). Falls back to a bare name on PATH.
GOBIN := $(shell go env GOPATH)/bin

help:
	@echo "make tools         — go install the quality toolchain (lint/vuln/dupl/mutation/etc.)"
	@echo "make quality       — pre-push gate: vet build file-size lint test-race vuln (no containers)"
	@echo "make quality-full  — quality + coverage gate (requires the container stack up)"
	@echo "make sqlc          — regenerate internal/db/sqlc/ from queries/"
	@echo "make lint          — golangci-lint run ./... (incl. dupl)"
	@echo "make vet           — go vet ./..."
	@echo "make vuln          — govulncheck ./... (supply-chain CVE scan)"
	@echo "make coverage      — owned-surface coverage floor >=85% (scripts/coverage_gate.sh; needs stack)"
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
	@echo "make sandbox-up    — start aura-sandbox; gVisor runsc overlay default-on x86, runc+seccomp on arm64"

# Bootstrap the quality toolchain into $GOPATH/bin. golangci-lint is pinned to the
# CI version (.github/workflows/ci.yml) for local/CI parity; the rest track latest.
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
	go install honnef.co/go/tools/cmd/staticcheck@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/mibk/dupl@latest
	go install gotest.tools/gotestsum@latest
	go install golang.org/x/tools/cmd/deadcode@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@latest
	go install github.com/evilmartians/lefthook@latest
	@echo "now run: lefthook install   (wires the git pre-commit/pre-push hooks)"

sqlc:
	sqlc generate

vet:
	go vet ./...

lint:
	$(GOBIN)/golangci-lint run ./...

vuln:
	$(GOBIN)/govulncheck ./...

# Owned-surface coverage floor (CLAUDE.md >=85%). Integration tiers need the
# container stack + composed DSNs; bring them up with `make neo4j-migrate` first
# (or run inside the CI knowledge job that already has the stack).
coverage:
	bash scripts/coverage_gate.sh

# Pre-push gate that needs NO containers — fast feedback before a push.
quality: vet file-size lint test-race vuln
	go build ./...
	@echo "ok: quality gate passed (vet build file-size lint test-race vuln)"

# Full gate including the container-backed coverage floor.
quality-full: quality coverage
	@echo "ok: quality-full passed (quality + coverage >=85%)"

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

# ↓↓ Slice 2a — hardened sandbox sidecar (gVisor default-on x86, D-04/SC#5) ↓↓
# Arch-gated at make-parse time via `uname -m` so `make -n sandbox-up` prints a
# command line that INCLUDES `-f compose.gvisor.yaml` on x86_64 (gVisor runsc
# default-on) and OMITS it on arm64 (runc + seccomp floor — Pitfall 5, gVisor
# arm64 is non-GA). The overlay is the OPERATOR DEFAULT, not opt-in, so the
# strongest x86 boundary is on unless deliberately stripped.
SANDBOX_ARCH := $(shell uname -m)
ifeq ($(SANDBOX_ARCH),x86_64)
SANDBOX_COMPOSE := docker compose -f compose.yaml -f compose.gvisor.yaml
SANDBOX_RUNTIME_LABEL := gVisor runsc (x86 default-on)
else
SANDBOX_COMPOSE := docker compose -f compose.yaml
SANDBOX_RUNTIME_LABEL := runc + seccomp floor ($(SANDBOX_ARCH) — gVisor arm64 non-GA)
endif

sandbox-up:
	@echo "sandbox runtime profile: $(SANDBOX_RUNTIME_LABEL)"
	$(SANDBOX_COMPOSE) up -d aura-sandbox
	@echo "Waiting for aura-sandbox healthy..."
	@until docker compose ps --format json aura-sandbox | grep -q '"Health":"healthy"'; do sleep 1; done
	@echo "ok"
