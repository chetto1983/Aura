# Aura — Consolidated Fix Plan

Plan date: 2026-07-20
Supersedes-as-execution-index: `operator-reported-runtime-bugs-2026-07-18.md` (BUG-1..7) + `live-attack-plan-2026-07-18.md` (Waves 0–3).

## Provenance

This is the single execution truth-source that reconciles the two 2026-07-18 audits, folds in a 2026-07-20 **adversarial code-verification pass** (17 read-only verifier agents, workflow `wenpc28z1`; default stance "the finding is wrong until the code proves it"), records the **operator decisions**, and captures a **newly-discovered bug (BUG-8)** found this session by live DB inspection and already shipped.

- **Verification outcome:** all 17 load-bearing reliability/operator-bug findings came back **CONFIRMED** or **CONFIRMED_NARROWED** — **zero REFUTED**. No fix in this plan rests on an unverified claim. Three audit fix-directions were corrected in flight (see 0.1, 0.2, 2.1).
- **Ordering axis:** reliability-first — make every exposed feature work E2E before hardening.
- **Sequencing constraint:** the **compaction SPIKE** is the one hard dependency (gates 0.1's scope, 1.9, 2.3).

## Decisions (locked 2026-07-20)

1. **Compaction engine** → ~~run the SPIKE first~~ ~~SPIKE RESOLVED = REMOVE~~ **✅ REMOVAL SHIPPED 2026-07-20** (operator-ratified, PRD Amendment #86). The dark Phase 42 `llm-conversation-compaction` engine is **DELETED** on `master` (`22610817` PRD → `e5b557f0` Go/db 58 files + de-wire + **migration `0042_drop_compaction`** + sqlc regen → `28c12351` web UI + bundle → `826a92e8` docs+CI re-scope → `22f043f7` quality). **1.9/2.3 = NO-GO (superseded — nothing left to wire/mount)**. The anti-rot core is **L4 extractive graph memory** — L4 archival recall SHIPPED (`AURA_CONTEXT_MEMORY_RECALL`, `340d6966`) and **E2E-proven live** post-removal (`memory_get_facts` in a real cockpit turn, 0.08s). Verified: coverage 85.5% (WSL db+neo4j) + `TestMigrateSteps` 0042 up/down/up reversibility + real E2E (gauge 13.2k/1%, cache 93%, graph 3n/2e, 0 errors); 0042 applied to the live DB (0 compaction tables, no data lost, no BUG-6a crash on boot). ADR: `docs/audit/compaction-spike-2026-07-20.md`.
2. **Missed reminders (1.2)** → **catch-up-once on restart** (parity with agent_job/backup); fix the false doc comment.
3. **AG-016 force-gate** → **redesign as three-tier** (Comet *prohibited / explicit-permission / regular* + Poke's honor-prior-confirmation). Recommended pending final ratification; carries a PRD amendment. Research + rationale in the AG-016 section below.

## BUG → wave map

| Operator BUG (2026-07-18) | Lands as | Wave | Status |
|---|---|---|---|
| BUG-6a evaluator crash | 0.1 + 0.2 | **0** | verified; **live-confirmed on 2026-07-20 boot** |
| BUG-6b OpenRouter errors | 1.10 + 1.11 | 1 | verified |
| BUG-1B approval undelivered | 1.7 (reframed: relay-liveness) | 1 | verified |
| BUG-3 memory no delete | 1.8 | 1 | verified |
| BUG-2 no Telegram notify | 2.1 | 2 | verified (fix-direction corrected) |
| BUG-7 compact UI orphaned | 2.3 → REMOVED | 2 | ✅ removed (`28c12351`, engine deleted) |
| BUG-4 no profile-edit tool | 2.4 | 2 | verified |
| BUG-3b memory→task cascade | 2.8 | 2 | verified (needs migration + PRD) |
| BUG-5 superfluous tool calls | prompt-tuning task | side | behavioral |
| BUG-1A AG-016 force-gate | AG-016 redesign | 1 | verified; policy decision |
| — (new, this session) | **BUG-8 context gauge** | done | **✅ SHIPPED + DEPLOYED** |
| — (regression, post-removal) | **cockpit scheduler layout** — action buttons squeezed the schedule literal to a zero-width span | done | **✅ SHIPPED `9371e162`** (this was the "Web-E2E flake" — it was a deterministic failure, not a flake) |
| — (agent-found, post-removal) | **BUG-9 fs_glob/fs_grep silent-empty** — home caches exhaust the walk budget before user files | done | **✅ SHIPPED `c4504cf8`** + real-agent E2E |

Two root patterns, not 40 bugs: **A — built-but-not-wired (dark code)**, **B — silent death/loss (no dashboard)**. Attacked via three cross-cutting fixes woven through Waves 0–1: per-tick self-heal (0.2), honest readiness (1.4), deadlines on every blocking external read (0.3 + 1.6).

## Shipped after the compaction removal (2026-07-20 PM session)

Three fixes landed on `master` on top of the removal, all E2E-verified on the redeployed `aura:local` container:

1. **Cockpit scheduler layout regression** (`9371e162`, CI fully green) — the path-gated **Web-E2E** job (skipped on Go/scripts-only commits) went red the moment the removal's bundle rebuild re-triggered it. Root cause: `SchedulerBoard`'s always-inline operator buttons squeezed the `1fr` body until the `shrink-0` next-fire timestamp overflowed and collapsed the cron `truncate` span to **zero width** (Playwright `hidden`). Deterministic on desktop **and** mobile — NOT the "flake" the intervening handoff assumed (CI on `b6aeabba` failed). Fix: a compact always-visible **44px icon action rail** (WCAG 2.5.8; icon-only + `aria-label`) + one-truncation-context schedule line. Verified: vitest 1571, governance E2E 12/12 (chrome+mobile), full sweep 72/72, dist built via the docker webbuild stage to satisfy web-dist-freshness. **New reference:** the committed `internal/webui/dist` must be built on Linux node-24 (docker webbuild / CI) — a Windows Vite build re-hashes every chunk.
2. **`windows-unit` CI lane removed** (`0dcf6029`) — Windows-native is unsupported (container/Ubuntu/DGX-Spark only), so the `windows-latest` O-07 lane no longer gates a supported platform.
3. **BUG-9 — `fs_glob`/`fs_grep` silent-empty** (`c4504cf8`) — **found by the operator driving the agent** (a real system test), recorded as a `has_bug` Fact in the agent-memory graph. `skipWalkDir` only skipped `.git/node_modules/vendor`, so a walk of `/root` descended `/root/.cache` (66,646 files on the appliance); `WalkDir` visits dot-dirs first, exhausting the 50k node budget inside `.cache` before reaching the operator's top-level files → "[no matches] + walk truncated" while `shell_exec ls` saw them. Fix: prune hidden dot-dirs (ripgrep/fd default) + `__pycache__`, guarded so an explicit hidden `path` root still descends; plus a fix-on-touch for `grepFile` never checking `scanner.Err()` (a >1 MiB line silently truncated the scan). Verified by a **real agent turn** on the container (agent ran `fs_glob(path:/root, pattern:test_aura*)` → returned both files, no truncation). **Method note:** agent-driven, real-scenario testing is now the primary bug-discovery path — mine the agent-memory graph for `has_bug` Facts (`MATCH (f:Fact) WHERE f.predicate='has_bug'`).
4. **Wave 1.10 — llama.cpp token estimator** (`c051c64d`) — see the Wave-1 table row (DONE). First forward Wave-1 item after the removal.
5. **Memory-tool hardening** (`3fb13ede` + `2ddccf26`) — operator-directed, in the vendored agent-memory fork (`docker/agent-memory/`) + Aura wiring, all live-verified on the redeployed sidecar:
   - **Dedup**: `add_preference` gained an exact-text fallback (`FIND_EXACT_PREFERENCE`) closing the hole where the embedding path is skipped and an identical preference duplicated (operator saw the `tool_search` preference as 2 nodes). *Live: identical preference twice → same id, count 1.*
   - **Delete (Wave 1.8 partial / BUG-3)**: new `memory_forget` (ownership-scoped, refuse-not-no-op, returns removed id) for `preference | fact | entity`. Entity forget is **non-cascading** (unlink ownership; delete node only if fully orphaned — never `DETACH DELETE` a shared entity). `aura memory` CLI gained `forget`; batched `save_profile` was designed but NOT built (parked with the supersession).
   - **Prune**: removed the reasoning-trace quartet + `memory_export_graph` (redundant with Aura's own reasoning store) and **`graph_query`** (unscoped arbitrary Cypher = cross-user exfiltration surface; already fork-disabled for scoped sessions, so dead in Aura; operator keeps `aura neo4j cypher read/write`). Clean 12-tool `memory_*` surface remains.
   - **System prompt** (`internal/agent/prompt.go`): explicit load-deferred-tool rule (`tool_search("select:<name>")` before calling — the operator's anti-loop rule, now permanent) + `<profile_context>` superseded off Agent.md onto the memory graph.
   - Full 1.8 `memory_manage` (update/list_by_type) + BUG-3b memory→task cascade (2.8) remain open.

---

## ✅ BUG-8 — Context gauge showed cumulative session tokens as context fill (SHIPPED)

**Not in either audit doc.** Found 2026-07-20 by live DB inspection: conversation `03b9c7c2` showed `sum(input_tokens)=620201` but `max(input_tokens)=29817` — the footer's "CONTESTO 620k / 1M · 62%" was the *lifetime cumulative* input sum mislabeled as *current* context fill (real fill: 29,468 = ~2.9%). Root cause `RuntimeFooter.tsx:90` fell back to `session.promptTokens` (cumulative) when no turn was streaming (cold reload/idle), violating the gauge's own contract.

**Fix (shipped):** new `GetConversationLastInputTokens` query + `Conversation.LastInputTokens` populated in `Get`/`GetForIdentity`; `RuntimeFooter` fallback → `turn?.promptTokens ?? conv?.LastInputTokens ?? 0`. Regression tests: db_integration (`LastInputTokens==11`) + vitest (gauge reads 3%, never 62%).

**Verification:** `go build/vet/gofmt` ✓; db_integration PASS on disposable DB (live `aura` untouched); 28 vitest + tsc + eslint + prettier ✓. Commits `0c652d06` (fix) + `1713b621` (rebuilt embed bundle). **Deployed** to `aura:local` container 2026-07-20 (new bundle served, healthy). Not pushed.

---

## Wave 0 — Stop the bleeding (P0, live-broken now)

| # | Verdict | Fix (verified) | migration / PRD | risk |
|---|---|---|---|---|
| **0.1** Evaluator dies on 23505 | CONFIRMED (high) → **SHIPPED** | Treat empty `{}` windows as "no observation" — `EvaluateOnce` skips `Apply` (the root guard that stops the storm) + `Run` tolerates a stray 23505 (self-heal, item 0.2). **The originally-planned idempotent `ON CONFLICT DO NOTHING` append was REVERTED** — it broke `TestRolloutStoreAtomicRollbackRestoresLastKnownGood`, which enforces the real immutable-ledger dedup contract (a duplicate evidence digest MUST 23505-abort the tx atomically). Guard + self-heal fully fix the crash without weakening that invariant. | no migration / no | low |
| **0.2** Goroutine log-and-die | CONFIRMED_NARROWED (high) | **Correction: no shared "supervisor helper" exists in-repo — do NOT build one.** Mirror the swallow-per-tick pattern: `return err` → `slog.Warn`+`continue`; only ctx-cancel terminates. Census-confirmed resilient goroutines untouched. | no / no | low |
| **0.3** Cypher hang under `mu` | CONFIRMED (high) | Extract `readLineWithContext(ctx)` mirroring `initializeWithContext` (goroutine + ctx-select + `Process.Kill()` on timeout); replace the bare `ReadBytes` at `client.go:215`; keep `mu` held (do not release across the read). | no / env-note | low |

**Live status:** 0.1 re-confirmed firing on the 2026-07-20 container boot (`compaction rollout evaluator stopped … 23505` within seconds).

---

## Compaction SPIKE (gate — precedes 1.9 + 2.3) — ✅ RESOLVED + REMOVAL SHIPPED 2026-07-20

**Outcome: REMOVE** (operator-ratified, PRD Amendment #86; ADR `docs/audit/compaction-spike-2026-07-20.md`). Verified dark end-to-end (`FinalizeCompaction` zero non-test callers; Preview/Restore on a never-filled table; rollout windows never populated; trigger enums dead; cost one P0 = BUG-6a). It is **redundant on top of amendment #21's shipped 5-layer defense**, not the defense itself. Verdict: delete the Phase 42 engine + migrations 0036-0039 in a dedicated removal phase (drop-migrations at the next free slot; Wave 0.1 crash-guard stays until then); **1.9 = NO-GO, 2.3 = NO-GO**.

Hard constraint honored: context management stays Aura-side + provider-agnostic (the amendment #21 ladder + 1.10 token estimation remain load-bearing). **The anti-rot core is L4 extractive graph memory** — the industrial survey (Anthropic native compaction/context-editing, mem0 v3, tokenjuice, Letta, neo4j agent-memory) concluded transcript compaction is the one layer the market gives away, while retrieval-of-salient-facts is the durable rot defense Aura is uniquely positioned for (Neo4j in-stack). **L4 archival recall is now SHIPPED + CI-green** (runner `ArchivalRecaller` seam → memory MCP `get_context` long-term, scoped by identity, query-keyed; `AURA_CONTEXT_MEMORY_RECALL`, commit `340d6966`).

---

## Wave 1 — Wire the cables + turn on the dashboard (P1)

| # | Verdict | Fix (verified) | migration / PRD | risk |
|---|---|---|---|---|
| **1.1** Wallclock finalize DOA | CONFIRMED (high) | Derive synthesis/critic/recovery ctx from `context.WithoutCancel(ic.Ctx)` + fresh `TotalTimeoutSec` `WithTimeout` (keeps request/trace values, severs the expired deadline). Pattern already used by `maybeAutoTitle`/`flushPause`. | no / no | low |
| **1.2** Missed reminders dropped | CONFIRMED (high) | Flip `ReminderHandler.Meta().ReschedulesOnRecovery` false→true (catch-up-once, collapsed, carries `MissedSince`); fix the false comments (`reminder.go:24`, `serve.go:322`); update 3 tests (justified behavior change, not babysitting). | no / PRD note | med |
| **1.3** SSE no resilience | 🟡 **PARTIAL** — Tier A (server heartbeat) shipped this commit; client comment-tolerance pre-existing; Tier B (reconnect + Last-Event-ID resume, run-lifetime decoupling) requires a PRD amendment (AG-UI gateway contract change) — deferred to the PRD-first batch (1.2/1.7/1.8/AG-016) | Tier A: idle SSE-comment (`:hb\n\n`) heartbeat ticker in `streamSSE`'s drain loop (`internal/agui/server_sse.go`) — the drain goroutine is the sole writer of `w`, so the ticker's write and `WriteEventWithType` are mutually-exclusive `select` cases and can never split a frame. `AURA_AGUI_SSE_HEARTBEAT_SEC` (default 15, `<=0` disables, no ticker allocated). Client comment-tolerance was already shipped (`web/src/chat/sseAdapter.ts:421`, tested). Tier B: client reconnect + `Last-Event-ID` resume; decouple run lifetime from the single fetch. | no / env-var (PRD catalog, done this commit) | med |
| **1.4** `/readyz` lies | ✅ **DONE** (core shipped by 39-03 `a20eeddd`/`2727b1cf`; residual staleness-window/tick coupling + env knob in this commit) | Core: `CodeSchedulerStalled` gating via `readiness.Snapshot`/`Reasons()`, scheduler `markTick`/`markTerminalFailure` wiring, `/readyz` merging reasons, compose healthcheck. Residual closed here: the hardcoded 90s staleness window now derives from the RESOLVED tick (`schedulerReadinessMaxAge` = 3×tick, floored at 90s) so an operator-widened `AURA_SCHEDULER_TICK_SECONDS` doesn't false-positive `scheduler_stalled` between ticks; new `AURA_SCHEDULER_READY_MAX_STALE_SEC` override knob. Rerank/embed probes deliberately NOT added (fail-soft by design). | no / PRD note | low |
| **1.5** Ingestion silent drop | ✅ **DONE** (this commit) | Observability only (durable dead-letter row already existed): `slog.Warn("documents: ...")` at the embed-enqueue drop (`service.go`) and both dead-letter branches (`jobs_worker.go`, `handler_missing`/`handler_failed`); new `ingestion_jobs` counter (outcome `succeeded`/`dead_letter`/`retry_scheduled`), `ingestion_embed_duration` histogram, and `ingestion_queue_depth` gauge (catalog-owned, mirrors the retention trio) in `internal/documents/metrics.go`. Queue depth sourced via optional `IngestionQueueDepthSource` (`CountByStatus`, new sqlc query `CountIngestionJobsByStatus`), sampled once per `ProcessOnce` pass, fail-soft on error. Fail-soft behavior byte-identical; no config knobs, no retry-logic changes. | no / no | low |
| **1.6** MCP no health poll | ✅ **DONE** (this commit) | Stdio reactive reconnect-on-transport-error was already shipped (`reconnectingServer`, `bridge_reconnect.go`, breaker+backoff+two-context reopen+no-replay guard, unchanged) and the 0-tool boot race was already fixed by 38-05 (`mount_retry.go` `MountWithRetry` + `AURA_MCP_MOUNT_TIMEOUT`) — neither rebuilt. This commit closes the two residual gaps: (a) generalized the replacement-open into a pluggable `open` seam on `reconnectingServer` (`setOpen`, default = the existing `openMCPClient` var) so `MountManagedServer`'s streamable-HTTP branch now wraps its transport in the SAME reconnecting machinery the stdio branch always had (`mount.go` `mountManagedHTTP`) — a dropped HTTP session/sidecar restart self-heals on the next call instead of leaving those tools dead until reboot; (b) added a bounded per-server background `Ping` poll (`bridge_ping.go`, `AURA_MCP_PING_INTERVAL_SEC` default 60s, `<=0` disables, each ping bounded to `min(10s, interval)`) that proactively triggers the existing `reconnectAfterTransport` on a transport-classified failure, started after a successful mount on both branches, goleak-clean on Close and on `processCtx` cancellation. | no / env-var (`AURA_MCP_PING_INTERVAL_SEC`, PRD catalog updated this commit) | med |
| **1.7** Approval relay-liveness | CONFIRMED (high) | Recurring pending-approval reminder sweep on the scheduler tick (mirrors `sweepNotifications`): re-surface channel-owned `pending_approval` tasks via `ChannelDeliverer` with a throttle stamp. Fixes the "gated job silently forgotten" residual (BUG-1B). | **migration** (throttle ts) / PRD | med |
| **1.8** Memory no delete/update/list | CONFIRMED_NARROWED (high) | Action-verb `memory_manage` (forget/update/list_by_type) in `mcp/_tools.py`; `memory_forget` MUST enforce ownership (`_node_in_user_scope`, refuse not no-op) and **return removed ids** for BUG-3b. Cursor/Devin parity. | no / PRD | med |
| **1.9** Compaction wiring | ✅ **DONE — N/A** (engine removed `e5b557f0`) | Superseded: the engine is deleted, not wired. Anti-rot is L4 archival recall (shipped `340d6966`, E2E-proven live). | — | — |
| **1.10** Token estimator (llama.cpp) | ✅ **DONE** (`c051c64d`) | `llamaCppEstimator` declares `DeclaredErrorTokens` (default 4096, env `AURA_MODEL_LLAMACPP_ERROR_RESERVE_TOKENS`); `ProviderErrorReserveTokens(cfg)` returns it only for `ReasoningTargetLlamaCpp`; `hardCap()` subtracts it (0 for OpenRouter → byte-unchanged; still floors to the M-03 small-window floor). Dead `1.15/256` estimator retired. | no / env-note | med |
| **1.11** OpenRouter overflow fail-safe | CONFIRMED_NARROWED (high) | Opt-in `transforms:["middle-out"]` on `wireRequest`, set ONLY when `ReasoningTarget==OpenRouter` AND a config knob is on (`omitempty` keeps llamacpp byte-unchanged). Explicitly NOT the compaction mechanism (lossy, no tool-pair awareness). | no / env-var | low |

---

## Wave 2 — Complete + UX (P2)

| # | Verdict | Fix (verified) | migration / PRD | risk |
|---|---|---|---|---|
| **2.1** notify no Telegram (BUG-2) | CONFIRMED (high) | **Correction: `telegram` cannot be a `buildSend` branch** (`compositeNotifier` has no `ChannelDeliverer`/identity). Intercept at the `Dispatch` layer (`dispatch.go`) where `task.IdentityID` + `ChannelDeliverer` exist; add server-side enum validation (bad route 400s, no silent stdout-degrade); tool enum + cockpit dropdown. | no / PRD | med |
| **2.2** Long TG output dropped (>4096) | (audit) | Reuse interactive chunking on the origin-channel push path. | no / no | low |
| **2.3** Compaction UI orphaned (BUG-7) | ✅ **DONE — removed** (`28c12351`) | `CompactionHistory` + `useCompactions` + `resources.compaction` + `compaction_memory_api` deleted; bundle rebuilt. | — | — |
| **2.4** No profile-edit tool (BUG-4) | CONFIRMED (high) | Deferred, action-multiplexed MUTATING `profile` builtin (show/add-fact/edit-section/remove-fact) via ActionRouter, backed by `internal/profile.Store` (add `Store.RemoveFact` + `Store.ReplaceSection`; only `AddFact` exists). Never expose the raw Agent.md path to `fs_write`. Correct the false memory record. | no / PRD | med |
| **2.5** `fs_edit/grep/glob` bypass box | (audit) | Give them a `Router` like `fs_read/write` — **blocker before any sandbox enablement**. | no / no | med |
| **2.6** docker_integration 0% CI | (audit) | Add a `docker_integration` CI job (or daemon-free unit tests) before sandbox rollout. | no / no | low |
| **2.7** RLS backstop (7 tables) | (audit, LATENT) | Owner-isolation policies (defense-in-depth; app-level `*ForIdentity` is primary). | migration / no | low |
| **2.8** Memory→task cascade (BUG-3b) | CONFIRMED (high) | Provenance column on `scheduler_tasks` (mirror `origin_conversation_id`) + Aura-side cascade: on memory-forget success extract removed ids → existing soft-cancel path via a new import-cycle-free `taskStore` seam. | **migration** / **PRD** | med |
| **2.9** Dead `sandbox_exec` ref | (audit) | Remove dead escalation instruction + stale docblock. | no / no | low |
| **2.10** Orphan cleanup | (audit) | Delete dead `RuntimeHealthPanel`; wire export + skills PATCH/DELETE UI or remove endpoints. | no / no | low |
| **2.11** Operator recovery seed | (audit) | Run manual `aura identity recover-operator` on host (not a live lockout). | no / no | low |

---

## Wave 3 — Hardening / prod-readiness (P2–P3)

3.1 strict profile + MUSR isolation (before a 2nd identity — **live warns 25 identities, isolation off**) · 3.2 `mem_limit` on all sidecars · 3.3 SSRF enforce for HTTP MCP · 3.4 gate the `changeme` PIM token · 3.5 coverage blind spots (`live_e2e` + `docker_integration` into CI, foreign→404/403 tests) · 3.6 compaction telemetry + retention sweeper (only if SPIKE=keep) · 3.7 short-TTL identity cache (kills mass-logout coupling).

**Side task (behavioral, Low):** BUG-5 — prompt guidance to prefer `shell_exec` for broad discovery. **P3 MCP prune:** cut `graph_query`; evaluate cutting the reasoning-trace trio; fix `catalog.go:162` stale "16-tool" comment (real count 17).

---

## AG-016 — three-tier redesign (policy, needs PRD amendment)

**Verified:** `task.go:216-218` unconditionally forces every `agent_job` to `pending_approval` *after* the tier gate already ran; for `agent_job` the tier is Normal (`GateRecommended(Normal)==false`), so the override is the sole gate. Reminders/backups (Safe) are exempt.

**Industrial pattern (researched — `D:\tmp\system-prompts-and-models-of-ai-tools` + web):** unanimous and *not* blanket-gating — every mature agent gates on the **nature/effect of the action** and honors prior confirmation.
- **Comet** (action-taking): three tiers *prohibited / explicit-permission / regular*; explicit-permission = irreversible external effects (send/publish/purchase/share/download); regular runs automatically.
- **Poke** (scheduled assistant, closest analog): confirmation captured at automation-setup time; scheduled job fires without re-asking; *"don't re-confirm what the user already confirmed."*
- **CodeBuddy / Augment / Devin / Claude Code:** per-command `requires_approval` = true only for impactful/destructive; gate "dangerous/expensive" only; never surprise, never destructive without explicit request.

**Counter-weight (defends the current gate):** an `agent_job` fires **unattended with an unbounded tool surface** — the LLM picks tools at fire time when no human is in the loop, so schedule-approval is Aura's only checkpoint for exactly the class every system gates; the keyword scorer on the job description is a weak signal.

**Resolution (verified fix, re-aligns with PRD "Hybrid C"):** keep `GateRecommended(tier)` as the structural veto (explicit-permission tier — always wins for Risky/Destructive incl. destructive-keyword payloads). For a Normal/Safe `agent_job`, gate **only when unattended-and-unconfirmed**; honor an operator confirmation captured at schedule-creation as the approval (Poke pattern — fixes the BUG-1A "already-confirmed but re-gated" complaint). Wave 1.7 still ships so a gated job can't be silently forgotten. **Policy change → PRD amendment before code.**

---

## Migrations & PRD amendments needed

- **New migrations** (number at landing via `ls internal/db/migrations/ | tail -1` — never deduce): **1.7** (approval-reminder throttle timestamp), **2.8** (scheduler_tasks memory-provenance column), **2.7** (RLS policies). 0.1 is query-only (sqlc regen, no migration).
- **PRD amendments before code** (architecture/policy): **AG-016** (gate policy), **2.8** (memory↔task provenance edge), **1.8** (memory tool-surface contract), **1.7** (approval-reminder seam). Minor PRD env-catalog updates: 1.3/1.4/1.6/1.10/1.11/2.1 (new `AURA_*` knobs).

## Execution discipline (standing, per CLAUDE.md)

Every item closes under the standing gates: 85% coverage floor on the `db_integration neo4j_integration` matrix (verify the stricter Skills-gate `db_integration`-only number; daemon/DB-gated code needs daemon-free unit tests), quality-snapshot re-attestation at phase close, no-skip-as-green, E2E score >9.8. Waves are sequential; items within a wave parallelize by subsystem. The compaction SPIKE is the one hard ordering dependency (blocks 1.9 + 2.3). Local full-matrix verification on the live stack beats push-and-wait; never point `db_integration` at the live `aura` DB (use the disposable-DB path — `scripts/coverage_docker.sh` / the `aura_cov` pattern).
