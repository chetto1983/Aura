# Aura — Consolidated Fix Plan

> **Historical execution plan.** Completion status is now machine-reconciled in
> [definitive-closure-ledger-2026-07-31.md](definitive-closure-ledger-2026-07-31.md);
> use this file for rationale, not current open-state counting.

Plan date: 2026-07-20
Supersedes-as-execution-index: `operator-reported-runtime-bugs-2026-07-18.md` (BUG-1..7) + `live-attack-plan-2026-07-18.md` (Waves 0–3).

## Staleness warning (2026-07-25)

**This document under-reports progress, and it was caught by accident.** Work started on row
1.1 only to find it shipped days earlier in `ac636ddcb`, with the prescribed fix at
`internal/agent/llm_agent_finalize.go:223` + `llm_agent_completion.go:98` and three RED tests.
A spot-check then found 0.3 in the same state (`readLineWithContext`,
`internal/knowledge/client.go:247`) and 0.2 mooted by a deletion the plan itself records
elsewhere. A row that says CONFIRMED here does NOT mean "still broken" — it means nobody came
back to this file after fixing it.

Three rows were verified against the code and corrected on 2026-07-25 (0.2, 0.3, 1.1). **Wave 2,
Wave 3 and the Phase A table were NOT verified** — two audit subagents were dispatched for them
and both were killed by the 600s stall watchdog, so those sections carry the same doubt as
before. Verify against the code before starting any row here; the cheapest check is to grep for
the symbols the "Fix (verified)" column already names.

---

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
| BUG-1B approval undelivered | 1.7 (reframed: relay-liveness) | 1 | **✅ SHIPPED 2026-07-24** (CI green) |
| BUG-3 memory no delete | 1.8 | 1 | verified |
| BUG-2 no Telegram notify | 2.1 | 2 | verified (fix-direction corrected) |
| BUG-7 compact UI orphaned | 2.3 → REMOVED | 2 | ✅ removed (`28c12351`, engine deleted) |
| BUG-4 no profile-edit tool | 2.4 | 2 | ✅ **closed — superseded by Amendment #87** (`internal/profile` deleted; the memory verbs are the profile surface) |
| BUG-3b memory→task cascade | 2.8 | 2 | **rescoped 2026-07-27** — visibility + one prompt line, not a cascade (no migration, no PRD amendment) |
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
| **0.2** Goroutine log-and-die | ✅ **MOOT** (verified 2026-07-25) | **Correction: no shared "supervisor helper" exists in-repo — do NOT build one.** Mirror the swallow-per-tick pattern: `return err` → `slog.Warn`+`continue`; only ctx-cancel terminates. Census-confirmed resilient goroutines untouched. | no / no | low |
| **0.3** Cypher hang under `mu` | ✅ **SHIPPED** (verified 2026-07-25) | Extract `readLineWithContext(ctx)` mirroring `initializeWithContext` (goroutine + ctx-select + `Process.Kill()` on timeout); replace the bare `ReadBytes` at `client.go:215`; keep `mu` held (do not release across the read). | no / env-note | low |

**Live status:** 0.1 re-confirmed firing on the 2026-07-20 container boot (`compaction rollout evaluator stopped … 23505` within seconds).

---

## Compaction SPIKE (gate — precedes 1.9 + 2.3) — ✅ RESOLVED + REMOVAL SHIPPED 2026-07-20

