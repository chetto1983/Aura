---
name: babysit
description: Job-by-job diagnosis of a red CI run on an Aura pull request — what each ci.yml / skills.yml / codeql.yml job runs, how to reproduce it locally, and how to tell the root failure from the secondary errors it causes. Use when a CI check fails on an Aura PR or on master, or when watching a PR's checks. For conventions and push rules see the steward skill.
---

# Babysit: diagnosing Aura CI

## Posture

- **No "flake".** A test that fails or hangs has a cause; find it. Re-run a job at most once,
  and only when it died before any test body ran (checkout, install, runner loss).
- **Every red job and every open review thread gets an outcome.** That is a pushed fix, or one
  comment that names the check, the cause and why it is not this PR's.
- **Read the first failure, not the last line.** Many jobs end with an error that is a
  consequence, not the cause. The table below lists the known ones.

## Reading logs

Fetch a failed job's log with `mcp__github__get_job_logs` (`return_content: true`). Start
with about 120 tail lines and widen if the first `--- FAIL` or `panic` is above them.
A package that `FAIL`s at about 600 s hit the `go test` timeout. Read the goroutine dump for the
blocked test: it is usually waiting on a retry loop whose error is permanent.

## Secondary errors (not the cause)

| Message | Real cause |
|---|---|
| `No files were found with the provided path: artifacts/production-readiness/...` | The test step before it failed and never wrote the report |
| `critical-mutation-gate: FAIL: <scope>: no measurement of its current inputs` | The `Go mutation (<group>)` matrix job for that scope failed or was cancelled; fix that job |
| CodeQL `neutral`, "configuration not found" | The PR's analysis had not uploaded yet; not a finding |

## Jobs

Unless noted, jobs live in `.github/workflows/ci.yml` and run Go 1.27.1 with `GOTOOLCHAIN=local`.
Go package lists come from `bash scripts/go_packages.sh`, never a bare `./...`
(`scripts/check_ci_go_packages.sh` rejects one).

| Job | Local reproduction | Notes |
|---|---|---|
| Build + vet + lint + deadcode + file-size cap | `go vet`, `go build`, `golangci-lint run` (v2.13.2), `make file-size` | 600-LOC cap per file |
| Unit tests (race detector) | `go test -race -count=1 $(bash scripts/go_packages.sh)` | Includes the docs contract tests in `cmd/aura` |
| Supply-chain vulnerability scan | `govulncheck $(bash scripts/go_packages.sh)` (CI pins v1.6.0) | Only findings in called code fail it |
| sqlc generate is in sync | `sqlc generate` (v1.31.1), then `git diff internal/db/sqlc/` | Commit the generated diff, never hand-edit |
| Integration tests (db_integration tag) | `make db-up`, then `go test -tags db_integration -race -count=1 -p 1 ./internal/db/... ./internal/cron/... ./internal/agui/... ./internal/documents/...` | Needs `AURA_DB_URL` / `AURA_DB_MIGRATE_URL` |
| Race + leak DB tier | `make db-up`, `go run ./cmd/aura db migrate`, `go test -race -tags=db_integration -count=1 -p 1 ...` | goleak: a leaked goroutine fails it |
| Owned-surface coverage gate | `bash scripts/coverage_docker.sh` | Floor 85%, package policy in `scripts/coverage_package_policy.json`; a test failure here shows as a missing report |
| Sandbox docker_integration tier | `bash scripts/docker_coverage_gate.sh` | Needs native Linux dockerd |
| Go mutation (turn-reading / elicitation-boundaries / media) | `go-mutesting` per `scripts/critical_mutation_gate.py` | Scopes and the 70% minimum live in that script |
| Critical mutation testing (≥ 70% killed) | `PYTHONPATH=scripts python3 scripts/critical_mutation_gate.py` | Aggregates the matrix jobs; see secondary errors |
| MUSR two-identity E2E | `make db-migrate memory-up-core && make musr-e2e` | Cross-identity deny is the contract |
| Agent Memory MRS | `make db-migrate`, `docker compose up -d arcadedb`, `make agent-memory-eval` | Live ArcadeDB + EmbeddingGemma |
| Ingestion sidecar / reconciliation | `make ingest-test` / `make ingest-image` with Garage | Live stores |
| Web tools (SSRF + web_integration) | `bash scripts/ssrf_smoke.sh`, `go test -race -tags web_integration ./internal/web/` | Needs SearXNG |
| Telegram / WhatsApp / Calendar / Multimodal tiers | `go test -race -tags <tier>_integration ...` (see the job) | Under `$CI` a missing env var is `t.Fatal`, never a skip |
| Reasoning tier accuracy | `go test -tags reasoning_live -run TestReasoningClassifierLive ./internal/agent/prompt/` | Real embedder |
| Web lint / unit / Stryker / E2E | `cd web && npm ci && npm run lint && npm run typecheck`, `npm run test`, `npm run mutation`; E2E boots `aura serve` | Playwright E2E needs Postgres + Garage |
| Skills gate / Skills mutation (`skills.yml`) | see `.github/workflows/skills.yml` | db_integration + fuzz + snippet exec |
| CodeQL (`codeql.yml`) | none locally | Also runs weekly on schedule |

## Known root causes

- **Credentialed request over plain HTTP in a test.** openai-go ≥ 3.69 refuses an
  `Authorization` header over `http://`. Provider fixtures must be `httptest.NewTLSServer`
  with the server's own client. Do not use `option.WithUnsafeAllowHTTP`: it bypasses the
  caller's transport, so the redirect, dialer and keep-alive behaviour under test stops running.
- **A docs move breaking `cmd/aura`.** Contract tests read phrases from README and docs.
- **A test waiting to the package timeout.** Watchers and pollers retry until their
  deadline by design. A permanent error in the fixture shows up as a 600 s hang, not as a
  fast failure.
