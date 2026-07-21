---
phase: 39
slug: idempotency-observability-pack
status: approved
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-21
---

# Phase 39 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing`, table-driven subtests, `-race`, integration build tags, `go vet`, existing `goleak`; Prometheus `promtool`; Docker Compose smoke tests |
| **Config file** | `go.mod`, `.github/workflows/ci.yml`, `compose.yaml`; new Phase 39 observability fixtures are Wave 0 |
| **Quick run command** | Run the focused `<verify><automated>` command for the active `39-NN-MM` task; every task has one below. |
| **Full suite command** | `go test ./...` then `go vet ./...`, the repository's exact tagged CI matrix, `pwsh -NoProfile -File scripts/verify-observability.ps1`, and `docker compose --profile observability config` |
| **Estimated runtime** | Measure during Wave 0; keep per-task package loops below 60 seconds |

---

## Sampling Rate

- **After every task commit:** Run the narrow package/file command assigned to that task in its `<automated>` check.
- **After every plan wave:** Run `go test ./...` and all observability/config validators introduced by the completed wave.
- **Before `$gsd-verify-work`:** Run `go test ./...`, `go vet ./...`, repository lint/integration commands, Prometheus rule checks/tests, dashboard validation, and `docker compose config`.
- **Max feedback latency:** 60 seconds for the per-task loop; split slower integration checks into the wave gate.

---

## Per-Plan and Per-Requirement Verification Map

| Task IDs | Wave | Requirement / phase contract | Planned observable evidence | Automated command | File status | Status |
|----------|------|------------------------------|-----------------------------|-------------------|-------------|--------|
| 39-01-01..03 | 1 | Phase goal: registry foundation | Deterministic typed fingerprint; one owner under 32 concurrent starts; replay/conflict/in-progress/indeterminate; current-head migration; independent replay expiry. | `go test ./internal/idempotency` then `go test -race -tags db_integration -count=1 -p 1 ./internal/idempotency` | Wave 0 new package/migration | ⬜ pending |
| 39-02-01..03 | 2 | Phase goal: end-to-end mutation idempotency | Complete HTTP/CLI/tool inventory including approval/resume; trusted identity/scope; stable key across retries; zero effects for non-acquired states; no mutating reconnect replay. | `go test -race ./internal/idempotency ./internal/gateway ./internal/agent/... ./internal/mcp ./internal/runner ./internal/cron ./internal/agui ./cmd/aura -run 'Test.*(Idempot|Operation|MutationCoverage|Resume|Replay|Conflict|Reconnect)'` | Wave 0 adapters/coverage | ⬜ pending |
| 39-03-01..03 | 1 | OBS-01, OBS-02 | Bind/runtime listener failure; concurrent one-deadline PG/Neo4j plus migration/listener/scheduler/drain truth table; sanitized codes; liveness separation; Compose endpoint/budget. | `go test -race ./internal/readiness ./internal/agui ./internal/cron ./cmd/aura -run 'Test.*(Ready|Readiness|Healthz|Listener|Scheduler|Compose)'` | Existing readiness partial; new snapshot/seams | ⬜ pending |
| 39-04-01..03 | 3 | OBS-03 | One dual-reader MeterProvider; bounded shutdown; finite catalog; no duplicate legacy series; LLM/tool/MCP/pause-resume/DB/scheduler/listener signals for success/error/cancel. | `go test -race ./internal/obs ./internal/agent/... ./internal/mcp ./internal/cron ./internal/db ./cmd/aura -run 'Test.*(Metric|Telemetry|Catalog|Observable|Shutdown)'` | Wave 0 meter/catalog recorder | ⬜ pending |
| 39-05-01..03 | 4 | OBS-04 | PromQL threshold/debounce/no-data fixtures; valid unique UIDs, queries, links, runbooks and provisioning; digest/internal-network checks; Compose smoke. | `pwsh -NoProfile -File scripts/verify-observability.ps1` then `docker compose --profile observability config` | Wave 0 immutable pack/verifier | ⬜ pending |
| 39-06-01..03 | 4 | OBS-05 | Deterministic dry-run token; disjoint bounded claims; crash-resumable mark/remove/finalize; all activity exclusions; 70/80/85 behavior; owner export manifest and export-delete ordering. | `go test -race ./internal/retention ./internal/conversations ./internal/documents ./internal/cron/handlers ./internal/agui ./internal/runner ./internal/share ./cmd/aura -run 'Test.*(Retention|Cleanup|Export|Delete|Disk|Owner)'` plus tagged disposable-DB suite | Wave 0 migration/adapters/clock | ⬜ pending |
| 39-07-01..03 | 4 | OBS-06 | 100k/30d seen set; 90d/512/10k writes and loads; exact UTF-8 hashing; empty/cap/TTL boundaries; deterministic newest-25% weighted selection; pinned exclusion; bounded compaction. | `go test -race ./internal/activelearn ./internal/reasoningstore ./internal/toolselectstore ./internal/learningretention ./internal/cron/handlers ./cmd/aura -run 'Test.*(Seen|Learning|Cap|TTL|Compact|Reservoir|UTF8)'` plus disposable-Neo4j suite | Existing stores partial; new bounded seams | ⬜ pending |

