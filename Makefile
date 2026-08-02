# Source: PRD §Slice 0.5 file targets; sqlc CLI docs; golang-migrate v4 CLI docs.
# Windows operators: run from PowerShell, OR prefix `docker compose run` calls with
# MSYS_NO_PATHCONV=1 in Git Bash (Pitfall #7 — feedback_docker_compose_run_msys_path_mangling).
# Phase 1 targets do not use `docker compose run`; this is mostly informational.
#
# sqlc CLI: install with `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`
# (v1.27.0 panics on Windows hosts via wazero out-of-bounds; v1.31.1 verified clean).

.PHONY: help tools sqlc lint vet deadcode vuln coverage coverage-docker quality quality-full test test-race tagged-tier-compile file-size web-freshness web-lint web-test web-mutation web-quality evidence-contracts critical-mutation observability-evidence release-readiness db-up db-migrate db-status db-reset memory-up arcadedb-integration restore-drill load-chaos

# Resolve go-installed tool binaries even when $GOPATH/bin is not on PATH
# (common in a fresh WSL login shell). Falls back to a bare name on PATH.
GOBIN := $(shell go env GOPATH)/bin
GO_PACKAGES := $(shell bash scripts/go_packages.sh)

help:
	@echo "make tools         — go install the quality toolchain (lint/vuln/dupl/mutation/etc.)"
	@echo "make quality       — pre-push gate: deadcode vet build file-size lint test-race vuln (no containers)"
	@echo "make quality-full  — quality + coverage gate (requires the container stack up)"
	@echo "make sqlc          — regenerate internal/db/sqlc/ from queries/"
	@echo "make lint          — golangci-lint run ./... (incl. dupl)"
	@echo "make deadcode      — deadcode -test ./... (unreachable Go code scan)"
	@echo "make vet           — go vet ./..."
	@echo "make vuln          — govulncheck ./... (supply-chain CVE scan)"
	@echo "make coverage      — owned-surface coverage floor >=85% (scripts/coverage_gate.sh; needs the stack up)"
	@echo "make coverage-docker — like coverage, but every database it touches is a DISPOSABLE container"
	@echo "make test          — go test ./... (unit tier, no build tags)"
	@echo "make test-race     — go test -race ./... (unit tier with race detector)"
	@echo "make tagged-tier-compile — compile every discovered Aura integration/live/eval tier"
	@echo "make file-size     — enforce 600-LOC cap via scripts/check-file-size.sh"
	@echo "make web-freshness — rebuild web/ + assert committed internal/webui/dist is fresh (D-05)"
	@echo "make web-lint      — frontend static gate: eslint --max-warnings=0 + tsc + prettier --check"
	@echo "make web-test      — vitest run --coverage (>=85% thresholds enforced in vitest.config.ts)"
	@echo "make web-mutation  — Stryker mutation run (break=70: fails below 70% killed)"
	@echo "make web-quality   — full frontend gate: web-lint + web-test + web-mutation"
	@echo "make evidence-contracts — self-test every candidate-bound release report parser"
	@echo "make critical-mutation — >=70% per critical Go boundary + frontend, no averaging"
	@echo "make observability-evidence — fixtures + runtime smoke + live Aura endpoints"
	@echo "make release-readiness — validate the ten fresh reports for the current Git SHA"
	@echo "make db-up         — docker compose up -d postgres (waits healthy)"
	@echo "make db-migrate    — aura db migrate (role aura_migrate)"
	@echo "make db-status     — aura db status"
	@echo "make db-reset      — DESTRUCTIVE: drop+recreate schema aura (dev only, requires AURA_RESET_YES=1)"
	@echo "make memory-up     — docker compose up -d arcadedb arcadedb-mcp aura-llama-embed (waits healthy)"
	@echo "make arcadedb-integration — run the arcadedb_integration tier live, as CI does"
	@echo "make restore-drill — three-plane DR drill with measured RPO/RTO"
	@echo "make load-chaos    — blocking Vegeta + Toxiproxy production gate"

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
	go vet $(GO_PACKAGES)

lint:
	$(GOBIN)/golangci-lint run $(GO_PACKAGES)

deadcode:
	bash scripts/deadcode_gate.sh "$(GOBIN)/deadcode" -test $(GO_PACKAGES)

vuln:
	$(GOBIN)/govulncheck $(GO_PACKAGES)

# Owned-surface coverage floor (CLAUDE.md >=85%). Integration tiers need the
# container stack + composed DSNs; bring them up with `make db-migrate memory-up`
# first (or run inside the CI knowledge job that already has the stack).
coverage:
	bash scripts/coverage_gate.sh

# Same owned-surface floor as `coverage`, but every database the destructive tiers
# touch is a DISPOSABLE container, never the live deployment. Needs the embed sidecar
# up (`make memory-up`) + creds in .env.
coverage-docker:
	bash scripts/coverage_docker.sh

# Pre-push gate that needs NO containers — fast feedback before a push.
quality: deadcode vet file-size lint test-race vuln
	go build $(GO_PACKAGES)
	@echo "ok: quality gate passed (deadcode vet build file-size lint test-race vuln)"

# Full gate including the container-backed coverage floor.
quality-full: quality coverage
	@echo "ok: quality-full passed (quality + coverage >=85%)"

test:
	go test -count=1 $(GO_PACKAGES)

test-race:
	go test -race -count=1 $(GO_PACKAGES)

