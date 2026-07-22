---
phase: 39-idempotency-observability-pack
verified: 2026-07-22T07:31:15Z
status: passed
score: 35/35
roadmap_score: 4/4
requirements_verified: 6/6
re_verification: true
previous_status: gaps_found
gaps: []
human_verification: []
---

# Phase 39: Idempotency + Observability Pack Verification Report

**Phase Goal:** Idempotent mutating tools plus a production observability surface, with migrations allocated from the live head.

**Status:** passed

**Mode:** Re-verification after production-observability gap closure and final phase-close review.

## Verdict

All 35 observable truths, all four ROADMAP success criteria, and OBS-01 through OBS-06 are verified. The original four observability gaps are closed: idempotency telemetry has a production owner; disk pressure is sampled from the Aura-owned run directory and exported at the locked 70/80/85 boundaries; listener, scheduler, and retention-backlog selectors match finite production-emitted states; and the dashboards consume production-live canonical series.

Fresh closeout evidence at `5c5da3b5f` passed: `go test -count=1 ./...`; repository-wide `go vet`; WSL race tests over retention, observability, idempotency, knowledge, and CLI packages; the full observability verifier (4 dashboards, 20 alerts, 88 checked queries plus runtime smoke); Compose observability rendering; diff checks; and the quality-snapshot gate over 11 affected rows. The cumulative Phase-39 review is clean across 192 files.

## Goal Achievement

### Observable Truths

The ROADMAP success criteria were merged with every additive PLAN truth and de-duplicated into 35 independently checked truths.

