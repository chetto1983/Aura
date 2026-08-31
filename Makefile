# Source: PRD §Slice 0.5 file targets; sqlc CLI docs; golang-migrate v4 CLI docs.
# Windows operators: run from PowerShell, OR prefix `docker compose run` calls with
# MSYS_NO_PATHCONV=1 in Git Bash (Pitfall #7 — feedback_docker_compose_run_msys_path_mangling).
# Phase 1 targets do not use `docker compose run`; this is mostly informational.
#
# sqlc CLI: install with `go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1`
# (v1.27.0 panics on Windows hosts via wazero out-of-bounds; v1.31.1 verified clean).

.PHONY: help tools sqlc lint vet deadcode vuln coverage coverage-docker quality quality-full test test-race tagged-tier-compile file-size embedding-model-contract llm-model-contract web-lint web-test web-mutation web-quality evidence-contracts agent-memory-eval-contract agent-memory-eval critical-mutation observability-check observability-evidence release-readiness db-up db-migrate db-status db-reset memory-up sandbox-images arcadedb-integration ingest-test restore-drill load-chaos

# Resolve go-installed tool binaries even when $GOPATH/bin is not on PATH
# (common in a fresh WSL login shell). Falls back to a bare name on PATH.
GOBIN := $(shell go env GOPATH)/bin
GO_PACKAGES := $(shell bash scripts/go_packages.sh)

help:
	@echo "make embedding-model-contract — verify validated atomic EmbeddingGemma cache materialization"
	@echo "make llm-model-contract     — verify pinned gemma-4-12B model+MTP-drafter fetch"
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
	@echo "make web-lint      — frontend static gate: eslint --max-warnings=0 + tsc + prettier --check"
	@echo "make web-test      — vitest run --coverage (>=85% thresholds enforced in vitest.config.ts)"
	@echo "make web-mutation  — Stryker mutation run (break=70: fails below 70% killed)"
	@echo "make web-quality   — full frontend gate: web-lint + web-test + web-mutation"
	@echo "make evidence-contracts — self-test every candidate-bound release report parser"
	@echo "make agent-memory-eval-contract — deterministic evaluator; never claims a live MRS"
	@echo "make agent-memory-eval — blocking MRS over the already-running live memory stack"
	@echo "make sandbox-images — build the per-identity box + egress images the daemon needs"
	@echo "make critical-mutation — >=70% per critical Go boundary + frontend, no averaging"
	@echo "make observability-check — verify live Tempo/Prometheus readiness and Aura scrape"
	@echo "make observability-evidence — fixtures + runtime smoke + live Aura endpoints"
	@echo "make release-readiness — validate the twelve fresh reports for the current Git SHA"
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

# The BEHAVIOUR gate: does she still answer the question? Every other gate in this
# file measures the code — build, vet, lint, race, coverage, mutation — and none of
# them can see a turn that answers wrong, loops, or reaches for the open internet to
# find something in the operator's own spreadsheet. All three happened on
# 2026-08-03 and were found by hand, one at a time.
#
# NOT in CI, and deliberately: every case is a real turn against a real model and
# costs money. Run it before shipping anything that touches the prompt, the tool
# manifest, the registry or memory retrieval. Needs the stack up and the identity
# that owns the document library:
#
#   AURA_EVAL_IDENTITY=<uuid> make agent-eval
agent-eval:
	@test -n "$(AURA_EVAL_IDENTITY)" || { echo "AURA_EVAL_IDENTITY=<uuid> is required (see the Makefile comment)"; exit 1; }
	go test -tags agent_eval -count=1 -v -timeout 30m ./internal/agenteval/

# Pre-push gate that needs NO containers — fast feedback before a push.
quality: deadcode vet file-size embedding-model-contract llm-model-contract lint test-race vuln
	go build $(GO_PACKAGES)
	@echo "ok: quality gate passed (deadcode vet build file-size embedding-model-contract llm-model-contract lint test-race vuln)"

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

embedding-model-contract:
	bash scripts/fetch_embedding_model_test.sh

# The same contract for the answering model. It had a test from the day it was written
# and no target and no CI job to run it, so for two weeks the pins it guards were only
# ever checked by whoever remembered the script existed.
llm-model-contract:
	bash scripts/fetch_llm_model_test.sh

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
		scripts/agent_memory_eval_test.py \
		scripts/capability_eval_test.py \
		scripts/critical_mutation_gate_test.py \
		scripts/observability_evidence_test.py \
		scripts/production_load_chaos_test.py \
		scripts/release_check_run_gate_test.py \
		scripts/release_readiness_gate_test.py \
		scripts/rollback_rehearsal_test.py \
		scripts/security_evidence_test.py
	bash scripts/coverage_profile_gate_test.sh
	bash scripts/coverage_gate_test.sh
	bash scripts/docker_coverage_gate_test.sh
	bash scripts/restore_drill_name_test.sh

