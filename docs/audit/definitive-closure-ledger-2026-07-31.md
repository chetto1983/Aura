# Aura definitive audit closure ledger

Date: 2026-07-31

This is the machine-checked reconciliation of every canonical finding under
`docs/audit`. Allowed states are `closed`, `superseded`, `retired`,
`external_blocked`, and `open`. `external_blocked` is disclosed and excluded
from the implementation score; it is never counted as passed. The companion
report is produced by `scripts/audit_closure_gate.py` and binds this ledger and
all source registers to the exact candidate commit by SHA-256.

| ID | State | Evidence | Verification |
|---|---|---|---|
| F-001 | closed | `2aa7c1e06` sandbox posture correction | sandbox contract and docker tier |
| F-002 | closed | `33bbda038` strict profile gates | destructive-shell profile tests |
| F-003 | closed | `344b1bae4` terminal exclusivity | capability terminal positive and negative |
| F-004 | closed | `5f7a8dd5b` atomic resume | pause-resume unit and DB integration |
| F-005 | closed | `3b4b1759a` rooted sidecar fence | traversal and sidecar regressions |
| F-006 | closed | `0e410ffe2` explicit hook-fault policy | hook fault branch tests |
| F-007 | closed | `406a1e75b` per-identity object resolver | MUSR Garage cross-deny E2E |
| F-008 | closed | `64d826894` bounded loop | capability and loop budget tests |
| F-009 | closed | `ccb302d58` send-file boundary | outside-workspace reject test |
| F-010 | closed | `b4365c394` file-mode preservation | fs write/edit mode tests |
| F-011 | closed | `dfd336105` decision observability | correlated structured-log tests |
| F-012 | closed | `e5b0b62d0` session concurrency bounds | agent runtime race tests |
| F-013 | closed | `0541a75fe` canonical MCP classification | MCP classification matrix |
| F-014 | closed | `6fc9d87dc` strict legacy-env gate | production-profile negative tests |
| F-015 | closed | `5e68b6659` CI package scope gate | `check_ci_go_packages_test.sh` |
| F-016 | closed | `72da6f0b0` typed config reparse | malformed env and strict-profile tests |
| F-017 | closed | `178cb0006` allowlist and UTF-8 cleanup | agent/tool regression suite |
| F-018 | closed | `33bbda038` credential/profile matrix | effective-behavior profile tests |
| F-019 | closed | `8a6b34320`, `9cd54126c`, `ee31c5a64` | four-plane DR, load/chaos, capability reports |
| F-020 | closed | `178cb0006` bounded runtime cleanup | focused runner regressions |
| F-021 | closed | `178cb0006` lifecycle cleanup | process and MCP lifecycle tests |
| F-022 | closed | `9322cdaa1` same-origin enforcement | CORS/origin negative tests |
| F-023 | closed | `178cb0006` runtime cleanup | focused configuration regressions |
| F-024 | closed | `dfd336105` run correlation | observability contract |
| F-025 | closed | ADRs 0037 and 0040-0044 | ADR presence and release checklist |
| F-026 | closed | `e07549506` deployment contract | strict production config validation |
| F-027 | closed | `03642a60f` canonical trust classification | mixed URL-command rejection tests |
| F-028 | closed | `5ef44e84f` fallback cleanup | focused agent/runtime tests |
| F-029 | closed | `5f7a8dd5b` transactional resume | DB integration atomicity |
| F-030 | closed | `5f7a8dd5b` transactional resume | duplicate-resume regression |
| F-031 | closed | `445713ae3` panic consequence pin | mutating-panic dispatch test |
| F-032 | closed | `5ef44e84f` session-bound background IDs | shell background ownership tests |
| F-033 | closed | `5ef44e84f` per-server mount deadline | MCP timeout and recovery tests |
| F-034 | closed | `5ef44e84f` bounded MCP frames | oversized-frame regressions |
| F-035 | closed | `9668c17e0` process-tree kill | grandchild-survival negative test |
| F-036 | closed | Phase 37 enforced egress floor | native docker integration and chaos |
| F-037 | closed | `f32892b5b` audited MCP writer | CLI mutation ledger tests |
| F-038 | closed | Phase 38 strict trust input | empty-body and schema negative tests |
| F-039 | closed | `7cecfaacc` conversation eviction fix | delete and in-memory eviction tests |
| F-040 | closed | `d101a87f6` orphan sidecar reconcile | age-grace cleanup tests |
| F-041 | closed | `18cc1ece4` absolute run-dir validation | cwd-independent sidecar tests |
| F-042 | closed | `1498623cf` detached admitted work | scheduler cancellation race tests |
| F-043 | closed | `1498623cf` handler/systemd budget alignment | static budget and backup cancellation tests |
| F-045 | closed | `7e402699c` owned waiter lifecycle | goleak and shutdown tests |
| F-046 | closed | Phase 38 live MCP probe | doctor/status live sidecar checks |
| F-047 | closed | `cd6cd8c61` loopback console bind | unsafe-bind rejection tests |
| F-048 | retired | `ddb4f3db6` accepted sidecar-search exclusion | explicit exclusion regression |
| F-049 | closed | retention manager and bounded stores | retention package tests and readiness |
| F-050 | closed | URL-token static gate | `check-no-url-tokens.sh --self-test` |
| F-051 | closed | `9a5321594` immutable supply-chain pins | workflow pin gate |
| F-052 | closed | `e89b6d4ef` strict JSON decode | privileged-route trailing/unknown-field tests |
| QA-A-01 | closed | `1b436593d` canonical args extraction | lint and canonicalization tests |
| QA-A-02 | closed | `9828f3c58` transient-error extraction | retry and stream tests |
| QA-A-03 | closed | Phase 32 boot composition cleanup | config overlay failure tests |
| QA-A-04 | closed | Phase 32 file split | 600-LOC gate |
| QA-A-05 | closed | Phase 32 env helper consolidation | lint and config tests |
| QA-A-06 | closed | Phase 32 truncation tests | focused tail/UTF-8 tests |
| QA-A-07 | closed | `c1824d48f` dead AgentTier removal | gateway agent-job floor tests |
| QA-A-08 | closed | typed config ownership | config catalog tests |
| QA-A-09 | closed | `f5440d551` tier decision and coverage | Telegram plus memory tier CI |
| QA-A-10 | closed | Phase 32 dead export cleanup | deadcode gate |
| QA-A-11 | closed | Phase 32 duplicate build removal | agent prompt tests |
| QA-A-12 | closed | Phase 32 request-ID cleanup | runner correlation tests |
| QA-B-01 | closed | Phase 32 shared Neo helpers | store parity tests |
| QA-B-02 | closed | Phase 32 shared GraphClient seam | compile and mock tests |
| QA-B-03 | closed | Phase 32 content hash extraction | hash parity tests |
| QA-B-04 | closed | Phase 32 numeric helper extraction | numeric round-trip tests |
| QA-B-05 | closed | `c1824d48f` UUID boundary consolidation | DB UUID boundary tests |
| QA-B-06 | closed | `c1824d48f` PostgreSQL text safety | NUL replacement tests |
| QA-B-07 | closed | transaction documentation correction | DB transaction regressions |
| QA-B-08 | closed | bounded int conversion | boundary and CodeQL tests |
| QA-B-09 | closed | config loader split | 600-LOC and config tests |
| QA-B-10 | closed | named 1-GiB default | config default parity test |
| QA-B-11 | superseded | strict profiles reject dev credentials | profile effective-behavior tests |
| QA-B-12 | closed | CI skip guard | tagged-tier CI contract |
| QA-B-13 | closed | secret marker consolidation | redaction matrix tests |
| QA-B-14 | retired | tool-select oracle became adaptive-policy internal state | adaptive policy tests |
| QA-B-15 | closed | title caps named | conversation title tests |
| QA-B-16 | closed | canonical branch sentinel protected | branch identity tests |
| QA-B-17 | closed | `c1824d48f` shared turn-cap default | config/store default parity |
| QA-C-01 | closed | Phase 32 strict decode consolidation | AG-UI body decoder tests |
| QA-C-02 | closed | shared envutil adoption | config and package tests |
| QA-C-03 | closed | Phase 38 canonical trust classifier | MCP trust matrix |
| QA-C-04 | closed | shared agentrender extraction | render parity tests |
| QA-C-05 | closed | Phase 32 transport helper extraction | web/MCP tests |
| QA-C-06 | superseded | bespoke memory settings replaced by managed MCP | memory composition tests |
| QA-C-07 | closed | throttle coverage added | web throttle unit tests |
| QA-C-08 | closed | setup ordering coverage added | token invalidation/SSE test |
| QA-C-09 | retired | asset intermediate states intentionally reserved | deadcode annotation and asset tests |
| QA-C-10 | closed | stdlib string helpers adopted | AG-UI tests |
| QA-C-11 | retired | deferred QR primitive removed from active surface | deadcode gate |
| QA-C-12 | closed | redundant blank import removed | build and tidy checks |
| QA-C-13 | closed | truncation helper consolidated | UTF-8 truncation tests |
| QA-C-14 | closed | transport antipattern removed | MCP lifecycle tests |
| QA-C-15 | retired | LOC watch was not a defect | current 600-LOC gate |
| QA-C-16 | closed | channel fallback coverage added | Telegram routing tests |
| QA-C-17 | closed | transport error path normalized | focused failure tests |
| QA-D-01 | closed | Phase 32 shared `getJSON` | frontend API tests |
| QA-D-02 | closed | Phase 32 shared focus trap | accessibility tests |
| QA-D-03 | closed | Login tests split | 600-LOC gate |
| QA-D-04 | closed | `a1e1a29b3` API helper consolidation | frontend unit tests |
| QA-D-05 | closed | shared Spinner adoption | component tests |
| QA-D-06 | closed | Stryker scope expanded | mutation gate |
| QA-D-07 | closed | scoped Go package helper | CI lint self-test |
| QA-D-08 | closed | skeleton ownership consolidated | knip and component tests |
| QA-D-09 | closed | `e3da4c503` strict boolean helper | JSON coercion tests |
| QA-D-10 | closed | immutable action pins | workflow pin gate |
| QA-D-11 | retired | placeholder modes removed from promised surface | shell mode unit tests |
| QA-D-12 | closed | `e3da4c503` reactive translation hook | language re-render test |
| QA-D-13 | closed | unused skeleton exports removed | knip gate |
| QA-D-14 | closed | Composer coverage added | Vitest coverage |
| QA-D-15 | closed | GPU compose override corrected | compose config validation |
| QA-D-16 | retired | remaining scripts are operator or hook entry points | Make/CI/hook reachability review |
| QA-D-17 | closed | push backstop fixed | path-filter negative fixture |
| QA-D-18 | closed | skeleton barrel cleaned | knip gate |
| QA-D-19 | retired | density watch was not a present cap violation | 600-LOC gate |
| SEC-001 | closed | `30e1d8a98` authenticated tenant scope | live forged-scope cross-deny |
| SEC-002 | closed | `30e1d8a98` tenant-session key | two-user same-session test |
| MCP-001 | closed | `9976f12c1` source-bound memory policy | alias and duplicate tests |
| MCP-002 | closed | `94e373d4b` cancellable lifecycle | MCP race/deadline tests |
| MCP-003 | closed | `e7d01a319` per-hop egress | strict-profile SSRF tests |
| ARC-001 | closed | `cfb907f77` hardened MCP reuse | knowledge lifecycle tests |
| PRIV-001 | closed | `9ee2e109f` owner purge | live purge postconditions |
| MEM-001 | closed | `40e6e0e90` atomic graph outcomes | injected rollback and live graph |
| MCP-004 | closed | `c6bb6f09b` typed domain outcomes | direct failure replay E2E |
| CON-001 | closed | `b2ec395dd` serialized canonical writes | 100-writer one-record E2E |
| CTX-001 | closed | `caaf33ae4` model context substitution | exact request regression |
| CTX-002 | closed | `36bc8535c` active-round protection | resume truncation tests |
| CTX-003 | closed | `c3ef04e6b` protected hook state | mutation/reorder tests |
| CTX-004 | closed | `b2ec395dd` final prompt serialization | exact final request budget tests |
| CTX-005 | closed | `423cccaf5` token config validation | impossible-budget negatives |
| MEM-002 | closed | `3b021257c` preference supersession | recall exclusion test |
| OBS-001 | closed | `3bba1007c` semantic memory readiness | outage/recovery readiness E2E |
| TST-001 | closed | capability, load/chaos and direct tool suites | all named cross-plane classes execute |
| REL-001 | closed | `40a216c9d` removed post-return tasks | goleak and lifecycle tests |
| SEC-003 | closed | `27efe86ea` capacity bounds | oversized input/result and HTTP 413 E2E |
| MEM-003 | retired | `bcda8e0b4` unsafe consolidation removed | absence plus retention tests |
| MEM-004 | closed | `b31267c68` aggregate provenance | exact provenance assertions |
| MEM-005 | closed | `b31267c68` aggregate cap | 8+8 bounded recall tests |
| CTX-006 | closed | `d819ed243` managed history bound | linear and branch >N tests |
| MCP-005 | closed | `f00f65d0b` reconnect drift suppression | add/remove/rename tests |
| MCP-006 | closed | `f00f65d0b` negotiation validation | malformed/version/capability tests |
| ARC-002 | closed | `3bba1007c` functional memory readiness | live semantic probe |
| ARC-003 | closed | `3bba1007c` shared process-owned client | lifecycle and recall tests |
| MCP-007 | closed | `8e614b480` CLI reservation/idempotency | replay and operation-context tests |
| PERF-001 | closed | `2fa42e6cc` pinned FastMCP measurement | direct p50/p95 and 100-writer metrics |
| R-01 | closed | `e111610b8` catalog failure mapping | terminal embedding failure test |
| R-02 | closed | `5c38bedfa` operator identity propagation | live CLI search |
| R-03 | closed | `10aca367e` metered degradation | non-2xx degraded signal and metrics |
| R-04 | closed | `24db0acde` CLI embedding composition | live ingest-to-embedding |
| R-05 | closed | `709fb713f` CLI catalog writer | owner catalog ready/failed E2E |
| R-06 | closed | `8fa655454` retrieval revision wiring | plan routing tests |
| R-07 | closed | `10aca367e` rerank config/telemetry | constructor and degraded tests |
| R-08 | closed | live graph anomaly counts both zero | read-only Neo4j postcondition |
| R-09 | closed | `1e5b62403` clears stale error | successful retry regression |
| R-10 | superseded | corrected T-1 retrieval contract | replacement test plan reviewed |
| R-11 | superseded | corrected T-1 corpus contract | replacement test plan reviewed |
| R-12 | superseded | corrected T-1 identity contract | replacement test plan reviewed |
| R-13 | superseded | corrected T-8 rerank contract | replacement test plan reviewed |
| R-14 | superseded | corrected T-8 fixture contract | replacement test plan reviewed |
| R-15 | superseded | corrected T-9 failure contract | replacement test plan reviewed |
| R-16 | superseded | corrected implementation targets | replacement test plan reviewed |
| R-17 | superseded | corrected latency method | direct cold/warm measurements |
| R-18 | superseded | corrected sampling method | direct repeated samples |
| R-19 | superseded | corrected cost model | measured local sidecar path |
| R-20 | superseded | corrected quality denominator | executed evidence only |
| R-21 | superseded | corrected deployment assumptions | live compose inspection |
| R-22 | retired | proposed custom GraphRAG path not adopted | managed MCP decision |
| R-23 | retired | proposed custom reranker rewrite not adopted | fail-soft metered design |
| R-24 | closed | current retrieval planner wiring | focused routing tests |
| R-25 | closed | current corpus revision wiring | revision validation tests |
| R-26 | retired | positive control, not a defect | direct expected-result probe |
| R-27 | closed | `709fb713f` XLSX coordinate fix | internal-empty-cell workbook fixture |
| R-28 | retired | positive control, not a defect | direct expected-result probe |
| R-29 | closed | `ee6dae630` query ownership fence | cross-owner retrieval test |
| R-30 | retired | obsolete proposed cleanup | current graph contract |
| R-31 | superseded | research option replaced by adopted design | ADR and PRD decision |
| R-32 | superseded | research option replaced by adopted design | ADR and PRD decision |
| R-33 | superseded | research option replaced by adopted design | ADR and PRD decision |
| R-34 | superseded | research option replaced by adopted design | ADR and PRD decision |
| R-35 | superseded | research option replaced by adopted design | ADR and PRD decision |
| R-36 | superseded | research option replaced by adopted design | ADR and PRD decision |
| R-37 | superseded | research option replaced by adopted design | ADR and PRD decision |
| R-38 | superseded | research option replaced by adopted design | ADR and PRD decision |
| R-39 | closed | `6f001bf97` bounded result shaping | retrieval bounds tests |
| R-40 | closed | `6ccbae7de` corpus revision correctness | revision regression |
| R-41 | retired | abandoned alternative test topology | current eval contract |
| R-42 | closed | `e3c3e72f9` truthful retrieval evidence | eval negative controls |
| BUG-1 | superseded | split into BUG-1A and BUG-1B | child findings reconciled |
| BUG-1A | retired | confirmed intentional agent-job approval policy | scheduler policy tests |
| BUG-1B | closed | `2c873863e` approval reminder delivery | pending-approval liveness tests |
| BUG-2 | closed | `dd0e164a8` Telegram notify route | scheduler delivery tests |
| BUG-3 | closed | `3fb13edee` memory forget | direct create-forget E2E |
| BUG-3b | closed | `8e614b480` task ownership visibility | linked operation/replay tests |
| BUG-4 | superseded | Agent.md surface retired for managed Memory | managed memory direct E2E |
| BUG-5 | retired | behavioral preference, not a correctness defect | capability workflow completion |
| BUG-6 | superseded | split into BUG-6a and BUG-6b | child findings reconciled |
| BUG-6a | retired | `e5b557f0c` compaction engine removed | absence and migration tests |
| BUG-6b | retired | compaction provider path removed | absence and default-loop tests |
| BUG-7 | retired | compaction UI removed with subsystem | frontend deadcode gate |
| BUG-8 | closed | context gauge fixed before compaction removal | gauge regression |
| BUG-9 | closed | `c4504cf8` hidden-cache walk pruning | real-agent glob/grep E2E |
| VW-01 | retired | Linux production path proved bounded; Windows-only hypothesis unconfirmed | WSL race and MCP timeout E2E |
| VW-02 | closed | `e8c1fa39` semantic JSON compare | DB integration scheduler test |
| VW-03 | closed | production-resolved MCP probe timeout | doctor direct timeout evidence |
| VW-04 | closed | `431d2a8de` package-aware tagged compile | 29 tiers across 37 packages |
| REL-002 | closed | `28851f7b6` portable workflow-pin self-test | Ubuntu self-test and real pin gate |
| REL-003 | closed | `962e94180` portable tagged-tier self-test | Ubuntu 29-tier / 37-package compile |
| REL-004 | closed | `accb7b8c3` explicit Neo4j helper shell | four-plane DR reaches offline dump/load |
| REL-005 | closed | `febfeb35c` Compose-resolved sidecar source volume | local and GitHub four-plane checksum drill |
| REL-006 | closed | readiness daemon boot env and fail-fast diagnostics | workflow contract and rollback diagnostic tests |
| REL-007 | closed | loopback rollback web-auth compatibility env | current and previous image boot contract |
| EXT-001 | external_blocked | no authorized Calendar provider account | 14 direct calls; no-account failures honest |
| EXT-002 | external_blocked | no authorized email test account/recipient | send-email failed closed without delivery |
| EXT-003 | external_blocked | WhatsApp bridge waiting for QR pairing | 14 direct calls; unpaired failures honest |

## Current closure evidence

- Agent Memory direct HTTP MCP: 14 tools mounted; 4 isolated writes; 4 unique
  nodes; warm search p95 155.3 ms; exact cleanup and empty postcondition.
- Web fetch: real pages complete in 0.44-3.07 s; configured 10 s timeout fires
  at 10.024 s; 335,275-byte result reconstructs exactly via continuation.
- Concurrency: 100 writers in 533.3 ms (187.5 writes/s), one canonical fact and
  one canonical preference, zero leaked fixture users.
- Load: concurrency 32; health p95 0.738 ms; readiness p95 50.396 ms; 100%
  success; peak RSS 81,428,480 bytes under 512 MiB.
- Chaos: DB, MCP, Garage, and process-kill scenarios executed, recovered, and
  produced zero false readiness, duplicates, or promoted partial artifacts.
- DR: all four planes checksum-verified and cleaned; total RTO 18,643 ms;
  maximum measured RPO 12 s.