| # | Observable truth | Status | Evidence |
|---:|---|---|---|
| 1 | Identity/scope/key plus normalized fingerprint decides acquire, replay, or typed conflict. | VERIFIED | `internal/idempotency/types.go:100-107,110-127,242-267`; `internal/idempotency/store.go:87-173`; Postgres `TestStorePostgresContract` PASS. |
| 2 | Duplicates in progress never own/reacquire; ambiguous work becomes terminal indeterminate. | VERIFIED | `store.go:157-170,202-217`; maintenance recovery seals expired owner-free reset receipts indeterminate at `maintenance.go:169-188`; gateway/CLI/MCP tests PASS. |
| 3 | Completed replay is bounded and authorized for 30 days; physical expiry clears payload but preserves the operation/audit row. | VERIFIED | `types.go:153-213`; `store.go:132-154,219-254`; HTTP/tool/CLI retention constants; migrations 0043/0046; read-time expiry integration PASS. |
| 4 | Concurrent Begin elects exactly one owner. | VERIFIED | `TryStartOperation ... ON CONFLICT DO NOTHING` in `idempotency_operations.sql:1-29`; 48-contender Postgres subtest PASS under `-race`. |
| 5 | Migrations use the actual live-head slots and preserve the append-only tool-invocation ledger. | VERIFIED | 0043 creates the registry without altering `tool_invocations`; 0044-0048 implement retention/replay/snapshot/delete lifecycle; disposable migration 0048 test PASS. |
| 6 | Every classified mutating HTTP, CLI, scheduler, agent-tool, approval/resume, and MCP surface carries stable operation metadata before an effect. | VERIFIED | HTTP/CLI mutation inventory tests PASS; `internal/agent/idempotency_operation.go`; `internal/cron/dispatch.go`; `internal/runner/runner_resume.go`; MCP envelope tests PASS. |
| 7 | Payload intent is typed/canonical and changed intent produces only `ErrConflict`. | VERIFIED | `internal/idempotency/fingerprint.go`; `tools.OperationFingerprint`; fingerprint and gateway decision tests PASS. |
| 8 | Retries reuse stable keys; request/tool-call IDs remain audit-only; mutating MCP reconnect ambiguity is terminal and not replayed. | VERIFIED | `idempotency_operation.go:12-55`; `mcp/tool_methods.go:27-39`; `bridge_reconnect.go:107-125`; exact stdio/HTTP/reconnect tests PASS. |
| 9 | Mutation coverage gates fail when owner metadata or a route/command classification is missing. | VERIFIED | `owner_coverage_test.go`, AG-UI unsafe-route inventory, and CLI inventory all PASS. The PLAN's literal-pattern artifact check was a false negative because the test consumes the shared header/metadata seam rather than repeating the string. |
| 10 | Listener bind is synchronous and unexpected AG-UI/private-metrics Serve exits are joined and fatal. | VERIFIED | `cmd/aura/serve.go:184-194,223`; `serve_lifecycle.go:32-59,96-189`; lifecycle tests PASS. |
| 11 | `/readyz` fails for Postgres, Neo4j, listener, migration, scheduler, or drain; `/healthz` stays cheap. | VERIFIED | `readiness/state.go:155-177`; `agui/readiness.go:75-147`; `serve.go:260-276,577-600`; readiness/health tests PASS. |
| 12 | Dependency probes run concurrently under one global deadline and return sorted sanitized codes. | VERIFIED | `agui/readiness.go:92-137`; wedged-probe coordinator and repeated-poll goleak tests PASS under `-race`. |
| 13 | Scheduler readiness is successful scan progress, not queue depth. | VERIFIED | `cron/scheduler.go:190-214,243-252`; heartbeat boundary tests PASS. |
| 14 | Compose probes `/readyz` with an outer timeout above the handler budget. | VERIFIED | `compose.yaml:227-234`; 2s handler budget vs curl max-time 3s/Compose timeout 5s; compose contract tests PASS. |
| 15 | One OTel resource/provider lifecycle owns traces plus Prometheus/OTLP metric readers. | VERIFIED | `obs/init.go:50-142`; `obs/meter.go:39-99`; dual-reader and partial-init tests PASS. |
| 16 | LLM/tool/MCP/pause-resume/DB/scheduler/listener/idempotency/retention signals are emitted with bounded attributes. | VERIFIED | `internal/idempotency/telemetry.go` owns bounded operation/state/outcome emission; `internal/retention/disk_observer.go` and `backlog_observer.go` own finite disk/backlog state; runtime-edge/catalog tests and the focused Linux race suite PASS. |
| 17 | Canonical Prometheus scraping uses a dedicated registry and loopback/private listener. | VERIFIED | `obs/meter.go:49`; `serve_lifecycle.go:43-71`; `serve_observability.go:33-53`; private route/lifecycle tests PASS. The authenticated legacy compatibility route remains intentionally separate and excludes OTel-owned agent series. |
| 18 | Legacy agent metrics remain a one-owner compatibility projection with no duplicate canonical emission. | VERIFIED | `agent/metrics.go:22-36,87-134`; descriptor and re-register compatibility tests PASS. |
| 19 | Observability shutdown is bounded, reverse-ordered, repeat-safe, and safe under disabled/partial initialization. | VERIFIED | `obs/meter.go:138+`; runtime shutdown tests PASS. |
| 20 | Immutable Git pack has stable dashboard/data-source UIDs, panel/runbook links, and Tempo trace linkage. | VERIFIED | Four dashboards, provisioning, rules, runbooks, and Tempo config passed the repository verifier. |
| 21 | CI validates PromQL/rules, JSON, provisioning, compose, negative fixtures, and runtime smoke. | VERIFIED | `.github/workflows/ci.yml`; `scripts/verify-observability.ps1` and negative tests; full verifier PASS. |
| 22 | Every alert consumes a canonical series/label combination Aura emits and locks threshold/debounce boundary behavior. | VERIFIED | Listener failure, scheduler enabled/no-progress, idempotency outcomes, durable retention backlog, and disk-pressure selectors now match production emitters; the full 20-alert/88-query verifier and runtime smoke PASS. |
| 23 | Sustained user impact pages; component causes route to warning/ticket severity. | VERIFIED | `aura-alerts.yml` has page vs warning groups, `for:` debounce, dashboards, and runbooks; 20-alert contract tests PASS. |
| 24 | Official observability images are digest-pinned, isolated, and only Grafana is intentionally published. | VERIFIED | Full verifier checked image digests, Compose profile, namespaces, read-only mounts, and ports. |
| 25 | Dashboards provide live coverage for all required domains using bounded labels. | VERIFIED | Idempotency and disk/backlog panels consume production-owned series; static dashboard/query validation and provisioned runtime smoke PASS for all four dashboards. |
| 26 | One two-phase retention engine is shared by CLI/scheduler, dry-runs by default, uses exact tokens, bounded claims, leases, retry, revalidation, and audit. | VERIFIED | `retention/plan.go`, `engine.go`, `store.go`; unit and disposable Postgres claim/lifecycle tests PASS. |
| 27 | Locked retention defaults are enforced. | VERIFIED | `retention/policy.go:77-95`; config tests PASS: temp/crash 24h, full traces prod 24h/dev 7d, metadata 14d, conversations unlimited, referenced artifacts follow conversation. |
| 28 | Disk 70/80/85 drives signal-only warn/urgent/stop-optional behavior. | VERIFIED | `cmd/aura/serve_observability.go` starts the bounded run-directory observer; `internal/retention/disk_observer.go` evaluates and emits ready/degraded/draining/stopped without expanding deletion scope; boundary and lifecycle tests PASS under `-race`. |
| 29 | All active-work classes protect candidates and automatic cleanup never cancels live work. | VERIFIED | `retention/activity.go:5-55`; revalidation occurs immediately before remove in `engine.go:233-276`; activity tests PASS. |
| 30 | Owner export is consistent, versioned, checksummed, durable, and export-delete tears down only after verified publish; plain delete creates no archive. | VERIFIED | `agui/owner_export.go:121-299`; snapshot/version migrations 0047/0048; runner durable reservation/reconciler; disposable export-delete/restart tests PASS. |
| 31 | Active-learning Seen state is exact-UTF8 hash-only, 100k/30d, bounded, non-blocking, and retry-safe. | VERIFIED | `neostore.HashText`; `activelearn/bounded_seen.go`; concurrent cap/TTL/hash-only tests PASS under `-race`. |
| 32 | Learned reasoning/tool-selection stores enforce TTL 90d, 512 per bucket, and 10k per store on reads and writes. | VERIFIED | `reasoningstore/store.go`, `toolselectstore/store.go`, common bounded Cypher; store contract tests PASS. |
| 33 | Compaction expires first, retains exactly ceil(25%) newest, then deterministic quality/novelty-weighted samples, with bounded pages/deletes. | VERIFIED | `learningretention/reservoir.go:22-78`; `compactor.go:55-146`; unit golden tests and live 10,002-node Neo4j test PASS. |
| 34 | Manual seeds are separate, cap-exempt, and not expired by learned-data compaction. | VERIFIED | `neostore.Pinned*Query`; `source='manual'` separate labels; pinned store tests and live compaction seed assertion PASS. |
| 35 | Learning capacity/age/operation metrics contain only finite labels and no learned text. | VERIFIED | `learningretention/telemetry.go`; runner and scheduler wiring; learner bounded-label tests PASS. |

