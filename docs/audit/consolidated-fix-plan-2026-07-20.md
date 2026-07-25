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
| BUG-1B approval undelivered | 1.7 (reframed: relay-liveness) | 1 | **✅ SHIPPED 2026-07-24** (CI green) |
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
| **2.1** notify no Telegram (BUG-2) | CONFIRMED (high) | **Correction: `telegram` cannot be a `buildSend` branch** (`compositeNotifier` has no `ChannelDeliverer`/identity). Intercept at the `Dispatch` layer (`dispatch.go`) where `task.IdentityID` + `ChannelDeliverer` exist; add server-side enum validation (bad route 400s, no silent stdout-degrade); tool enum + cockpit dropdown. | no / PRD | med |
| **2.2** Long TG output dropped (>4096) | (audit) | Reuse interactive chunking on the origin-channel push path. | no / no | low |
| **2.3** Compaction UI orphaned (BUG-7) | ✅ **DONE — removed** (`28c12351`) | `CompactionHistory` + `useCompactions` + `resources.compaction` + `compaction_memory_api` deleted; bundle rebuilt. | — | — |
| **2.4** No profile-edit tool (BUG-4) | CONFIRMED (high) | Deferred, action-multiplexed MUTATING `profile` builtin (show/add-fact/edit-section/remove-fact) via ActionRouter, backed by `internal/profile.Store` (add `Store.RemoveFact` + `Store.ReplaceSection`; only `AddFact` exists). Never expose the raw Agent.md path to `fs_write`. Correct the false memory record. | no / PRD | med |
| **2.5** `fs_edit/grep/glob` bypass box | (audit) | Give them a `Router` like `fs_read/write` — **blocker before any sandbox enablement**. | no / no | med |
| **2.6** docker_integration 0% CI | (audit) | Add a `docker_integration` CI job (or daemon-free unit tests) before sandbox rollout. | no / no | low |
| **2.7** RLS backstop (7 tables) | (audit, LATENT) | Owner-isolation policies (defense-in-depth; app-level `*ForIdentity` is primary). | migration / no | low |
| **2.8** Memory→task cascade (BUG-3b) | CONFIRMED (high) | Provenance column on `scheduler_tasks` (mirror `origin_conversation_id`) + Aura-side cascade: on memory-forget success extract removed ids → existing soft-cancel path via a new import-cycle-free `taskStore` seam. | **migration** / **PRD** | med |
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