agent-memory-eval-contract:
	PYTHONPATH=scripts python3 -m unittest scripts/agent_memory_eval_test.py
	PYTHONPATH=scripts python3 scripts/agent_memory_eval.py --tier deterministic

agent-memory-eval:
	PYTHONPATH=scripts python3 -m unittest scripts/agent_memory_eval_test.py
	PYTHONPATH=scripts python3 scripts/agent_memory_eval.py --tier all

critical-mutation:
	PYTHONPATH=scripts python3 -m unittest scripts/critical_mutation_gate_test.py
	PYTHONPATH=scripts python3 scripts/critical_mutation_gate.py

observability-check:
	bash scripts/observability_sidecar_check.sh

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

# The per-identity box and its egress sidecar. They are NOT compose services --
# the daemon builds one box per identity through usersandbox -- so nothing in
# `docker compose up` ever produces them, and until 2026-08-31 the ONLY build of
# aura-sandbox in the repo lived in scripts/production_load_chaos.py. The gap is
# not cosmetic: ensureImage cannot find the image, Resolve fails, Route denies,
# and every box-capable tool (shell_exec, read_file, write_file, patch,
# search_files, send_file, document_open) denies with it while the sandbox
# readiness probe holds the daemon unhealthy.
#
# An appliance never runs this -- it pulls the published edge tags that
# scripts/install.sh pins. This target is for a developer working from the repo,
# where the default image names are the bare local ones.
sandbox-images:
	docker build -f docker/aura-sandbox/Dockerfile -t $${AURA_SANDBOX_IMAGE:-aura-sandbox:latest} .
	docker build -f docker/aura-egress/Dockerfile -t $${AURA_SANDBOX_EGRESS_IMAGE:-aura-egress:latest} .
	@echo "ok"

# Compatibility name for operators' muscle memory. The MRS evaluator is the
# single current memory gate and fails closed on missing, skipped, coverage or
# latency evidence.
arcadedb-integration: db-migrate memory-up
	$(MAKE) agent-memory-eval

# The ingestion sidecar's own suite. It runs INSIDE aura-ingest:local because half of it
# is about that image's contents -- iscc-tika, LibreOffice, poppler -- and a host venv
# would test a toolchain nobody deploys. Two mounts and the env are not optional and each
# one has burned an hour: without /fx, 25 of the extraction tests fail on missing
# fixtures; without ARCADEDB_PASSWORD, test_arcade_integration.py reads os.environ at
# import and fails COLLECTION for the whole suite, which is a red that looks like a broken
# product. pytest is installed at run time rather than baked in: a test dependency has no
# business in the image that ships to production.
ingest-test: memory-up
	docker build -f docker/aura-ingest/Dockerfile -t aura-ingest:local .
	@# @-silenced: make echoes the recipe line and ARCADEDB_PASSWORD sits on it.
	@docker run --rm --network aura_default --entrypoint sh \
	  -v "$(CURDIR)/services/ingest:/app/ingest:ro" \
	  -v "$(CURDIR)/scripts/fixtures/document_pipeline_e2e:/fx:ro" \
	  -e ARCADEDB_PASSWORD="$(ARCADEDB_PASSWORD)" \
	  -e ARCADE_HTTP=http://arcadedb:2480 \
	  -e ARCADE_BOLT=bolt://arcadedb:7687 \
	  -e AURA_EMBED_BASE_URL=http://aura-llama-embed:8081 \
	  -e AURA_INGEST_IDENTITY_ID=00000000-0000-0000-0000-000000000001 \
	  -e AURA_INGEST_S3_BUCKET=aura-ingest-test \
	  -e AURA_INGEST_S3_ACCESS_KEY_ID=test \
	  -e AURA_INGEST_S3_SECRET_ACCESS_KEY=test \
	  aura-ingest:local -c 'pip install --quiet pytest && cd /app && python -m pytest ingest/tests -q'

restore-drill: db-migrate memory-up
	bash scripts/garage_bootstrap.sh
	bash scripts/restore_drill.sh

load-chaos: db-migrate memory-up
	bash scripts/garage_bootstrap.sh
	python3 scripts/production_load_chaos.py --race