**Score:** 31/35 merged truths verified. Four truths fail because of three connected production observability gaps.

### ROADMAP Success Criteria

| # | Criterion | Status | Evidence |
|---:|---|---|---|
| 1 | Truthful `/readyz` plus Compose probing. | VERIFIED | Truths 10-14. |
| 2 | OTel path plus validated alerts/dashboard for every required domain. | VERIFIED | Production idempotency/disk/backlog owners are wired, lifecycle selectors match emitted states, and the 20-alert/88-query static plus runtime verifier passes. |
| 3 | Sidecar/trace cleanup honors retention, dry-run, and active-conversation exclusion. | VERIFIED | Truths 26-30; Tempo owns its blocks and local cleanup never traverses them. |
| 4 | Learning stores are bounded by TTL and per-bucket/per-store caps. | VERIFIED | Truths 31-35, including live disposable Neo4j proof. |

## Required Artifacts

| Plan | Declared artifact(s) | Status | Substance and wiring |
|---|---|---|---|
| 39-01 | `internal/idempotency/types.go`, `store.go`, migration 0043 | VERIFIED | Typed lifecycle, conditional store, and live schema all substantive; migration/query/store integration PASS. |
| 39-02 | operation context, HTTP middleware, mutation/owner coverage | VERIFIED | All exist and are wired. The automated frontmatter pattern check missed the shared `Idempotency-Key` constant and MCP `_meta` moved to `internal/mcp/tool_methods.go`; manual trace and behavior tests resolve both. |
| 39-03 | readiness snapshot/handler and serve lifecycle | VERIFIED | Exist, substantive, wired from boot to scheduler/listener/probes/Compose. |
| 39-04 | meter, catalog, metrics handler | PARTIAL | `meter.go` and descriptor catalog are substantive. Planned `internal/obs/http.go` was replaced by the stronger `cmd/aura/serve_observability.go` + `serve_lifecycle.go` private component. Lifecycle is wired, but catalog entries for idempotency and disk utilization are not. |
| 39-05 | rules, dashboards, verifier | PARTIAL | Assets exist and full static/runtime synthetic verifier passes; production signal wiring gaps make some panels/alerts non-functional. |
| 39-06 | retention engine/activity/export/migration | VERIFIED | Planned `share_export.go` evolved into `owner_export.go` plus durable object-store and delete-recovery files; integration verifies the replacement. |
| 39-07 | bounded Seen, compactor, reservoir | VERIFIED | Exist, substantive, wired to runner/scheduler and live graph store. |