tagged-tier-compile:
	bash scripts/tagged_tier_compile_test.sh
	bash scripts/tagged_tier_compile.sh

file-size:
	bash scripts/check-file-size.sh

# Rebuild web/ on the local Node toolchain and assert the committed embed source
# (internal/webui/dist) equals a fresh build (D-05 tamper-evidence). The byte-canonical
# proof is the CI web-dist-freshness job on Linux Node 24; this is the local mirror.
web-freshness:
	bash scripts/web_dist_freshness.sh

# ↓↓ Frontend (web/) industrial gates — mirror the CI web-* jobs. Assume
# web/node_modules is present (run `npm ci` in web/ first). ↓↓
web-lint:
	cd web && npm run lint && npm run typecheck && npm run format:check

web-test:
	cd web && npm run test

web-mutation:
	cd web && npm run mutation

# Full frontend gate: static checks + unit/coverage + mutation. Mirrors the CI
# web-lint + web-test + web-mutation jobs (the heavy tiers stay out of the git
# pre-push hook, which runs only the fast web-lint trio via lefthook).
web-quality: web-lint web-test web-mutation
	@echo "ok: web-quality passed (lint typecheck format test coverage mutation)"

evidence-contracts:
	PYTHONPATH=scripts python3 -m unittest \
		scripts/audit_closure_gate_test.py \
		scripts/capability_eval_test.py \
		scripts/critical_mutation_gate_test.py \
		scripts/observability_evidence_test.py \
		scripts/production_load_chaos_test.py \
		scripts/release_check_run_gate_test.py \
		scripts/release_readiness_gate_test.py \
		scripts/rollback_rehearsal_test.py \
		scripts/security_evidence_test.py
	bash scripts/coverage_gate_test.sh
	bash scripts/restore_drill_name_test.sh

critical-mutation:
	PYTHONPATH=scripts python3 -m unittest scripts/critical_mutation_gate_test.py
	PYTHONPATH=scripts python3 scripts/critical_mutation_gate.py

observability-evidence:
	PYTHONPATH=scripts python3 -m unittest scripts/observability_evidence_test.py
	PYTHONPATH=scripts python3 scripts/observability_evidence.py

release-readiness:
	PYTHONPATH=scripts python3 scripts/release_readiness_gate.py \
		--evidence-dir artifacts/production-readiness \
		--candidate "$$(git rev-parse HEAD)" \
		--output artifacts/production-readiness/release-readiness-report.json

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

define wait_compose_healthy
	@echo "Waiting for $(1) healthy..."
	@deadline=$$(($$(date +%s) + $${AURA_COMPOSE_HEALTH_TIMEOUT_SEC:-900})); \
	while true; do \
		container=$$(docker compose ps -q "$(1)" 2>/dev/null || true); \
		if [ -n "$$container" ]; then \
			status=$$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$$container" 2>/dev/null || echo starting); \
		else \
			status=starting; \
		fi; \
		echo "$(1) health: $$status"; \
		[ "$$status" = "healthy" ] && break; \
		if [ $$(date +%s) -ge $$deadline ]; then \
			echo "$(1) did not become healthy" >&2; \
			docker compose ps "$(1)" || true; \
			docker compose logs --tail=120 "$(1)" || true; \
			exit 1; \
		fi; \
		sleep 5; \
	done
endef

# ↓↓ live stack: graph substrate (ArcadeDB + its MCP) + embed sidecar ↓↓
#
# ArcadeDB needs no migration job — its schema is idempotent DDL applied at
# connect — so this target only brings the services up healthy. A tagged tier that
# wants the whole live stack asks for `db-migrate memory-up`.
memory-up:
	docker compose up -d arcadedb arcadedb-mcp aura-llama-embed
	$(call wait_compose_healthy,arcadedb)
	$(call wait_compose_healthy,arcadedb-mcp)
	$(call wait_compose_healthy,aura-llama-embed)
	@echo "ok"

# The arcadedb_integration tier, run exactly the way the CI job
# `arcadedb-integration-test` runs it — same tags, same -skip/-run selectors, same
# ArcadeDB-only stack. Keep the two in step: a local green that exercised a different
# set than CI is worth nothing.
#
# The tier reads ARCADEDB_PASSWORD / ARCADEDB_URL / ARCADEDB_DATABASE /
# AURA_ARCADEDB_TENANT_SECRET from the environment and SKIPS locally when they are
# absent. Export them (from .env) to make this a real gate; add CI=1 to arm the
# no-skip-as-green guards and turn any missing piece into a failure, as in the pipeline.
#
# TestLocomo* and TestMemoryVector* are excluded here for the same reasons as in CI:
# an external dataset and a model download respectively. Run those by hand.
arcadedb-integration:
	docker compose up -d arcadedb
	$(call wait_compose_healthy,arcadedb)
	go test -race -tags arcadedb_integration -count=1 \
		-skip '^TestLocomo|^TestMemoryVector' ./internal/arcadedb/
	go test -race -tags arcadedb_integration -count=1 \
		-run '^TestArcadeMemoryPurgeLive' ./cmd/aura/
	@echo "ok: arcadedb_integration tier passed against a live ArcadeDB"

restore-drill: db-migrate memory-up
	bash scripts/garage_bootstrap.sh
	bash scripts/restore_drill.sh

load-chaos: db-migrate memory-up
	bash scripts/garage_bootstrap.sh
	python3 scripts/production_load_chaos.py --race