---

## Wave 0 Requirements

- [ ] 39-01-01/02/03: create the typed registry package, live-next migration/sqlc surface, and disposable-PostgreSQL concurrency harness.
- [ ] 39-02-01/02/03: create ingress operation-context fakes, gateway decision matrix, mutation-owner gate, and captured MCP envelope/reconnect tests.
- [ ] 39-03-01/02/03: add injectable clock/global deadline/snapshot/listener factory and semantic Compose healthcheck tests.
- [ ] 39-04-01/02/03: add manual/in-memory OTel readers, fake exporters, catalog descriptor recorder, and boundary observation fakes.
- [ ] 39-05-01/02/03: add Prometheus rule fixtures, dashboard/catalog/link/image/network verifier fixtures, and provisioning smoke harness.
- [ ] 39-06-01/02/03: add retention adapters, fake clock/IDs/activity sources, crash injection, adversarial filesystem, and export manifest verifier.
- [ ] 39-07-01/02/03: add seen-set fake clock, exact hash fixtures, deterministic sampler golden data, and disposable Neo4j compaction harness.
- [ ] During execution, copy the exact current lint and tagged integration commands from `.github/workflows/ci.yml`; the workflow may evolve after planning, so the live CI file remains authoritative.

---

## Manual-Only Verifications

All phase behaviors should have automated evidence. Operator-facing dashboard readability may receive a final human review, but it cannot substitute for JSON, UID, datasource, query, provisioning, and rule validation.

---

## Security Verification Minimums

- Cross-identity operation-key replay is impossible; same-key/different-payload returns a typed conflict.
- Concurrent duplicate reservation produces one effect owner; crash ambiguity becomes terminal `indeterminate` without automatic reinvocation.
- Metric descriptors reject identity, conversation/request/operation keys, raw errors, paths, prompts, arguments, and results as labels.
- Readiness responses remain bounded and sanitized when dependencies return credential/path-bearing errors.
- Cleanup revalidates ownership and activity after claim and immediately before deletion; symlinks and traversal inputs are rejected.
- Export manifests are owner-scoped, versioned, checksummed, and fully successful before combined export-delete begins deletion.

---

## Validation Sign-Off

- [x] Planner replaced requirement rows with exact task IDs, plans, waves, and commands.
- [x] All 21 tasks have `<automated>` verification and explicit Wave 0 dependencies where needed.
- [x] Sampling continuity: every task has an automated check; no gap exists.
- [x] Wave 0 maps every missing seam and fixture above to a task.
- [x] No watch-mode flags.
- [ ] Per-task feedback latency is measured and stays below 60 seconds.
- [x] `nyquist_compliant: true` set after the inline final plan map passed structure, dependency, decision, requirement, gap, API-coverage, and sampling-continuity checks.

**Approval:** approved 2026-07-21 (inline plan checker passed; Wave 0 remains execution-pending)