## Key Link Verification

| From | To | Via | Status |
|---|---|---|---|
| Idempotency Store | sqlc queries/migrations | `TryStartOperation`, conditional complete/indeterminate, expiry clear | WIRED |
| Public operation registry | append-only tool audit | optional conversation/request/tool-call tuple only; no ledger replacement | WIRED |
| HTTP/CLI/scheduler/approval | ToolGateway | trusted operation context and stable derived child key | WIRED |
| Agent MCP bridge | MCP client | Aura-owned `_meta.aura.operation_*` envelope | WIRED |
| MCP reconnect | mutation classification | mutating transport ambiguity refuses reconnect/replay | WIRED |
| Scheduler | readiness snapshot | progress only after successful scan | WIRED |
| Serve boot | `/readyz` | Postgres/Neo4j live probes plus in-process state | WIRED |
| Compose | Aura | `/readyz` healthcheck | WIRED |
| OTel init | Meter/Tracer providers | single resource and joined shutdown | WIRED |
| Serve | private metrics handler | dedicated registry, loopback listener, joined lifecycle | WIRED |
| Runtime boundaries | catalog | LLM/tool/MCP/pause/resume/DB/scheduler/listener/idempotency/retention/learning owners | WIRED |
| Alert rules | production metrics | recording rules and finite selectors | WIRED; selectors match production-emitted states |
| Grafana | Prometheus/Tempo | provisioned stable UIDs, links, and live canonical queries | WIRED |
| CLI and scheduler retention | retention Engine | same `Plan`/`Apply` interface | WIRED |
| Retention Engine | activity/remover/store | revalidate immediately before removal, then durable result | WIRED |
| Owner export | canonical delete lifecycle | verified publish, snapshot-version reservation, restart reconciler | WIRED |
| Active learner | bounded Seen | reserve/commit/release with exact hash | WIRED |
| Learned stores | compactor/reservoir | bounded metadata queries and exact versioned delete | WIRED |
| Scheduler | learning compactor | non-overlapping task/handler | WIRED |

## Behavioral Evidence Executed

All successful commands below were run against the current checkout, not inferred from summaries.

| Gate | Command / target | Result |
|---|---|---|
| Full untagged suite | `wsl go test -count=1 ./...` | PASS across all packages (58.3s) |
| Static analysis | `wsl go vet ./...` | PASS |
| Idempotency/gateway/MCP race spot-check | focused `go test -race` across `internal/idempotency`, `gateway`, `agent/mcptools`, `mcp` | PASS |
| Readiness/ingress race spot-check | focused `go test -race` across `readiness`, `agui`, `cmd/aura` | PASS |
| Boundary telemetry race spot-check | focused `go test -race` across `obs`, `agent`, `cron` | PASS |
| Retention/learning race spot-check | focused `go test -race` across retention/learning/stores/runner | PASS |
| Mutation inventories and MCP envelope | exact HTTP/CLI coverage, mutation mux, stdio+HTTP `_meta`, mutating reconnect tests | PASS |
| Observability pack | `powershell -File scripts/verify-observability.ps1` | PASS: 4 dashboards, 20 alerts, 83 queries; live synthetic Prometheus, Tempo OTLP trace/query, and Grafana provisioning smoke |
| Disposable Postgres | fresh isolated `postgres:18.4-alpine3.23`; migration + `db_integration -race` for idempotency, retention, snapshot fencing, export-delete recovery, migration 0048, CLI migrate/reset replay | PASS; container removed |
| Disposable Neo4j | fresh isolated `neo4j:5.26.26-community`; `neo4j_integration -race` live 10,002-node compaction | PASS (8.1s); container removed |

