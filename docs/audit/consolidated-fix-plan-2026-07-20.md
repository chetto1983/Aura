# Aura — Consolidated Fix Plan

Plan date: 2026-07-20
Supersedes-as-execution-index: `operator-reported-runtime-bugs-2026-07-18.md` (BUG-1..7) + `live-attack-plan-2026-07-18.md` (Waves 0–3).

## Provenance

This is the single execution truth-source that reconciles the two 2026-07-18 audits, folds in a 2026-07-20 **adversarial code-verification pass** (17 read-only verifier agents, workflow `wenpc28z1`; default stance "the finding is wrong until the code proves it"), records the **operator decisions**, and captures a **newly-discovered bug (BUG-8)** found this session by live DB inspection and already shipped.

- **Verification outcome:** all 17 load-bearing reliability/operator-bug findings came back **CONFIRMED** or **CONFIRMED_NARROWED** — **zero REFUTED**. No fix in this plan rests on an unverified claim. Three audit fix-directions were corrected in flight (see 0.1, 0.2, 2.1).
- **Ordering axis:** reliability-first — make every exposed feature work E2E before hardening.
- **Sequencing constraint:** the **compaction SPIKE** is the one hard dependency (gates 0.1's scope, 1.9, 2.3).

## Decisions (locked 2026-07-20)

1. **Compaction engine** → ~~run the SPIKE first~~ **SPIKE RESOLVED 2026-07-20 = REMOVE** (operator-ratified, PRD Amendment #86). The dark Phase 42 `llm-conversation-compaction` engine (migrations 0036-0039) is deleted in a dedicated removal phase; **1.9/2.3 = NO-GO**. The anti-rot core is **L4 extractive graph memory** — L4 archival recall SHIPPED (`AURA_CONTEXT_MEMORY_RECALL`, commit `340d6966`, CI-green). Wave 0.1 crash-guard stays until the delete lands. ADR: `docs/audit/compaction-spike-2026-07-20.md`.
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
| BUG-7 compact UI orphaned | 2.3 (gated on SPIKE) | 2 | verified |
| BUG-4 no profile-edit tool | 2.4 | 2 | verified |
| BUG-3b memory→task cascade | 2.8 | 2 | verified (needs migration + PRD) |
| BUG-5 superfluous tool calls | prompt-tuning task | side | behavioral |
| BUG-1A AG-016 force-gate | AG-016 redesign | 1 | verified; policy decision |
| — (new, this session) | **BUG-8 context gauge** | done | **✅ SHIPPED + DEPLOYED** |

Two root patterns, not 40 bugs: **A — built-but-not-wired (dark code)**, **B — silent death/loss (no dashboard)**. Attacked via three cross-cutting fixes woven through Waves 0–1: per-tick self-heal (0.2), honest readiness (1.4), deadlines on every blocking external read (0.3 + 1.6).

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

## Compaction SPIKE (gate — precedes 1.9 + 2.3) — ✅ RESOLVED 2026-07-20 = REMOVE

**Outcome: REMOVE** (operator-ratified, PRD Amendment #86; ADR `docs/audit/compaction-spike-2026-07-20.md`). Verified dark end-to-end (`FinalizeCompaction` zero non-test callers; Preview/Restore on a never-filled table; rollout windows never populated; trigger enums dead; cost one P0 = BUG-6a). It is **redundant on top of amendment #21's shipped 5-layer defense**, not the defense itself. Verdict: delete the Phase 42 engine + migrations 0036-0039 in a dedicated removal phase (drop-migrations at the next free slot; Wave 0.1 crash-guard stays until then); **1.9 = NO-GO, 2.3 = NO-GO**.

Hard constraint honored: context management stays Aura-side + provider-agnostic (the amendment #21 ladder + 1.10 token estimation remain load-bearing). **The anti-rot core is L4 extractive graph memory** — the industrial survey (Anthropic native compaction/context-editing, mem0 v3, tokenjuice, Letta, neo4j agent-memory) concluded transcript compaction is the one layer the market gives away, while retrieval-of-salient-facts is the durable rot defense Aura is uniquely positioned for (Neo4j in-stack). **L4 archival recall is now SHIPPED + CI-green** (runner `ArchivalRecaller` seam → memory MCP `get_context` long-term, scoped by identity, query-keyed; `AURA_CONTEXT_MEMORY_RECALL`, commit `340d6966`).

---

## Wave 1 — Wire the cables + turn on the dashboard (P1)

| # | Verdict | Fix (verified) | migration / PRD | risk |
|---|---|---|---|---|
| **1.1** Wallclock finalize DOA | CONFIRMED (high) | Derive synthesis/critic/recovery ctx from `context.WithoutCancel(ic.Ctx)` + fresh `TotalTimeoutSec` `WithTimeout` (keeps request/trace values, severs the expired deadline). Pattern already used by `maybeAutoTitle`/`flushPause`. | no / no | low |
| **1.2** Missed reminders dropped | CONFIRMED (high) | Flip `ReminderHandler.Meta().ReschedulesOnRecovery` false→true (catch-up-once, collapsed, carries `MissedSince`); fix the false comments (`reminder.go:24`, `serve.go:322`); update 3 tests (justified behavior change, not babysitting). | no / PRD note | med |
| **1.3** SSE no resilience | CONFIRMED (high) | Tier A: idle SSE-comment heartbeat ticker in `streamSSE` drain loop (drain goroutine owns `w`, race-free) + client tolerance. Tier B: client reconnect + `Last-Event-ID` resume; decouple run lifetime from the single fetch. | no / env-var (PRD catalog) | med |
| **1.4** `/readyz` lies | CONFIRMED (high) | Add a `scheduler` readiness probe gating on tick freshness via existing `LastTick()`; pure `ReadinessStale(maxStale)` with injectable clock (nil during boot grace). Do NOT gate rerank/embed. | no / PRD note | low |
| **1.5** Ingestion silent drop | CONFIRMED_NARROWED (high) | Observability only (durable dead-letter row already exists): `slog.Warn` at embed-enqueue drop + dead-letter branches; queue-depth/embed-latency metrics. Fail-soft behavior unchanged. | no / no | low |
| **1.6** MCP no health poll | CONFIRMED_NARROWED (high) | Primary: give the streamable-HTTP managed branch the same reconnect-on-use as stdio (generalize `reconnectingServer.reopen`). Plus bounded `Ping` background poll + fix the 0-tool boot-race. Stdio reactive reconnect already works — don't rebuild. | no / env-var | med |
| **1.7** Approval relay-liveness | CONFIRMED (high) | Recurring pending-approval reminder sweep on the scheduler tick (mirrors `sweepNotifications`): re-surface channel-owned `pending_approval` tasks via `ChannelDeliverer` with a throttle stamp. Fixes the "gated job silently forgotten" residual (BUG-1B). | **migration** (throttle ts) / PRD | med |
| **1.8** Memory no delete/update/list | CONFIRMED_NARROWED (high) | Action-verb `memory_manage` (forget/update/list_by_type) in `mcp/_tools.py`; `memory_forget` MUST enforce ownership (`_node_in_user_scope`, refuse not no-op) and **return removed ids** for BUG-3b. Cursor/Devin parity. | no / PRD | med |
| **1.9** Compaction wiring | ❌ **NO-GO** (SPIKE=remove, Amendment #86) | Superseded by the engine-removal phase; the anti-rot direction is L4 archival recall (shipped `340d6966`), not compaction wiring. | — | — |
| **1.10** Token estimator (llama.cpp) | CONFIRMED_NARROWED (med) | Add `DeclaredErrorTokens` on the llamacpp capability row + `ProviderErrorReserveTokens` subtracted in `hardCap()`; retire the dead conservative `1.15/256` fields/function. Load-bearing regardless of SPIKE. | no / env-note | med |
| **1.11** OpenRouter overflow fail-safe | CONFIRMED_NARROWED (high) | Opt-in `transforms:["middle-out"]` on `wireRequest`, set ONLY when `ReasoningTarget==OpenRouter` AND a config knob is on (`omitempty` keeps llamacpp byte-unchanged). Explicitly NOT the compaction mechanism (lossy, no tool-pair awareness). | no / env-var | low |

---

## Wave 2 — Complete + UX (P2)

| # | Verdict | Fix (verified) | migration / PRD | risk |
|---|---|---|---|---|
| **2.1** notify no Telegram (BUG-2) | CONFIRMED (high) | **Correction: `telegram` cannot be a `buildSend` branch** (`compositeNotifier` has no `ChannelDeliverer`/identity). Intercept at the `Dispatch` layer (`dispatch.go`) where `task.IdentityID` + `ChannelDeliverer` exist; add server-side enum validation (bad route 400s, no silent stdout-degrade); tool enum + cockpit dropdown. | no / PRD | med |
| **2.2** Long TG output dropped (>4096) | (audit) | Reuse interactive chunking on the origin-channel push path. | no / no | low |
| **2.3** Compaction UI orphaned (BUG-7) | ❌ **NO-GO** (SPIKE=remove, Amendment #86) | Nothing to mount — the `CompactionHistory` surface + `compaction_memory_api` are deleted in the removal phase. | — | — |
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