| # | Defect | Status |
|---|---|---|
| **M-1** Reads projected `canonical_name` as `name` | ✅ closed `df2e055a0` (after `20a4e8d88` + `28fbe5efb` fixed four of the five sites). `memory_get_entity` built its dict elsewhere and was missed; so were the entities-catalog resource and the relationship echo. Confirmed fixed in the live trace: `{"name": "Davide", "canonical_name": "David"}`. The guard no longer checks a literal from the site just fixed — it bans `.display_name` across the whole caller-facing read surface, over the AST, on a file set derived from the package directory, so a read path in a NEW module is covered the day it is written. |
| **M-2** No update verb | ✅ shipped `20a4e8d88` — `memory_update`, see 1.8. |
| **M-3** Canonical resolution unscoped | ✅ shipped `20a4e8d88`. `SEARCH_ENTITIES_BY_TYPE` fed the resolver **every** entity of that type in the database, so a fresh "Marco Bianchi" came back canonicalised onto another user's "Davide" — one user's data corrupting another's, and the stranger's name leaking back through the caller's own results. Now scoped to the caller. |
| **M-4** `*_SCOPED` reads did not scope — **cross-user data leak** | ✅ shipped `20a4e8d88`. Ownership was enforced by a `WHERE` glued directly to an `OPTIONAL MATCH`; in Cypher that constrains the optional pattern, NOT the outer rows, so the filter did nothing. **Conversations (by id and by session), messages, entities, reasoning traces and session lists were readable across users.** Proven on the live graph: querying as a user owning exactly one entity returned another user's entity at score 0.999; after the fix, only its own. Seven queries corrected — six shipped, the seventh added the same day by copying the broken shape, which is how the pattern propagates. The in-memory fake clients the suite uses never execute Cypher and structurally could not catch it; a new structural test parses `queries.py` and fails any `*_SCOPED` query written that way. |
| **M-5** Entities written with no embedding → invisible to `memory_search` | 🔧 in progress. `short_term.py:1141,1325` pass `"embedding": None` hardcoded, never consulting the embedder, so every entity born from message extraction is unsearchable (the operator's own two "David" nodes are). Also: no uniqueness constraint backs the bare `MERGE (e:Entity {name, type, deduplication_scope})`, so concurrent writes can duplicate; and `GET_ENTITIES_WITHOUT_EMBEDDINGS`/`UPDATE_ENTITY_EMBEDDING` exist with **zero callers** — a repair path designed and never wired. |
| **M-7** `memory_update` reaches the sidecar with **no caller identity** — every correction the agent attempts is refused | 🔧 fix written + verified, **NOT committed** (Go, blocked on the `internal/adaptive` lint). Patch: `D:/tmp/bridge-user-identifier.patch`. `internal/agent/mcptools/bridge.go:144` decided which memory tools get `user_identifier` injected from a **hand-kept name list**, and `memory_update` was never added to it when the verb shipped. The server treats a missing identifier as "no scope" on its write verbs, so it answered `"not found or not owned by this user"` — about the operator's own entity, which they own by a direct `HAS_ENTITY` edge. Proven on the live graph: the same scope query returns `scope_count=0` with a null identifier and `1` with the operator's. The agent then falls back to `add_*`, which merges instead of correcting and **creates duplicates** — the very failure the update verb was added to end. The list is now derived from the tool's own advertised schema, so a verb added in the sidecar cannot be forgotten in the bridge. |
| **M-6** Duplicate "David" nodes | root-caused, no fix needed. They differ by `type` (`OBJECT` vs `PERSON`), and the MERGE key is `{name, type, deduplication_scope}` — two nodes by construction, no deduplication could ever have merged them. The upstream fault is extraction classifying a person as an OBJECT. |

**Method note.** M-1 through M-4 were found by *using* the tool, M-5/M-6 by reading the graph. The bug class here is uniform: a read that silently returns something other than what it claims (a canonical instead of a name; another user's row instead of yours). Unit tests over fake clients cannot see any of it.

**Open (not started):** the `add_preference` exact-text dedup fallback (`long_term.py:749-775`, shipped `3fb13edee`) was never ported to `add_entity`/`add_fact`, whose dedup is embedding-gated only — with no embedding, dedup silently does not run.

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

- **New migrations** (number at landing via `ls internal/db/migrations/ | tail -1` — never deduce): ~~**1.7**~~ ✅ **landed as `0051_scheduler_approval_reminder`** (`approval_reminded_at` throttle stamp); still open: **2.8** (scheduler_tasks memory-provenance column), **2.7** (RLS policies). 0.1 is query-only (sqlc regen, no migration).
- **PRD amendments before code** (architecture/policy): **AG-016** (gate policy), **2.8** (memory↔task provenance edge), **1.8** (memory tool-surface contract), ~~**1.7**~~ ✅ **shipped as Amendment #92** (revised — `prd.md` §Scheduler pending-approval relay-liveness). Minor PRD env-catalog updates: 1.3/1.4/1.6/1.10/1.11/2.1 (new `AURA_*` knobs); 1.7's `AURA_SCHEDULER_APPROVAL_{REMINDER,GRACE}_SEC` are catalogued and plumbed into `compose.yaml`.

## Execution discipline (standing, per CLAUDE.md)

Every item closes under the standing gates: 85% coverage floor on the `db_integration neo4j_integration` matrix (verify the stricter Skills-gate `db_integration`-only number; daemon/DB-gated code needs daemon-free unit tests), quality-snapshot re-attestation at phase close, no-skip-as-green, E2E score >9.8. Waves are sequential; items within a wave parallelize by subsystem. The compaction SPIKE is the one hard ordering dependency (blocks 1.9 + 2.3). Local full-matrix verification on the live stack beats push-and-wait; never point `db_integration` at the live `aura` DB (use the disposable-DB path — `scripts/coverage_docker.sh` / the `aura_cov` pattern).