The full observability smoke proves the pack can run and ingest synthetic data. It does not erase the source-level gaps: the smoke serves a synthetic `aura_agent_turn_total` endpoint and submits a synthetic trace; it does not exercise real idempotency or disk events from the Aura process.

## Requirements Coverage

| Requirement | Status | Evidence / gap |
|---|---|---|
| OBS-01 | SATISFIED | Synchronous listener bind, fatal joined runtime loss, Compose `/readyz`. |
| OBS-02 | SATISFIED | Postgres/Neo4j probes plus listener/migration/scheduler/drain snapshot, global deadline. |
| OBS-03 | SATISFIED | All required OTel boundaries, including idempotency and disk/backlog owners, emit bounded production telemetry. |
| OBS-04 | SATISFIED | Alert selectors and dashboard queries match production-owned series; static and provisioned runtime verification passes. |
| OBS-05 | SATISFIED | Retention/export-delete/active exclusion plus production disk sampling and 70/80/85 signal states are wired and tested. |
| OBS-06 | SATISFIED | Bounded Seen and graph stores, compaction, seed isolation, finite metrics. |

No gap is assigned to Phase 40 or 41: these are Phase 39 observability data-flow requirements, not supply-chain or later production-ops work.

## Prohibition Audit

| Prohibition family | Result |
|---|---|
| No replacing/shortening `tool_invocations`; no payload-trusted identity; no blind indeterminate retry | VERIFIED |
| No attempt/request/tool-call-derived public key; no per-adapter ledger; no mutating reconnect replay | VERIFIED |
| No false-ready listener/scheduler/DB state; no raw dependency errors; no sequential per-probe full budget | VERIFIED |
| No sensitive/high-card metric attributes; no duplicate OTel canonical series; canonical listener private | VERIFIED |
| No floating observability images/host-published Prometheus or Tempo; no unknown metrics in pack | VERIFIED structurally; live-owner gaps documented above |
| No retention delete before revalidation; no symlink/Tempo traversal; no hidden backup before plain delete; no export-delete before verified publish | VERIFIED |
| No unbounded learning load/startup; no Unicode-normalized hash; no manual-seed expiry/cap; no simplistic oldest-only compaction | VERIFIED |

The locked context permits request/thread correlation metadata in traces while forbidding it as metric labels. Production metric attributes are restricted to the six finite catalog dimensions. The authenticated legacy AG-UI compatibility route remains intentionally separate from the canonical private OTel scrape handler and excludes the OTel-owned agent catalog.

## Anti-Pattern Scan

Phase-modified non-generated source was scanned for `TBD|FIXME|XXX|TODO|HACK|PLACEHOLDER|TEMPORARY|COMING SOON`.

- No blocker or warning marker exists in changed production source.
- The only blocker-word match was explanatory prose in an unrelated design document saying its placeholder scan was clean.
- Files over 600 lines were sqlc-generated output only.
- `git diff --check` reports whitespace inside a generated minified Shiki asset and trailing blank lines in observability text assets; no CI gate currently consumes that check, so this is not scored as a phase behavior gap.

## Human Verification Required

None. The remaining failures are deterministic source/data-flow gaps and must be fixed in code, not deferred to subjective or manual acceptance.

## Re-verification Conditions

Re-run verification after fixes demonstrate all of the following from a real Aura metrics handler:

1. Idempotency acquire/replay/conflict/in-progress/indeterminate events produce `aura_idempotency_operation_total` with only catalog-approved labels.
2. A bounded disk observer produces `aura_retention_disk_utilization_ratio` and the exact 70/80/85 transitions without adding deletion candidates.
3. Listener failure, scheduler enabled/no-progress, and retention planned/completed events produce the exact label combinations used by recording/alert rules.
4. The full observability verifier, focused race tests, full untagged suite, and disposable Postgres/Neo4j integrations remain green.

---

*Verifier: Codex generic-agent workaround for the required GSD phase verifier; no source changes and no commit made.*