**Outcome: REMOVE** (operator-ratified, PRD Amendment #86; ADR `docs/audit/compaction-spike-2026-07-20.md`). Verified dark end-to-end (`FinalizeCompaction` zero non-test callers; Preview/Restore on a never-filled table; rollout windows never populated; trigger enums dead; cost one P0 = BUG-6a). It is **redundant on top of amendment #21's shipped 5-layer defense**, not the defense itself. Verdict: delete the Phase 42 engine + migrations 0036-0039 in a dedicated removal phase (drop-migrations at the next free slot; Wave 0.1 crash-guard stays until then); **1.9 = NO-GO, 2.3 = NO-GO**.

Hard constraint honored: context management stays Aura-side + provider-agnostic (the amendment #21 ladder + 1.10 token estimation remain load-bearing). **The anti-rot core is L4 extractive graph memory** — the industrial survey (Anthropic native compaction/context-editing, mem0 v3, tokenjuice, Letta, neo4j agent-memory) concluded transcript compaction is the one layer the market gives away, while retrieval-of-salient-facts is the durable rot defense Aura is uniquely positioned for (Neo4j in-stack). **L4 archival recall is now SHIPPED + CI-green** (runner `ArchivalRecaller` seam → memory MCP `get_context` long-term, scoped by identity, query-keyed; `AURA_CONTEXT_MEMORY_RECALL`, commit `340d6966`).

---

## Wave 1 — Wire the cables + turn on the dashboard (P1)

| # | Verdict | Fix (verified) | migration / PRD | risk |
|---|---|---|---|---|
| **1.1** Wallclock finalize DOA | ✅ **SHIPPED** `ac636ddcb` (verified 2026-07-25) | Derive synthesis/critic/recovery ctx from `context.WithoutCancel(ic.Ctx)` + fresh `TotalTimeoutSec` `WithTimeout` (keeps request/trace values, severs the expired deadline). Pattern already used by `maybeAutoTitle`/`flushPause`. | no / no | low |
| **1.2** Missed reminders dropped | ✅ **DONE** (this commit) | Flipped `ReminderHandler.Meta().ReschedulesOnRecovery` false→true — this RESTORES the PRD Slice 6 contract (PRD always said `true`: "idempotente: ri-notificare è safe"; Phase 19 M-g shipped the drift). Boot catch-up now delivers ONE collapsed fire (D-18) and `Run` appends a "late delivery — originally scheduled …" marker from `MissedSince` (the D-18 docstring's stated purpose). False comments fixed (`reminder.go` Meta, `serve.go` catch-up note); 3 tests updated with justification: `TestReminderMeta` flipped + new `TestReminderLateDeliveryMarker`, `recover_test` M-g no-replay case swapped reminder→`skill_ttl_sweep` (a genuinely-false kind), `scheduler_test` seam/lookup maps aligned. PRD drift-note added inline at the Slice 6 reminder row. | no / PRD note (done) | med |
| **1.3** SSE no resilience | ✅ **DONE** (2026-07-23) — Tier A heartbeat `a93648c5`; Tier B FULLY SHIPPED: amendment #90 (`adfac168c`) → RS-01..03 core (`5283f54f8`/`246dd75e9`/`3dc5cc3cd`) → RS-04..06 server (`ddbbf64fc`/`004f41b25`/`fa849f679`) → RS-07 web resilient client (`870e9ae9c`) → E2E-found mount-gap fix (`a03ea717b`, /agent/runs/ subtree) → **live E2E 10/10** (kill-TCP gapless resume, reload-attach, cancel+lock-release, all wire+DB ground truth) → **default flip ratified as amendment #90.1** (AURA_AGUI_RUN_DETACH=true) | Tier A: idle SSE-comment (`:hb\n\n`) heartbeat ticker in `streamSSE`'s drain loop (`internal/agui/server_sse.go`) — the drain goroutine is the sole writer of `w`, so the ticker's write and `WriteEventWithType` are mutually-exclusive `select` cases and can never split a frame. `AURA_AGUI_SSE_HEARTBEAT_SEC` (default 15, `<=0` disables, no ticker allocated). Client comment-tolerance was already shipped (`web/src/chat/sseAdapter.ts:421`, tested). Tier B: client reconnect + `Last-Event-ID` resume; decouple run lifetime from the single fetch. | no / env-var (PRD catalog, done this commit) | med |
| **1.4** `/readyz` lies | ✅ **DONE** (core shipped by 39-03 `a20eeddd`/`2727b1cf`; residual staleness-window/tick coupling + env knob in this commit) | Core: `CodeSchedulerStalled` gating via `readiness.Snapshot`/`Reasons()`, scheduler `markTick`/`markTerminalFailure` wiring, `/readyz` merging reasons, compose healthcheck. Residual closed here: the hardcoded 90s staleness window now derives from the RESOLVED tick (`schedulerReadinessMaxAge` = 3×tick, floored at 90s) so an operator-widened `AURA_SCHEDULER_TICK_SECONDS` doesn't false-positive `scheduler_stalled` between ticks; new `AURA_SCHEDULER_READY_MAX_STALE_SEC` override knob. Rerank/embed probes deliberately NOT added (fail-soft by design). | no / PRD note | low |
| **1.5** Ingestion silent drop | ✅ **DONE** (this commit) | Observability only (durable dead-letter row already existed): `slog.Warn("documents: ...")` at the embed-enqueue drop (`service.go`) and both dead-letter branches (`jobs_worker.go`, `handler_missing`/`handler_failed`); new `ingestion_jobs` counter (outcome `succeeded`/`dead_letter`/`retry_scheduled`), `ingestion_embed_duration` histogram, and `ingestion_queue_depth` gauge (catalog-owned, mirrors the retention trio) in `internal/documents/metrics.go`. Queue depth sourced via optional `IngestionQueueDepthSource` (`CountByStatus`, new sqlc query `CountIngestionJobsByStatus`), sampled once per `ProcessOnce` pass, fail-soft on error. Fail-soft behavior byte-identical; no config knobs, no retry-logic changes. | no / no | low |
| **1.6** MCP no health poll | ✅ **DONE** (this commit) | Stdio reactive reconnect-on-transport-error was already shipped (`reconnectingServer`, `bridge_reconnect.go`, breaker+backoff+two-context reopen+no-replay guard, unchanged) and the 0-tool boot race was already fixed by 38-05 (`mount_retry.go` `MountWithRetry` + `AURA_MCP_MOUNT_TIMEOUT`) — neither rebuilt. This commit closes the two residual gaps: (a) generalized the replacement-open into a pluggable `open` seam on `reconnectingServer` (`setOpen`, default = the existing `openMCPClient` var) so `MountManagedServer`'s streamable-HTTP branch now wraps its transport in the SAME reconnecting machinery the stdio branch always had (`mount.go` `mountManagedHTTP`) — a dropped HTTP session/sidecar restart self-heals on the next call instead of leaving those tools dead until reboot; (b) added a bounded per-server background `Ping` poll (`bridge_ping.go`, `AURA_MCP_PING_INTERVAL_SEC` default 60s, `<=0` disables, each ping bounded to `min(10s, interval)`) that proactively triggers the existing `reconnectAfterTransport` on a transport-classified failure, started after a successful mount on both branches, goleak-clean on Close and on `processCtx` cancellation. | no / env-var (`AURA_MCP_PING_INTERVAL_SEC`, PRD catalog updated this commit) | med |
| **1.7** Approval relay-liveness | ✅ **DONE** (2026-07-24, pushed, CI green) | Recurring pending-approval reminder sweep on the scheduler tick (`internal/cron/deliver_approval.go`): ensure-or-mint the origin-channel `ask_user` pause and re-push it through `ChannelDeliverer`, throttled by `scheduler_tasks.approval_reminded_at`. Live E2E on the real stack (Telegram + DeepSeek-V4-Flash + cockpit) caught **five Telegram-only defects** the unit tests and the full local gate had all passed — accept-loop (resumed model never told the task went active), `BUTTON_DATA_INVALID` (telebot callback framing = 65 > 64 B), raw UUID in the prompt, duplicate prompt (sweep raced the in-turn relay → grace delay + deliver-only-when-minted), silent backstop resolve — fixed in `104a284d0`. Handoff: `docs/superpowers/2026-07-24-1.7-session-handoff.md`. **Successor = Phase A** (channel-approval consolidation) — see its own section below. | **migration `0051`** ✓ / PRD #92 ✓ | med |
| **1.8** Memory no delete/update/list | ✅ **UPDATE DONE** (`20a4e8d88`, 2026-07-25) — `list_by_type` still open | `memory_update(node_type, node_id, …)` shipped as `memory_forget`'s twin over `entity | preference | fact`: same dispatch shape, ownership-scoped, refuse-not-no-op, re-embeds when the embedded text changes, and keeps `canonical_name` aligned to a corrected name. The verbs now close (add / update / forget) for everything the agent can create. **The diagnosis that led here was wrong at first and the correction matters**: the operator watched the agent fail to rename "David"→"Davide" and thrash add→forget four times in 2.5 min. The write had SUCCEEDED on the first try — every read projected `Entity.display_name` (`canonical_name or name`) under the key `name`, so `add`, `get` and `search` all reported the resolver's canonical and the agent could never observe its own success. Reads now return the stored name, with the canonical as a separate field when it differs. Found by mounting the MCP and using it as a client, not by reading the code. | no / PRD | med |
| **1.9** Compaction wiring | ✅ **DONE — N/A** (engine removed `e5b557f0`) | Superseded: the engine is deleted, not wired. Anti-rot is L4 archival recall (shipped `340d6966`, E2E-proven live). | — | — |
| **1.10** Token estimator (llama.cpp) | ✅ **DONE** (`c051c64d`) | `llamaCppEstimator` declares `DeclaredErrorTokens` (default 4096, env `AURA_MODEL_LLAMACPP_ERROR_RESERVE_TOKENS`); `ProviderErrorReserveTokens(cfg)` returns it only for `ReasoningTargetLlamaCpp`; `hardCap()` subtracts it (0 for OpenRouter → byte-unchanged; still floors to the M-03 small-window floor). Dead `1.15/256` estimator retired. | no / env-note | med |
| **1.12** Reasoning lost on reload (operator-requested 2026-07-23) | ✅ **DONE** (this commit) | Migration `0050` adds nullable `conversation_turns.reasoning` + `reasoning_duration_ms`; the runner (turnTracker, `runner_reasoning_persist.go` — agent #57 stream-only contract untouched) accumulates the turn's `LLMResponse.Reasoning` deltas rune-bounded (`AURA_REASONING_PERSIST_MAX_RUNES` default 65536, `<=0` off; head-keep + `[reasoning truncated]` marker; duration = first→last delta wall time) and `persistAssistantAnswer` lands both on the final answer row — skipped entirely via the `ShowReasoning` config seam when the stream is redacted (HARDEN-05), reset on B-12 DiscardStreamed. `GET /threads/{id}/messages` merges the display-only `ListTurnReasoning` read (separate sqlc query — `ListTurnsBySeq` untouched) onto assistant answer messages as wire fields `reasoning`/`reasoningDurationMs` (camelCase, RS-07 consumes). Amendment point 2 PINNED: `llm.Message`/`Turn` reflection pin + db_integration wire-messages pin; point 4: share `Snapshot` pinned reasoning-free (`TestSnapshotOmitsReasoning`). | **migration 0050** / env-var (`AURA_REASONING_PERSIST_MAX_RUNES`, PRD #91 catalog row this commit) | med |
| **1.11** OpenRouter overflow fail-safe | ✅ **DONE** | Opt-in `transforms:["middle-out"]` on `wireRequest`, set ONLY when `ReasoningTarget==OpenRouter` AND `AURA_LLM_OPENROUTER_MIDDLE_OUT` is on (default off; `omitempty` keeps llamacpp and knob-off OpenRouter byte-unchanged). Explicitly NOT the compaction mechanism (lossy, no tool-pair awareness) — dormant while Aura runs local. | no / env-var (`AURA_LLM_OPENROUTER_MIDDLE_OUT`, PRD catalog updated this commit) | low |

---

## Wave 2 — Complete + UX (P2)

| # | Verdict | Fix (verified) | migration / PRD | risk |
|---|---|---|---|---|
| **2.1** notify no Telegram (BUG-2) | ✅ **DONE + LIVE-PROVEN 2026-07-28** (`dd0e164a`) | The correction about `buildSend` was right and one revision stale: the Dispatch-layer interception it asked for **already existed** (Phase 20 `deliverToOrigin` → `originGate` → `ChannelDeliverer`). The defect was the gate's rule. It split routes into set/unset, when the real split is INTENT: whatsapp/email name an alternate destination and pre-empt origin (R7), while `""`, `stdout` and `telegram` all mean "deliver where this came from" — `telegram` most explicitly of all, and it was landing in the pre-empt branch and degrading to stdout. New `originPreferring` carries that split in the one place both the live and sweep paths already call. `sendViaMCP` now refuses `telegram` outright (reaching the composite means the gate declined) instead of falling into the `default:` branch, which would have sent the operator's reminder to **WhatsApp**; that default is spelled out now. `cron.ValidNotifyRoute` is the single enum consumed by the tool, the cockpit API (400, no silent degrade) and the CLI flag. **Live E2E:** a reminder routed `notify=telegram` fired on the container and arrived in the operator's chat. Note found on the way: `aura task schedule` writes `identity_id='local'`, so a CLI-scheduled task can never reach a channel — the E2E row had to be assigned to the operator identity. | no / PRD env-catalog row ✓ | med |
| **2.2** Long TG output dropped (>4096) | ✅ **DONE + LIVE-PROVEN 2026-07-28** (`e60f0f13`) | Confirmed exactly as written, and the failure mode was worse than "dropped": the Bot API REJECTS an over-cap message, so nothing arrived at all — not a truncation. `Deliver` now reuses `splitTelegramText`/`telegramTextCap` (same package, already in scope — a second splitter is how the interactive path came to be correct alone), paces between chunks ctx-aware, and stops at the first failing chunk naming its position. `DeliverApproval` untouched (bounded by construction). **Live E2E:** a 6,732-character scheduled reminder arrived in the operator's Telegram as **two** messages; before the fix the same push was a single rejected API call and the operator saw nothing. The content assertion in the unit test earned its keep by first failing on a real subtlety — the splitter consumes the boundary separator, so chunks do not rejoin byte-for-byte; the invariant asserted is that no non-whitespace rune is lost, over accented Italian + emoji. | no / no | low |
| **2.3** Compaction UI orphaned (BUG-7) | ✅ **DONE — removed** (`28c12351`) | `CompactionHistory` + `useCompactions` + `resources.compaction` + `compaction_memory_api` deleted; bundle rebuilt. | — | — |
| **2.4** No profile-edit tool (BUG-4) | ✅ **CLOSED — superseded** (verified 2026-07-27) | The prescribed fix is no longer implementable and no longer needed: **Amendment #87 deleted `internal/profile/*`** along with Agent.md itself (`prd.md:3824`), so the `profile` builtin's backing store does not exist. The capability it was to provide now lives on the memory graph — `internal/agent/prompt.go:54-60` (`<profile_context>`) tells the model the profile is recalled and persisted **only** through the memory tools, and those verbs close (`memory_add_*` / `memory_update` `20a4e8d88` / `memory_forget` `2ddccf26`). Adding a `profile` facade over them would re-introduce the second implementation of one concept that Phase A existed to remove. Operator-ratified 2026-07-27. | no / no (#87 already carries it) | — |
| **2.5** `fs_edit/grep/glob` bypass box | ✅ **DONE** (`94bfc6cae`) | The three tools reached the HOST fs under every profile while `fs_read`/`fs_write` were confined to the box — so a strict-profile agent could not READ a host file but could enumerate the host tree with `fs_grep` and rewrite host files with `fs_edit`. Each now takes a `Router` and routes before any host path resolution, wired at the composition root (`cmd/aura/main.go`). Only the **I/O** moved into the box: matching stays in Go (`applyExactEdit`, `grepContent`, `parseBoxFileFrames` are shared by both paths), because handing the pattern to the box's `grep` would have silently swapped RE2 for GNU ERE under the same tool. Daemon-free unit tests cover the shared logic + path helpers (the container-gated half earns zero coverage); a `docker_integration` tier proves each operation lands in the box AND a host canary stays invisible/unmodified. **Two defects only the live run could show**: the helper pinned `server_production` → gVisor `runsc` → `unknown or invalid runtime name` (which incidentally *proved* the fail-CLOSED invariant — the routed branch denied instead of falling back to the host); and the box shell is **dash**, where `read -d` is a bash-only extension that fails to an EMPTY sweep, so `fs_grep` answered `[no matches]` for a file it could see — replaced with POSIX `find -exec`. Also fixed a **pre-existing** unclosed Docker client in `newDockerRouter` whose keep-alive goroutines failed the package's `goleak` `TestMain` and with it the WHOLE tier, green tests or not — which is why 2.6 was unreachable before this. | no / no | med |
| **2.6** docker_integration 0% CI | ✅ **DONE** (this commit) | New `sandbox-docker-integration` job in `ci.yml`: builds `docker/aura-egress/Dockerfile` as `aura-egress:latest` (the PRODUCTION default in three places — config's `AURA_SANDBOX_EGRESS_IMAGE`, `usersandbox.egressSidecarImage`, both egress tests — so the tier runs the default path, not a CI-only override), pre-pulls `busybox:stable` + `python:3-slim` (NOT interchangeable: wget+timeout for the egress probes, python3 for the routed snippet — hence no global `AURA_SANDBOX_IMAGE`), then runs `go test -tags docker_integration -p 1` over `usersandbox` + `agent/tools` + `cmd/aura`. A GitHub-hosted ubuntu runner is native-Linux dockerd on a non-masquerading bridge, so this is the ONLY place the SBX-04 egress DROP assertions are meaningful (Docker Desktop/WSL vpnkit NATs around any nftables rule); every gate `t.Fatal`s under `$CI`, so a skipped tier fails rather than passing green. **This buys behavioural signal, NOT coverage credit** — `scripts/coverage_gate.sh` runs the `db_integration neo4j_integration` matrix, so docker-gated runtime still contributes ZERO to the ≥85% floor and its pure logic still needs daemon-free unit tests (CLAUDE.md updated). | no / no | low |
| **2.7** RLS backstop (7 tables) | (audit, LATENT) | Owner-isolation policies (defense-in-depth; app-level `*ForIdentity` is primary). | migration / no | low |
| **2.8** Memory→task cascade (BUG-3b) | **RESCOPED by the operator 2026-07-27** — no cascade, no provenance | Operator decision: *"basta istruire l'agente e fargli vedere i task attivi, la memoria la gestisce bene lui."* The engineered cascade is dropped: the correlation is semantic and the agent already owns it. What the code must supply is **visibility**, and two gaps block that today. (a) `renderTaskList` (`internal/agent/tools/task.go:390`) prints `id kind schedule_kind next` only — no payload, so the model sees a UUID and cannot tell which task a forgotten memory produced (the same raw-UUID defect 1.7's live E2E hit on approvals). (b) `cronTaskStore.ListScheduledTasks` (`cmd/aura/serve_adapters.go:190-193`) selects `WHERE status IN ('active','pending_approval')` with **no identity filter**, and `CancelScheduledTask` takes a bare id — latent under a single operator, but surfacing the payload would turn a cross-identity *listing* into cross-identity *content disclosure*, so the scoping lands FIRST. Then one line in `<memory>` (`internal/agent/prompt.go`) ties a forget/supersede to reviewing `task list`. **No migration, no provenance column, no PRD amendment** — the memory↔task contract stays the agent's judgement, not a schema. | no / PRD note | low |
| **2.9** Dead `sandbox_exec` ref | (audit) | Remove dead escalation instruction + stale docblock. | no / no | low |
| **2.10** Orphan cleanup | 🟡 **PARTIAL** — export leg ✅ DONE 2026-07-23 | **Export leg wired** (`53f81024e`): "Esporta (Markdown)" in the conversation actions menu → `GET /api/conversations/{id}/export` (Content-Disposition download, i18n en+it, vitest). Remaining: dead `RuntimeHealthPanel` deletion + skills PATCH/DELETE UI-or-remove. | no / no | low |
| **2.11** Operator recovery seed | (audit) | Run manual `aura identity recover-operator` on host (not a live lockout). | no / no | low |

---

## Phase A — channel-approval consolidation (succeeds 1.7)

**Why it exists:** four of the five defects 1.7's live E2E found were Telegram-only, because Telegram carried a *duplicate* approval implementation divergent from the cockpit's. Two implementations of one concept means every fix must be made twice, and "works on one channel, silently broken on the other" is structural. Design + plan: `docs/superpowers/specs/2026-07-24-channel-approval-consolidation-phase-a-design.md`, `docs/superpowers/plans/2026-07-24-channel-approval-consolidation-phase-a.md`.

**Mechanism:** one runner-owned `ResolveDirective` decides continue-vs-outcome; each channel renders it and forwards the action, and neither re-derives the decision.

| Task | Commit | Status |
|---|---|---|
| T1-T2 runner `ResolveDirective` + `SubmitAnswer` seam | `adad718a5` / `58b3c678a` | ✅ |
| T3 collapse `aura_sappr` into `aura_hitl`, delete the duplicate | `a1543c8dc` | ✅ |
| T4 render the directive outcome (continuation only on `OutcomeContinue`) | `42b5785c6` | ✅ |
| T5 richer native approval (Approva/Rifiuta/Dettagli) | `10232b645` | ✅ |
| T6 resolve endpoint returns the directive (200 JSON) | `6eb9c8a2c` | ✅ |
| T7 cockpit card consumes it; gate re-drives only on `continue` | `4f957b181` | ✅ |
| T9 db_integration: directive agrees with the real task transition | `0446485e8` | ✅ |
| — mutation gap on `classifyResolve` (66.7% → 91.7%) | `5100d0005` | ✅ |
| — live-E2E fixes (see below) | `4d6ea1e27` | ✅ |
| **T8** rebuild the committed `internal/webui/dist` | — | ⏳ before push |
| **T10** coverage gate + full live E2E + push/CI | — | ⏳ |

Merged to `master` at `07ffb56b5` + `70981b87f`. Quality rows re-measured (`186f33711`): telegram **86.0%**, agui **85.0%** on the `db_integration` tier.

**Live E2E, round 1 (2026-07-25).** Schedule an `agent_job` from Telegram → DB ground truth: ONE pause minted, no duplicate, sweep stamped its throttle without re-minting, resolved, task `active`. The three 1.7 defects that were supposed to disappear did. It also found two new ones, both fixed in `4d6ea1e27`:
- **Dettagli was a dead affordance on the relay path.** The in-turn relay already renders the pause question AS the message text, so "revealing" it edited the message to identical content → `Bad Request: message is not modified (400)`. The button now exists only on the bounded sweep push, and the reveal consumes it (text becomes the question, keyboard re-arms without Dettagli), plus a guard skips the edit when the text already matches.
- **The agent explored for 62s before scheduling.** 13 tool calls — 6 `shell_exec` probing `/var/log`, a written script, a verification run — to schedule a cron job. Not a missing rule: `task`'s description already says to schedule recurring work rather than do it now, and it is non-deferred so the model reads it every turn. It collided with the global `Explore first, then commit` in `internal/agent/prompt.go`, which governs how to approach ANY request and wins at the start of the turn. Exploration is now scoped to work being done NOW, with delegation named as the exception. **The real damage was not the cost**: the resulting job froze the *container's* `/var/log` into its goal, which is not what the operator meant by "system logs" — delegation-time discovery bakes today's state into tomorrow's run.

---

## Memory sidecar — findings from dogfooding the MCP (2026-07-25)

The operator's instruction was "mount this MCP and test it as if it were yours". Everything below came out of **creating one test entity and reading it back**; none of it was visible from code review, and the existing 38-test suite was green throughout.

| Defect | Status |
|---|---|
| **M-1** Reads projected `canonical_name` as `name` | ✅ closed `df2e055a0` (after `20a4e8d88` + `28fbe5efb` fixed four of the five sites). `memory_get_entity` built its dict elsewhere and was missed; so were the entities-catalog resource and the relationship echo. Confirmed fixed in the live trace: `{"name": "Davide", "canonical_name": "David"}`. The guard no longer checks a literal from the site just fixed — it bans `.display_name` across the whole caller-facing read surface, over the AST, on a file set derived from the package directory, so a read path in a NEW module is covered the day it is written. |
| **M-2** No update verb | ✅ shipped `20a4e8d88` — `memory_update`, see 1.8. |
| **M-3** Canonical resolution unscoped | ✅ shipped `20a4e8d88`. `SEARCH_ENTITIES_BY_TYPE` fed the resolver **every** entity of that type in the database, so a fresh "Marco Bianchi" came back canonicalised onto another user's "Davide" — one user's data corrupting another's, and the stranger's name leaking back through the caller's own results. Now scoped to the caller. |
| **M-4** `*_SCOPED` reads did not scope — **cross-user data leak** | ✅ closed `0b5be1268` (2026-07-26) after **four more copies were found outside the net**. The 07-25 guard discovered its subjects with `[n for n in dir(queries) if n.endswith("_SCOPED")]`, so every copy living in an inline query string — under no naming convention, outside `queries.py` — survived: `_get_entity_neighbors` (`mcp/_tools.py`) and three inside `MemoryClient.get_graph` (`__init__.py`). The `_tools.py` one was the worst of the family — it backs `memory_get_entity(include_neighbors=True)`, which is **default-on**, and it is the only reader in the codebase walking an *untyped, variable-length* path. **Proven on the live deployment graph**: an identity owning nothing, asking for the neighbours of an entity it did not own, got back another user's entity; with the `WITH` inserted, zero rows, and the real owner still sees its own. The guard was rewritten rather than extended — discovery is now an AST walk over every Cypher string literal in the package, and the rule is sharper, because a glued `WHERE` is **not** wrong per se: `CREATE_MESSAGE`, `GET_SESSION_CONTEXT` and `gds._communities_fallback` correctly use one to constrain their own optional pattern, and a blanket ban would have had to "fix" those three by breaking them. The rule that separates them: *a WHERE glued to an OPTIONAL MATCH may only constrain variables that same OPTIONAL MATCH introduces.* Two of the new tests exercise the analysis against the shipped-broken and repaired text, so a guard that cannot detect the bug it exists for now fails loudly. — earlier note: shipped `20a4e8d88`. Ownership was enforced by a `WHERE` glued directly to an `OPTIONAL MATCH`; in Cypher that constrains the optional pattern, NOT the outer rows, so the filter did nothing. **Conversations (by id and by session), messages, entities, reasoning traces and session lists were readable across users.** Proven on the live graph: querying as a user owning exactly one entity returned another user's entity at score 0.999; after the fix, only its own. Seven queries corrected — six shipped, the seventh added the same day by copying the broken shape, which is how the pattern propagates. The in-memory fake clients the suite uses never execute Cypher and structurally could not catch it; a new structural test parses `queries.py` and fails any `*_SCOPED` query written that way. |
| **M-5** Entities written with no embedding → invisible to `memory_search` | 🔧 in progress. `short_term.py:1141,1325` pass `"embedding": None` hardcoded, never consulting the embedder, so every entity born from message extraction is unsearchable (the operator's own two "David" nodes are). Also: no uniqueness constraint backs the bare `MERGE (e:Entity {name, type, deduplication_scope})`, so concurrent writes can duplicate; and `GET_ENTITIES_WITHOUT_EMBEDDINGS`/`UPDATE_ENTITY_EMBEDDING` exist with **zero callers** — a repair path designed and never wired. |
| **M-10** `memory_get_entity` searched with `limit=1`, hiding every duplicate | ✅ shipped `4338b0152`. The one tool whose job is to answer about a name reported only the top hit, so an agent asked to clean up duplicates was told about one, corrected it and stopped (round 5). It now looks at a few and lists the rest under `other_matches`, present only when the name really is ambiguous. |
| **M-9** `memory_forget` succeeds and the entity **stays in the caller's reads** | ✅ shipped `4338b0152` — forget now cuts every edge that puts the entity in THAT caller's scope (ownership + their `MENTIONS`/`APPLIES_TO`/`TOUCHED`), and only theirs: still non-cascading, still never `DETACH DELETE`, node removed only when fully orphaned. Covered by a test that EXECUTES the Cypher against a live Neo4j — the suite's fake clients are why the `*_SCOPED` family shipped green while scoping nothing. Found 2026-07-25 round 5: Forget removes the `HAS_ENTITY` edge and answers `{"deleted": "3700…", "removed_node": false, "note": "unlinked from you; entity kept (still referenced elsewhere)"}` — the non-cascading design from `2ddccf269`, deliberate. But `ENTITY_IN_USER_SCOPE`, which backs every scoped read, also accepts `MENTIONS` — and the message that mentioned it is still the caller's. Measured right after the successful forget: `owned=0, mentioned=1, scope_count=1`. So search, `get_entity`, `get_context` and the recalled-context block can all still surface what the user was told was forgotten, and since MENTIONS is how extraction-born entities exist at all, this is the normal case, not the edge case. Deleting ownership is not forgetting; either forget must also cut the caller's `MENTIONS` edges, or it must stop reporting `deleted`. |
| **M-8** Restarting the sidecar leaves the running daemon **400ing on every memory call** | 🔧 fix written + verified, **NOT committed** (Go, blocked on the `internal/adaptive` lint) — deployed live at the operator's call. Patch: `D:/tmp/memory-bridge-and-session-fixes.patch`. Observed after `docker compose up -d aura-agent-memory-mcp`: 20 consecutive calls returned `mcp "memory": call …: http 400`, and only restarting `aura` cleared it. The recovery path existed but was **unreachable**: a stale session gets 404, which clears `c.sessionID` *before* `roundtripLocked` decides whether to retry; when it declines — correctly, a mutating tool call must not be replayed after send — the client is left with no session, and the only trigger for reinitialize was the session id just cleared. Every later call, reads included, posts bare and FastMCP answers `400 "Missing session ID"` forever. Both status codes were confirmed against the running sidecar. A session-less 400 now means the same thing as an expired 404; the test reproduces the exact sequence and, mutated, fails with the exact live error string. |
| **M-7** `memory_update` reaches the sidecar with **no caller identity** — every correction the agent attempts is refused | 🔧 fix written + verified, **NOT committed** (Go, blocked on the `internal/adaptive` lint). Patch: `D:/tmp/bridge-user-identifier.patch`. `internal/agent/mcptools/bridge.go:144` decided which memory tools get `user_identifier` injected from a **hand-kept name list**, and `memory_update` was never added to it when the verb shipped. The server treats a missing identifier as "no scope" on its write verbs, so it answered `"not found or not owned by this user"` — about the operator's own entity, which they own by a direct `HAS_ENTITY` edge. Proven on the live graph: the same scope query returns `scope_count=0` with a null identifier and `1` with the operator's. The agent then falls back to `add_*`, which merges instead of correcting and **creates duplicates** — the very failure the update verb was added to end. The list is now derived from the tool's own advertised schema, so a verb added in the sidecar cannot be forgotten in the bridge. |
| **M-6** Duplicate "David" nodes | root-caused, no fix needed. They differ by `type` (`OBJECT` vs `PERSON`), and the MERGE key is `{name, type, deduplication_scope}` — two nodes by construction, no deduplication could ever have merged them. The upstream fault is extraction classifying a person as an OBJECT. |

**Method note.** M-1 through M-4 were found by *using* the tool, M-5/M-6 by reading the graph. The bug class here is uniform: a read that silently returns something other than what it claims (a canonical instead of a name; another user's row instead of yours). Unit tests over fake clients cannot see any of it.

**Open (not started):** the `add_preference` exact-text dedup fallback (`long_term.py:749-775`, shipped `3fb13edee`) was never ported to `add_entity`/`add_fact`, whose dedup is embedding-gated only — with no embedding, dedup silently does not run.

---

## Memory sidecar — second dogfooding pass (2026-07-26)

Same method, same result: everything below came out of *using* the tool and reading the graph, with the suite green throughout. The uniform bug class this time is **queries written against a graph shape no writer produces**, and **the same server addressed by callers who disagree about who is asking**.

### Shipped

| Fix | Commit | What it was |
|---|---|---|
| Four surviving M-4 copies + rewritten guard | `0b5be1268` | See the corrected M-4 row above. A live cross-user leak through a default-on tool. |
| CLI memory calls carried no identity | `52a98688c` | `aura memory store-message` wrote a `:Conversation` with a NULL owner and zero `HAS_CONVERSATION` edges — data owned by nobody, invisible to every scoped read meant to return it. |
| Operator identity resolved instead of guessed | `8e51183f1` | **The root cause of the split-brain graph.** `identityctx.LocalOperatorIdentity` is documented as "the fail-closed fallback owner" and is not: `serve_auth.go:189` retires it at first login — migrates the Postgres references onto the enrolled identity and **DELETES the row**. Verified: `…0001` is absent from `aura.identities`; the operator is `e3c8eb3b`. The retirement does **not** reach Neo4j, so the memory graph forked — cockpit reading as the enrolled identity, CLI writing as a deleted tenant. `aura memory facts Davide` answered `fact_count: 0` with the facts sitting there. `identity_auth_links` is now the single answer; `scopeMemoryArgs` no longer invents an owner (a fallback that silently picks one cannot tell "no principal yet" from "wiring bug"). |
| Recall ranked by relevance | `12356a83a` | Search returned everything for any query. |
| Preferences can say what they are about; extraction scope unforked | `f7ba4a654` | `applies_to`, plus extraction and the agent path deriving the same `deduplication_scope` instead of two that could never merge. |
| `aura memory` exposes `--about`, `update`, `facts` | `69e03cae6` | Three sidecar capabilities were unreachable from the operator path. `facts` needs both forms because **facts are the one memory kind `memory_search` does not cover** (`integration_context.search` defaults to messages, entities, preferences). |
| Facts attach to the entity their subject names | `2658d228f` + migration `0006` | `Fact{subject:"Davide"}` and `Entity{name:"Davide"}` were unconnected. Now `(:Fact)-[:ABOUT_SUBJECT]->(:Entity)`, re-resolved when `update_fact` changes the subject, surfaced on `memory_get_entity`. **`ENTITY_IN_USER_SCOPE` deliberately NOT extended** — a scope-granting edge fed by LLM-generated free text is an entity-enumeration oracle; resolution only ever matches entities the caller already owns, so the capability lands with the widening surface untouched. `DELETE_ENTITY_SCOPED` cuts the edge *before* the orphan probe, which is untyped and would otherwise have left `removed_node` permanently false. |
| Cypher migration splitter cut on `;` inside comments | `29014e941` | Migration 0006 failed on first apply with `Invalid input 'this'` — the tail of an English sentence handed to Neo4j as a statement. |
| authlib deprecation noise in the suite | `7898ba3cd` | `authlib/deprecate.py` registers its own `always` filter at import, overriding pytest's config. |

### Open — decisions the operator owns

**D-1 — What gets persisted to memory per turn.** Measured on `aura.conversation_turns` (n=398, 2 days, single operator — ratios solid, absolute rate soft):

- **The volume objection does not hold.** Even persisting every turn including tool traffic is ~8,700-8,900 nodes and ~24,000-26,000 edges/month. Trivial for Neo4j.
- **The cost objection holds, for an unnoticed reason** — see D-2.
- **The number that decides it:** tool results are **83.5% of all conversational characters**, and their top extracted "entities" are `Vento` 73, `Pioggia` 50, `Nebbia` 28, `Pressione`, `Umidità`, `Percepita` — weather-table column headers scraped from ilmeteo.net — plus `Optional`, `Parameters`, `The` from `tool_search` schema dumps. And **66% of tool calls are `memory__*`**, so storing tool results re-ingests the graph's own output: the `Davide` 92 / `David` 83 split in that text *is* the duplicate the operator complained about, being read back and re-MERGEd.
- **Recommended: all text turns, tool blocks excluded** (~1,900-2,400 nodes, ~4,800-7,100 edges/month). *Not* "user turns only": 92% of user turns contain zero proper nouns (measured 0.08/turn — `"so?"`, `"ciao"`), so that policy would add ~50 entities/month and never build a traversable graph. The structure is in the assistant's text turns, at 3.91 entities each.
- Lever already in the code: `short_term.add_message(extraction_mode='skip')` stores with zero extraction cost.

**D-2 — Extraction is 100% paid LLM, and the "fallback" is not one — but it runs ZERO times today.**

> **Correction, 2026-07-26, operator-caught.** Everything in D-2 and D-3 below is about the
> extraction pipeline, and **nothing in Aura invokes it.** Extraction lives only in
> `short_term._extract_and_link_entities`, reachable solely from `add_message` /
> `memory_store_message`. Ground truth from `aura.tool_invocations`: across **238 memory
> tool calls the agent has made, `store_message` appears 0 times** — it calls
> `memory_search` 60, `memory_get_entity` 58, `memory_forget` 46, `memory_add_entity` 28,
> `memory_update` 18, `memory_get_context` 10, `memory_add_preference` 8,
> `memory_get_facts` 6, `memory_add_fact` 2. The `add_*` verbs write through
> `long_term`, which does not touch the extractor at all. The only other `add_message`
> callers in the package are the framework integrations (langchain, crewai, google_adk,
> llamaindex, microsoft_agent, agentcore) — none of which Aura mounts.
>
> So the per-message token figures below are **the cost of a policy that is not in effect**:
> they describe what D-1 would cost if it decided to persist turns, not what is being spent.
> Today the extraction LLM is called zero times per conversation, and the two dead NER stages
> throw only when an operator runs `aura memory store-message` by hand. **D-2 and D-3 are
> downstream of D-1, not independent of it** — there is nothing to optimise until something
> decides to persist messages, and no place for a local NER stage to plug in.

Two findings, both verified live, that become live the moment D-1 turns anything on:

1. **spaCy and GLiNER are not installed.** `Dockerfile:57` installs `-e ".[mcp,google,openai]"`; the `spacy`/`gliner`/`extraction` extras exist in `pyproject.toml:71-75` and nobody asks for them. But `settings.py:193-194` defaults both to enabled, so the pipeline builds both stages and **both throw on every message** (`Stage 'SpacyEntityExtractor' failed: spaCy is required…` in the live logs). Of the upstream three-stage design, only the paid stage runs.
2. **`factory.py:277` sets `stop_on_success = not fallback_on_empty`.** With the shipped default `fallback_on_empty=True` that is `False`, and `pipeline.py:482` therefore never breaks early. **The LLM stage is always-on, not a fallback.** Installing local NER without flipping this saves zero tokens.

Per-call overhead measured inside the container: fixed prompt + system 1,928 chars ≈ 521 tok, plus the `ExtractionPayload` JSON schema 2,723 chars ≈ 736 tok = **~1,257 fixed tokens before any content**. A median 44-char user turn is ~12 tokens of payload: **~100:1**. Plus a synchronous OpenRouter round-trip (1-3s) on the turn path.

Highest-leverage move, zero new dependencies: **gate extraction on content** so the 1,257-token prefix is not burned on `"ciao"`. Free cleanup regardless: set `enable_spacy=False` / `enable_gliner=False` so two stages stop being built and failing per message.

**D-3 — GLiNER: measured, not estimated (2026-07-26). → DECIDED: do not adopt.**

> **Operator decision, 2026-07-26: "se non porta niente lasciamo così".** Right call, and
> for a bigger reason than the one first given.
>
> The original reasoning: GLiNER is more *accurate* than what runs today (7/7 types vs the
> LLM's habit of typing a person as an OBJECT — M-6), but accuracy was not the ask. The ask
> was to stop paying for an LLM call per message, which it cannot deliver while relations
> and preferences are LLM-only and `stop_on_success=False` keeps the LLM running regardless.
>
> **The real reason, found on the operator's challenge ("guarda come l'agente chiama i
> tool"): the pipeline GLiNER would live in is never invoked.** `store_message` has 0 calls
> in `aura.tool_invocations` against 238 memory calls; the agent writes through
> `add_entity`/`add_fact`/`add_preference`, which bypass extraction entirely. So enabling
> GLiNER today changes nothing at all — there is no code path to change. A cascade design
> was worked out (GLiNER as the gate, LLM only when it finds something — measured 58.1% of
> real text turns skipped, relations preserved where entities exist) and then **not built**,
> because it would have been wiring for a pipeline that does not run.
>
> **Strictly downstream of D-1.** Revisit if and only if something starts persisting
> messages. The bench in `extraction-bench/` and the cascade numbers above exist so that
> revisit costs an hour, not a day.

Run against the sidecar's real configuration — the 15 POLE+O labels from `factory.py:43-60` at the shipped threshold 0.5 — on real Italian: three signal sentences, three verbatim user turns from `conversation_turns`, the weather-header noise, and `"ciao"`. CPU only.

| Build | Entities recalled | False positives | Median latency | Size |
|---|---|---|---|---|
| `urchade/gliner_multi-v2.1` (safetensors) | **7/7** | **0** | 212 ms | ~850 MB |
| `onnx-community/gliner_multi-v2.1` → `onnx/model_fp16.onnx` | **7/7** | **0** | **119 ms** | **553 MB** |
| `onnx-community/gliner_multi-v2.1` → `onnx/model.onnx` (fp32) | 3/3 spot-check, identical scores | 0 | — | 1.10 GB |
| `onnx-community/gliner_multi-v2.1` → `onnx/model_quantized.onnx` (int8) | **0/7** | 0 | 44 ms | 333 MB |

Three results worth keeping:

- **The int8 quantized ONNX is silently broken.** It loads, runs 5× faster, and returns **nothing** — no error, no warning beyond a generic `model of type 'gliner' to instantiate a model of type ''`. Anyone benchmarking the default-looking file would conclude the model is useless on Italian. fp16 is the build to use: identical output to the upstream weights, half the size of fp32, and faster than safetensors.
- **Quality on Italian is not in question.** `Marco Bianchi`=person 0.99, `Acme S.r.l.`=organization 0.73, `Torino`=location 0.92, `Bologna`=location 0.95, `VerifyCorp`=company 0.70.
- **It emits nothing on the weather-header noise** — the exact text where the LLM extractor produced 13 "entities". As a filter on tool traffic it is precisely right.

**Licensing, verified per-model on the HF API (the *search* endpoint reports apache-2.0 for the v1 repo and is wrong):** `urchade/gliner_multi` is **`cc-by-nc-4.0`** — non-commercial, disqualifying. `urchade/gliner_multi-v2.1` is **apache-2.0**. Both `onnx-community` repos declare no license of their own and inherit from their `base_model`, so **`onnx-community/gliner_multi-v2.1` is the only usable one**.

**What it still does not do.** GLiNER extracts entities only. The LLM stage also produces **relations** (`llm_extractor.py:361-378`) and **preferences** (`:379-390`); `gliner_extractor.py:561` returns `relations=[]` by construction. For a graph memory the relations *are* the product, so flipping `fallback_on_empty=False` to actually skip the LLM would trade them away. GLiNER's honest role is entity extraction **plus** a cheap gate on whether a message contains anything worth paying the LLM for — which is D-2's "gate extraction on content", and where the weather-noise result makes it valuable.

**D-4 — spaCy (stage 1): do NOT enable it. Measured 2026-07-26, same fixtures, same metric. → DECIDED: do not adopt** (same operator call as D-3, and here the measurements agree with it outright rather than merely accepting it — spaCy would make the graph worse, not just cost more).

Recall is the easy half. The half that matters is the **type**, because the entity MERGE key is `{name, type, deduplication_scope}` — a wrong type does not mislabel a node, it **creates a second one that no deduplication can ever collapse**. That is M-6 on the live graph, mechanically: two "David" nodes differing only by `OBJECT` vs `PERSON`.

| Extractor | Types correct | Miscategorised | Spurious on no-entity text | Median latency |
|---|---|---|---|---|
| **`gliner_multi-v2.1` fp16** | **7/7** | **0** | **0** | 119 ms |
| `it_core_news_sm` | 5/7 | 2 | 3 | 3.9 ms |
| `it_core_news_lg` | 3/7 (boundary errors) | 3 | 5 | 4.5 ms |
| `xx_ent_wiki_sm` | 4/7 | 3 | 3 | 2.2 ms |
| `en_core_web_sm` — **the shipped default** | 3/7 (3 missed outright) | 1 | **16** | 5.2 ms |

- **The shipped default is actively destructive on Italian content.** `en_core_web_sm` invented `Il fornitore`=PERSON, `Ho parlato`=PERSON, `Nella tua memoria il`=PERSON, `Sistema la tua memoria`=PERSON, `mio nome risulta sbagliato e ci sono`=ORG. Sixteen of those across five sentences, each of which would become an `:Entity`. **Installing the extras without also changing `spacy_model` would poison the graph faster than it populates it.**
- **Even the right Italian model makes the duplicate problem worse, not better.** `it_core_news_sm` is the best of them and still calls `Acme S.r.l.` a PER and `VerifyCorp` a MISC — two guaranteed duplicate nodes out of seven entities.
- **Bigger is not better**: `it_core_news_lg` (~540 MB) scores *below* `sm` (~13 MB) — `Davide`=MISC, `VerifyCorp`=LOC, `ciao`=MISC, `mandami`=PER, and a `Torino martedi` boundary error.
- **All four hallucinate on the weather-header noise**, turning the ilmeteo.net column row into `MISC`/`LOC`/`PER` entities. GLiNER emits nothing there. So spaCy does not solve the tool-noise problem — it is the problem, cheaply.

**Revised recommendation for the extraction pipeline.** The upstream three-stage design (spaCy → GLiNER → LLM) is not the right shape for this deployment; the measurements say **two** stages:

1. **`onnx-community/gliner_multi-v2.1`, `onnx/model_fp16.onnx`, CPU** — entities with correct types at 119 ms, no false positives, nothing on tool noise. Leave `enable_spacy=False`.
2. **LLM only when there is something to extract** — for relations and preferences, gated on GLiNER having found anything, which requires flipping `stop_on_success` (D-2) since it is currently never consulted.

This does not make the MCP "100%" of the upstream diagram — it makes it correct. Stage 1 as shipped would add duplicates to a graph whose duplicates are the operator's original complaint.

**What D-3/D-4 being closed leaves behind — and how little.** `settings.py:193-194` still defaults both stages to **enabled** while neither package is installed, so `SpacyEntityExtractor` and `GLiNEREntityExtractor` are built and throw. Measured on the live sidecar rather than assumed: **0** such log lines at rest, **0** after a read (`memory_search`), **2** after a single `store-message` — one per stage, per stored message, on the write path only. They are not a standing drain; they cost exactly as much as the write volume, which today is whatever an operator or the agent explicitly writes, because nothing persists turns automatically (that is D-1, still undecided). Setting `enable_spacy=False` / `enable_gliner=False` is therefore minor housekeeping — two exceptions and two log lines per stored message, zero behaviour change — not a saving. **The lever is D-2**, and its size is the same measurement read the other way: the 1,257-token LLM prefix is also paid once per stored message, so whatever D-1 decides to persist sets the bill.

**Wiring cost if adopted:** `extraction/gliner_extractor.py:407-424` calls `GLiNER.from_pretrained(self._model_name)` and then `.to(self.device)`; the ONNX path needs `load_onnx_model=True, onnx_model_file="onnx/model_fp16.onnx"` and the `.to()` guarded (the ORT wrapper is not a torch module). `ExtractionConfig` has no knob for either — new fields required. `pip install gliner` pulls **torch unconditionally** even on the ONNX path, so use `--index-url https://download.pytorch.org/whl/cpu` or the default Linux wheel drags the CUDA build; image delta ≈ +300-400 MB on the current 908 MB.

### Open — engineering, not decisions

| # | Finding | Evidence |
|---|---|---|
| **M-11** `_link_explicit_mentions` MERGEs entities by bare name, unscoped, unembedded | `short_term.py:685-731`. `MERGE (e:Entity {name, type})` carries **no `deduplication_scope`**, so it cannot match the `{name, type, deduplication_scope}` key every other writer uses — it creates a second node by construction, the same fork closed in `f7ba4a654` but in a function that was not touched. Also sets synthetic non-UUID ids (`$name + ':' + $type`) and no embedding, so the node is invisible to `memory_search`. **Latent, not live**: reachable from `add_message(explicit_mentions=…)`, and the live graph has 0 synthetic ids because nothing passes that argument yet. |
| **M-12** `_link_preference_to_entity` has the same bare-name MERGE | `long_term.py:895-925`. Attaches a caller's preference to whatever node already carries a name — and `APPLIES_TO` **is** scope-granting in `ENTITY_IN_USER_SCOPE`, so this one hands out read access. This is the pattern the fact linker deliberately did not copy. |
| **M-13** Shared Preference nodes leak entities across users | `add_preference` (`long_term.py:702`) does not call `_metadata_with_user_scope`, and `_can_dedupe_preference` (`:1697-1705`) reuses any node with identical text when scope is `None`. Two users writing the same sentence land on **one** Preference node, both getting `HAS_PREFERENCE` — and then `(:User B)-[:HAS_PREFERENCE]->(p)-[:APPLIES_TO]->(A's entity)` satisfies `ENTITY_IN_USER_SCOPE`. Facts are single-owner and do not have this shape; preferences do. |
| **M-14** MCP resources are entirely unscoped | `_resources.py:80,108` — `memory://entities` and `memory://preferences` call the search methods with no `user_identifier` and return every tenant's data. Aura's bridge namespaces and scopes *tools* only (`bridge.go:123-143`); resources bypass it. Not reachable through the current mount, one config change away. |
| **M-15** `get_related_entities` / `get_entity_relationships` take no `user_identifier` | `long_term.py:1428,2897`. No MCP caller today; library-public and unscoped. |
| **M-16** `server.py:145` + `_instructions.py:59` advertise `graph_query` | The fork does not register it — the model is told a tool exists that it cannot call, and it is the tool `memory_get_facts` tells it to prefer over. |
| **M-17** The Neo4j half of identity retirement | `retireLegacyLocalIdentityForAuthulaUser` migrates six Postgres tables and deletes the seed, but nothing rewrites `(:User {identifier: '…0001'})` in the graph. Harmless on this deployment (the husk owns 0 edges) and now unreachable for new writes after `8e51183f1`, but any deployment that wrote memory before first login has an orphaned subgraph and no path back. |

**Method note, repeated because it keeps paying.** The suite's fake clients never execute Cypher, so no unit test in this repo can see any of the scoping defects; and a structural guard keyed on a *naming convention* misses every copy written outside it — which is exactly how M-4 survived a sweep that was believed complete. Discovery mechanisms need their own test.

---

## Onboarding — cost vs value audit (2026-07-25, operator: "troppo lento e serve a poco")

**The complaint is correct and measurable.** The 5-question interview is NOT the problem: one cheap, reasoning-disabled, tool-free LLM call per answer, gated to exactly one, and paced by the operator's typing.

The cost is the final click: `MapProfile` fans one interview into ~3 entities + ~9+N facts + ~6 preferences, and `cmd/aura/memory_onboarding.go` writes them **one MCP call at a time, sequentially, each opening a fresh streamable-HTTP client, handshaking, calling one tool and closing it** (`cmd/aura/memory.go:188-204`) — roughly 15-25 serial round trips inside the one request the operator is watching a spinner on. There is **no latency instrumentation anywhere on that path**, so nobody can measure it. A batched `memory_save_profile` was scoped and deferred.

**And nothing reads the result.** Leg 1 (automatic profile injection) was removed by Amendment #87; leg 2 (`AURA_CONTEXT_MEMORY_RECALL`) is **default false and absent from the live `.env`**. So every artifact carrying actual profile content has **zero guaranteed runtime reader** — the only thing onboarding reliably produces for the rest of the system is the sentinel boolean that stops the wizard reappearing. That is the whole of "serve a poco", stated precisely.

Ranked follow-ups (none started):
1. Reuse one MCP connection across `StoreConfirmed` instead of open-call-close per item — zero product value lost, removes ~20 handshakes from the moment the operator is waiting.
2. Stop `/api/onboarding/status` polling on every cockpit load (`AppShell.tsx:151`): it pays a full MCP round trip to re-read a monotonic boolean, forever.
3. **Write-ordering bug**: the raw-draft message write can fail and `return` **before** the completion sentinel is written (`memory_onboarding.go:67-76`), so a transient failure on the lowest-value artifact reports the operator as never onboarded despite everything else landing.
4. Make the Telegram link+QR mint optional in `CompleteProfile` (currently unconditional on every call, including re-runs).
5. Stale UI copy: both channels still promise an "Agent.md profile" that Amendment #87 deleted (`bot.go:324`, `commands.go:145-146,175`, `resources.onboarding.ts` ×8). The product names a concrete, inspectable artifact and produces opaque graph nodes instead — this plausibly *feeds* the "serve a poco" perception.
6. **Operator decision, not an engineering call**: turn `AURA_CONTEXT_MEMORY_RECALL` on so the profile is actually consumed, or accept pull-on-demand and stop implying otherwise in the UI.

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

- **New migrations** (number at landing via `ls internal/db/migrations/ | tail -1` — never deduce): ~~**1.7**~~ ✅ **landed as `0051_scheduler_approval_reminder`** (`approval_reminded_at` throttle stamp); still open: **2.7** (RLS policies). ~~**2.8**~~ no longer needs one (rescoped 2026-07-27 — see the Wave-2 row). 0.1 is query-only (sqlc regen, no migration).
- **PRD amendments before code** (architecture/policy): **AG-016** (gate policy), ~~**2.8**~~ (dropped — the memory↔task link stays the agent's judgement, not a schema contract), **1.8** (memory tool-surface contract), ~~**1.7**~~ ✅ **shipped as Amendment #92** (revised — `prd.md` §Scheduler pending-approval relay-liveness). Minor PRD env-catalog updates: 1.3/1.4/1.6/1.10/1.11/2.1 (new `AURA_*` knobs); 1.7's `AURA_SCHEDULER_APPROVAL_{REMINDER,GRACE}_SEC` are catalogued and plumbed into `compose.yaml`.

## Execution discipline (standing, per CLAUDE.md)

Every item closes under the standing gates: 85% coverage floor on the `db_integration neo4j_integration` matrix (verify the stricter Skills-gate `db_integration`-only number; daemon/DB-gated code needs daemon-free unit tests), quality-snapshot re-attestation at phase close, no-skip-as-green, E2E score >9.8. Waves are sequential; items within a wave parallelize by subsystem. The compaction SPIKE is the one hard ordering dependency (blocks 1.9 + 2.3). Local full-matrix verification on the live stack beats push-and-wait; never point `db_integration` at the live `aura` DB (use the disposable-DB path — `scripts/coverage_docker.sh` / the `aura_cov` pattern).
