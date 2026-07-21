---
phase: 39
slug: idempotency-observability-pack
status: draft
nyquist_compliant: false
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
| **Quick run command** | `go test ./internal/obs ./internal/agui ./internal/gateway ./internal/toolinvocations ./internal/activelearn ./internal/reasoningstore ./internal/toolselectstore` |
| **Full suite command** | `go test ./... && go vet ./...` plus the exact lint/integration commands copied from `.github/workflows/ci.yml` |
| **Estimated runtime** | Measure during Wave 0; keep per-task package loops below 60 seconds |

---

## Sampling Rate

- **After every task commit:** Run the narrow package/file command assigned to that task in its `<automated>` check.
- **After every plan wave:** Run `go test ./...` and all observability/config validators introduced by the completed wave.
- **Before `$gsd-verify-work`:** Run `go test ./...`, `go vet ./...`, repository lint/integration commands, Prometheus rule checks/tests, dashboard validation, and `docker compose config`.
- **Max feedback latency:** 60 seconds for the per-task loop; split slower integration checks into the wave gate.

---

## Per-Requirement Verification Map

| Requirement | Planned evidence | Test type | Automated command or contract | File exists | Status |
|-------------|------------------|-----------|-------------------------------|-------------|--------|
| OBS-01 | Occupied port fails boot; unexpected listener exit reaches top-level failure; Compose healthcheck targets `/readyz`. | unit + container contract | Targeted `cmd/aura` tests and `docker compose config` | ❌ Wave 0 listener seam | ⬜ pending |
| OBS-02 | Concurrent probes share one deadline; response is stable/sanitized; PG/Neo4j/schema/listener/scheduler/drain truth table; `/healthz` stays live. | unit + tagged integration | `go test ./internal/agui` plus DB-tagged readiness tests | ⚠ partial | ⬜ pending |
| OBS-03 | One MeterProvider feeds OTLP and Prometheus; bounded shutdown; required LLM/tool/MCP/DB/scheduler metrics; forbidden labels rejected; no duplicate legacy metrics. | unit + integration | `go test ./internal/obs` plus instrumented boundary package tests | ❌ Wave 0 metric recorder | ⬜ pending |
| OBS-04 | Recording/alert rules parse and fire correctly; dashboards have valid JSON, stable unique UIDs, valid datasources/queries, and provisioning smoke coverage. | config contract + smoke | `promtool check rules`, `promtool test rules`, dashboard validator, `docker compose config` | ❌ Wave 0 assets/validator | ⬜ pending |
| OBS-05 | Dry-run is side-effect free; apply detects drift; cleanup is bounded/non-overlapping; active conversations are excluded; failures retain metadata; export/delete ordering and checksums hold. | unit + tagged integration + adversarial FS | Targeted `internal/retention`, conversations, documents, and runner tests | ❌ Wave 0 adapters/clock | ⬜ pending |
| OBS-06 | TTL/cap boundaries, newest 25%, deterministic weighted selection, pinned exclusion, hard admission, bounded load/batches, cancellation, and metrics. | unit + Neo4j-tagged integration | Targeted `activelearn`, `reasoningstore`, and `toolselectstore` tests | ⚠ partial | ⬜ pending |

---

## Wave 0 Requirements

- [ ] Add a listener factory/error channel for deterministic bind and unexpected-exit tests.
- [ ] Add injectable readiness clock, one global deadline, and scheduler readiness snapshot seams.
- [ ] Add an OTel descriptor/attribute recorder that can assert bounded labels and duplicate metric names.
- [ ] Add retention storage adapters plus deterministic clock and ID sources for dry-run/apply/race tests.
- [ ] Add learning-store clock and seeded-random sources for exact TTL/cap/selection assertions.
- [ ] Add Prometheus rule fixtures and a Grafana dashboard/provisioning validator.
- [ ] Copy exact repository lint and tagged integration commands from `.github/workflows/ci.yml` into plan verification.

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

- [ ] Planner replaces requirement rows with exact task IDs, plans, waves, and commands.
- [ ] All tasks have `<automated>` verification or explicit Wave 0 dependencies.
- [ ] Sampling continuity: no three consecutive tasks without an automated check.
- [ ] Wave 0 covers every missing seam and fixture above.
- [ ] No watch-mode flags.
- [ ] Per-task feedback latency is measured and stays below 60 seconds.
- [ ] `nyquist_compliant: true` is set only after `$gsd-validate-phase` confirms the final plan map.

**Approval:** pending
