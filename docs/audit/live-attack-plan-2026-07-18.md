# Aura — Live-System Attack Plan (reliability-first)

Plan date: 2026-07-18

Repository: `d:\Repo\Aura`. Live stack running at audit time (aura:local + Postgres 18.4 + Neo4j 5.26 + 7 sidecars).

## Method

Grounded on a **fresh full-stack live audit sweep** — 8 parallel read-only subsystem auditors (agent loop/runner, tools/shell-fs/sandbox, memory/context/compaction, MCP/governance, scheduler/channels/delivery, AG-UI/web/API, auth/identity/security, knowledge/ingestion/infra) — plus the same day's operator-reported runtime bug audit (`operator-reported-runtime-bugs-2026-07-18.md`, bugs BUG-1..7). Every finding cites `file:line`. No code was modified. The June 2026 industrial audit (`bug-report.md` etc.) is superseded where they conflict — e.g. its conversations/approvals IDOR concern is now **closed** (`internal/agui/owner_scoping_test.go`).

Ordering axis (operator-chosen): **reliability first** — make every exposed feature actually work end-to-end before hardening or feature-completeness. Two agents (MCP, AG-UI) were reviewed while the safety classifier was offline; their findings are marked ⚠︎VERIFY and must be re-confirmed before their fixes are executed.

## Red-team reconciliation (2026-07-19)

Every load-bearing finding was adversarially re-attacked by 6 independent red-team agents (default stance: "the finding is wrong until the code proves it"). Outcome: **the reliability P0/P1 core all survived; the security cluster all downgraded to LATENT; four items narrowed.** No top-severity reliability item was a false alarm. Net effect on this plan: Wave 0 gets **leaner and more honestly worded**, the compaction SPIKE gets a strong steer toward **removal**, and the security items are confirmed correctly placed in Wave 3 (not live emergencies).

| Finding | Verdict | Correction folded into the plan |
|---|---|---|
| 0.1/0.2 compaction evaluator crash | **CONFIRMED** | Reword "crash"→"silent permanent **goroutine** death" (daemon keeps serving). Blast radius bounded: the rollout windows are **never populated by any telemetry path**, so the evaluator is vestigial — reinforces removal. |
| Pattern A / 1.9 compaction engine dark | **CONFIRMED (strengthened)** | `FinalizeCompaction`'s only caller is a unit test; Preview/Restore act on a permanently empty table; trigger enum values dead. SPIKE default should be **remove**, not wire. |
| 0.3 Cypher hang freeze | **CONFIRMED (high)** | Reword "process-wide"→"per affected `Client`" (wedges `/api/graph/*` + goroutine leak). Hang unhandled; crash already handled. |
| 1.1 wallclock finalize | **CONFIRMED (possibly worse)** | First-trip path yields the error slot, not even the stub. Live on slow/tool-heavy runs, not the common case. |
| 1.2 missed reminders dropped | **CONFIRMED** | Deliberate + test-locked, but the code comment claims the opposite. |
| 1.3 SSE no resilience | **CONFIRMED (live)** | Stands unchanged. |
| 1.8/BUG-3 memory no delete | **CONFIRMED (strengthened)** | Library delete methods exist but are exposed to NO MCP tool by construction. |
| 1.4 `/readyz` lies | **DOWNGRADED** | rerank exclusion is correct-by-design (fail-soft); embed is boot-gated + self-healthchecked. Real gap narrows to **in-process worker liveness + scheduler freshness** (freshness already in `/healthz`, just not enforced). |
| 1.5 ingestion silent drops | **SPLIT** | Enqueue-drop CONFIRMED but **narrow** (`WithoutCancel` defends the common cancel cause → DB-error-only window). Dead-letter "no signal" **REFUTED** — a durable `ingestion_job.dead_letter` event row is written + surfaced in the cockpit; residue is only "no `slog` line." |
| 1.6 MCP Ping/recovery | **DOWNGRADED** | "No active health poll (Ping dead)" TRUE; but reconnect-on-use **does** self-heal a once-mounted stdio sidecar (`bridge_reconnect.go`). Real gaps narrow to: active health poll, the 0-tool boot-race, and the HTTP-managed branch having no reconnect. |
| 1.7 approval delivery | **DOWNGRADED** | By design the approval rides the origin-channel `ask_user` pause with an authenticated resume hook + fail-closed gating — not the Notifier. Residual is a **model-relay liveness** risk (a gated task is silently forgotten if the model never relays the directive), not a routing defect. |
| 0.4 operator lockout | **NARROWED → out of Wave 0** | NOT a total lockout: the operator retains host-CLI + known-password cockpit login. Only the **forgot-password recovery seed** is missing. Recoverability gap, not a live-broken emergency. |
| 3.1 strict/MUSR leak | **DOWNGRADED → LATENT** | The cross-identity read **cannot be armed**: provisioning a 2nd identity hard-refuses while MUSR is off (`errIsolationDisabled`), no CLI identity-create exists, live is single-operator. |
| 2.7 RLS gaps | **DOWNGRADED → LATENT** | Count accurate, but it's a **missing defense-in-depth backstop**, not an open leak (app-level `*ForIdentity` scoping is primary). `document_chunks/embeddings` have no identity column (transitively scoped). |
| 2.5/D fs_edit unrouted | **CONFIRMED (code) / DORMANT** | Real, but latent under the live non-strict profile (all fs tools are host-direct there anyway). Pre-sandbox blocker. |
| orphan components | **CONFIRMED** | Stands unchanged. |

## The two diseases

40+ findings, but they are two patterns repeating across every subsystem. The plan attacks the patterns, not the symptoms.

### Pattern A — "Built but not wired" (dark code)
Sophisticated, unit-tested code with **zero runtime callers**:
- **Durable semantic-compaction engine** — `ClaimCompaction`/`MarkCompactionInferenceStarted`/`FinalizeCompaction` (`internal/conversations/store_compaction.go:65,129,160`), budget preflight (`compaction_budget.go`, `internal/llm/capabilities.go`), reconstruction (`compaction_reconstruct.go`), rebase (`compaction_rebase.go`, `semantic_units.go`) — no caller but `cmd/compaction-test-worker`. Prod context safety runs **only** on the lexical L2.5 drop-oldest-pairs path (`internal/conversations/context.go:400`).
- Atomic scheduler `CreateRunAndAdvance` (`internal/cron/store.go:241-262`) — prevents duplicate-fire, wired nowhere; live path splits the two writes.
- MCP `Ping` liveness probe (`internal/mcp/transport.go:15`, `client.go:320`, `http_client.go:150`) — no production caller.
- Orphan web panels: `web/src/conversations/CompactionHistory.tsx:18`, `web/src/health/RuntimeHealthPanel.tsx:128` (superseded by `RuntimeStatusChip`) — built, tested, never mounted.
- Strict-profile validation matrix (`internal/config/config_validate.go:88-274`) — inert because live runs `AURA_PROFILE=dev` (`.env:40`).
- Conversation export handler (`internal/agui/share_export.go:39`), skills PATCH/DELETE (`internal/agui/governance_write_skills_api.go:44-45`) — no UI caller.

### Pattern B — "Silent death / silent loss" (no dashboard)
The system fails without saying so:
- Compaction rollout evaluator **logs-and-dies** → frozen control plane + auto-rollback safety gate stops firing (`cmd/aura/serve.go:217-223` → `internal/conversations/compaction_rollout.go:152-168`). **Happening now.**
- Cypher client: unbounded `ReadBytes` with no ctx deadline under a global mutex (`internal/knowledge/client.go:215`) → a hung `mcp-neo4j-cypher` freezes **every** graph op process-wide, forever.
- Missed reminders silently dropped on restart (`internal/cron/handlers/reminder.go:24-29` + `recover.go:90-93`).
- Discarded embed-enqueue error → doc stays sparse-only forever, ingest reports success (`internal/documents/service.go:142`); dead-letter emits no log (`jobs_worker.go:96`).
- `/readyz` green while embed/rerank/worker/evaluator are dead (`cmd/aura/serve.go:585`).
- SSE run bound to `r.Context()` with no heartbeat/resume → a network blip kills the whole turn (`internal/agui/server.go:395`, `web/src/chat/sseAdapter.ts:518-540`).
- Wallclock finalize runs on the already-expired bounded ctx → real answer degrades to terse stub on the most common exhaustion path (`internal/agent/llm_agent_finalize.go:201-215`).

### What is solid (do NOT touch needlessly)
MCP governance core (trust-classification F-027 closed, append-only immutable `mcp_audit`, fail-soft boot, orphan reaping, reconnect breaker); session-cookie crypto (canonical-base64 tamper guard); password-reset (attempt caps + `FOR UPDATE` + no enumeration oracle); deprovision saga (idempotent/resumable); backup (retention + 24h missed-backup alert + restore drill); agent-loop budget atomicity, pause/resume single-tx, text_response-siblings rejection, parallel-tool panic recovery; embed-dimension refuse-to-start; compose secrets use `${VAR:?required}` fail-fast. No files >600 LOC and essentially no TODO/FIXME across the audited surface.

> **Reconcile note (secrets):** the auth audit flagged "empty `GARAGE_RPC_SECRET` accepted" (D1) under the dev profile; the infra audit found compose uses `${GARAGE_RPC_SECRET:?required}` (boot fails if unset). Both are true: compose forces the var to be *set*, but the strict-profile gate that would reject a *sample/weak* value is dormant. The real gap is "strict gate off", not "no secret".

---

## Wave 0 — Stop the bleeding (P0, live-broken now)

Goal: nothing dies in silence; the currently-frozen subsystems come back.

| # | Item | Evidence | Fix summary | Verification (DoD) |
|---|------|----------|-------------|--------------------|
| 0.1 | Compaction rollout evaluator — silent permanent goroutine death (deterministic duplicate-key) | `internal/db/queries/compaction_rollout.sql:12-17` (no `ON CONFLICT`); `internal/conversations/compaction_rollout.go:83-105,141-148,157`; `compaction_rollout_store.go:125-141` (`{}` seed). **Red-team: CONFIRMED; goroutine death not process crash; windows never populated by any telemetry.** | `ON CONFLICT (scope_id, evidence_digest) DO NOTHING`/`DO UPDATE … RETURNING` (keep the id for the decision insert); tolerate 23505 in `Run` (`continue`, not `return`); treat empty `{}` windows as "no observation" not `L0Retention=0`. **Note:** if the SPIKE picks *remove*, this whole control plane is deleted instead — do the minimal `ON CONFLICT` only if it must survive the SPIKE window. | Evaluator survives ≥24h, no `evaluator stopped` log |
| 0.2 | Detached goroutines log-and-die → supervise | `cmd/aura/serve.go:217-223`; census below | Shared supervisor (restart + capped backoff + health signal); reserve termination for ctx-cancel | Killing the evaluator goroutine → auto-restart within backoff; health signal flips |
| 0.3 | Cypher client hang freezes graph ops **through the affected client** | `internal/knowledge/client.go:199,215` (unbounded `ReadBytes` under `mu`, no deadline). **Red-team: CONFIRMED high; wedges `/api/graph/*` via the boot client + goroutine leak; hang unhandled, crash already handled.** | Per-call read deadline (goroutine-wrapped read + `ctx`/timeout select, mirroring `initializeWithContext`); don't hold `mu` across an unbounded read | Inject a hung Cypher → op fails fast with ctx error; other graph ops proceed |

**Wave 0 is small, low-risk, and unblocks the most.** 0.1+0.2 stop the silent evaluator death; 0.3 removes the worst whole-subsystem freeze. (Operator lockout moved to Wave 2 item 2.11 — the red team showed it is a recovery-path gap, not a live cockpit lockout.)

---

## Compaction decision gate (SPIKE — precedes any Wave-1 compaction wiring)

The durable semantic-compaction engine (Pattern A) is the biggest single question. **Do not wire it blind.** A dedicated spike decides wire-vs-remove with cost/risk data.

**Red-team steer → default to REMOVE.** The red team confirmed the engine is dark end-to-end: `FinalizeCompaction`'s only caller is a unit test; the wired Preview/Restore operate on a `compaction_checkpoints` table the producer never fills (so Preview returns `ErrNoRows`→empty); the rollout evaluator's windows are never populated by any telemetry path; and the `Proactive/Boundary/Idle/Overflow` triggers are dead enum values. This is thousands of LOC of tested-but-unreachable code + a crashing evaluator + an orphaned UI, for a feature that has never run. The burden of proof is now on *keeping* it.

- If **remove**: delete the producer chain (`store_compaction.go` claim/finalize), budget preflight (`compaction_budget.go`, `llm/capabilities.go` compaction path), reconstruction/rebase, the rollout control plane + its migrations, and the compaction UI shell; document lexical-only in an ADR. This also deletes the 0.1 crash outright.
- If **wire**: producer in the agent loop + a telemetry path feeding the rollout windows + the generation-monotonic guard for restore (`store_compaction.go:255` — a restore-to-older currently wedges all future compaction via `UNIQUE(conversation_id,branch_id,generation)`, latent P1) + budget preflight + fail-closed reconstruction (`compaction_reconstruct.go:47-55` silently blanks missing turns today). Large, multi-phase.
- Output: an ADR + a go/no-go. **Wave 1.9 and Wave 2.3 are gated on this decision.** Until then, Wave 0.1/0.2 only *stabilize*; do the minimal `ON CONFLICT` only if the control plane must survive the SPIKE window.

**Hard constraint — dual provider (OpenRouter + llama.cpp/privacy).** Aura runs inference on both OpenRouter (cloud) and llama.cpp (local, for privacy). Any context-management choice MUST be provider-agnostic and Aura-side: OpenRouter's provider-side `middle-out`/context-compression transform covers only the OpenRouter path and does nothing for llama.cpp, so it can never be the strategy — at most an OpenRouter-only overflow fail-safe (item 1.11). If the SPIKE picks *wire the semantic engine*, its summarization on the llama.cpp path MUST use the local model — routing private conversations to a cloud provider to "compress" them defeats the reason the llama.cpp path exists. The local lexical ladder (`dropOldestPairs`) + correct token estimation (item 1.10) therefore stay load-bearing on the privacy path under **every** SPIKE outcome.

---

## Wave 1 — Wire the cables + turn on the dashboard (P1)

Goal: every exposed feature works E2E and is observable when it breaks.

| # | Item | Evidence | Fix summary |
|---|------|----------|-------------|
| 1.1 | Wallclock finalize dead-on-arrival | `internal/agent/llm_agent_finalize.go:201-215,127-137`; comment wrong at `:209-213` | Derive synthesis/critic ctx from `context.WithoutCancel(ic.Ctx)` + fresh `TotalTimeoutSec` (pattern `maybeAutoTitle`/`flushPause` already use) |
| 1.2 | Missed reminders silently dropped on restart | `internal/cron/handlers/reminder.go:24-29`; `recover.go:90-93`; `recover_test.go:130-208` | Decide: catch-up-once (like agent_job/backup) or explicit retire; fix the false doc comment either way |
| 1.3 | SSE has no resilience | ⚠︎VERIFY `internal/agui/server.go:395`; `server_sse.go`; `web/src/chat/sseAdapter.ts:518-540` | Server heartbeat (SSE comment) during idle; client reconnect + `Last-Event-ID` resume; decouple run lifetime from a single fetch stream |
| 1.4 | `/readyz` gap (narrowed by red team) | `cmd/aura/serve.go:585,373-378` | Gate readiness on **in-process worker liveness + scheduler freshness** (`scheduler_last_tick` is already computed in `/healthz`, just not enforced). Do NOT gate on rerank (fail-soft by design) or embed (boot-gated + self-healthchecked) |
| 1.5 | Ingestion silent-drop (narrowed by red team) | `internal/documents/service.go:142` | Add the missing `slog` line at enqueue-drop + dead-letter, and queue-depth/embed-latency metrics. Dead-letter already writes a durable `ingestion_job.dead_letter` event surfaced in the cockpit — this is observability polish, not a silent-loss fix |
| 1.6 | MCP no active health poll (narrowed by red team) | `internal/mcp/transport.go:15` (Ping dead); `cmd/aura/main.go:301` (10s clamps retry); `mount.go:32-42` (HTTP branch no reconnect) | Wire `Ping` into a bounded background poll; fix the 0-tool boot-race (unclamp retry / allow remount); add reconnect-on-use to the streamable-HTTP branch. **Reactive reconnect for once-mounted stdio already works (`bridge_reconnect.go`) — don't rebuild it** |
| 1.7 | Scheduled-approval model-relay liveness (reframed by red team) | `internal/agent/tools/task.go:271-290`; `cmd/aura/serve_adapters.go:365` | The approval rides the origin-channel `ask_user` pause by design (authenticated resume, fail-closed) — NOT a routing bug. Fix the residual: a gated task is silently forgotten if the model never relays the directive → add a fallback surfacing / a pending-task reminder so it can't be lost |
| 1.8 | Memory has no delete/update/list | `docker/agent-memory/src/neo4j_agent_memory/mcp/_tools.py` (create/read/update only) | Add `memory_forget` (delete-by-id, returns removed ids), `memory_update`, `memory_list_by_type`; shape as one `memory` action-verb (Cursor/Devin parity). See runtime-bug audit BUG-3 |
| 1.9 | Compaction engine wiring | *gated on the SPIKE decision* | If go: producer + generation guard + budget preflight + fail-closed reconstruction. **Must be provider-agnostic; summarization on the llama.cpp/privacy path must use the LOCAL model, never a cloud egress** |
| 1.10 | Token-estimation accuracy on the llama.cpp path | `internal/llm/capabilities.go:108-113` (hardcoded `1.15`/`256`, ignores per-provider `Estimator` fields) | Fix the conservative token estimator so the local lexical trim (`context.go` hardCap) cannot under-count on llama.cpp — there is **no provider-side middle-out net** there, so an under-trim is a hard local overflow. Load-bearing regardless of the SPIKE outcome |
| 1.11 | OpenRouter hard-overflow fail-safe (NOT a compaction strategy) | `internal/llm/*` OpenRouter request path; likely BUG-6b case (b) | Enable OpenRouter `transforms:["middle-out"]` / context-compression **only on the OpenRouter provider** as a belt against a hard 400 "context length exceeded" if the local trim mis-counts. **Explicitly scoped to OpenRouter — it does nothing for llama.cpp, which is why primary context management stays Aura-side (1.10 + the ladder). Middle-out is lossy truncation, not summarization, and drops messages without tool-call pairing awareness — do not treat it as the compaction mechanism** |

---

## Wave 2 — Complete + UX (P2)

| # | Item | Evidence | Fix summary |
|---|------|----------|-------------|
| 2.1 | notify missing Telegram | `internal/agent/tools/task.go:119`; `internal/cron/notify.go:34-38,107-118` | Add `RouteTelegram` end-to-end (constants, resolveRoute, buildSend via ChannelDeliverer, tool enum, cockpit dropdown, server-side enum validation) |
| 2.2 | Long scheduled output dropped (>4096) | `internal/channels/telegram/deliver.go:56` | Reuse interactive chunking (`renderer.go:203-218`) on the origin-channel push path |
| 2.3 | Compaction UI orphaned | *gated on SPIKE*; `web/src/conversations/CompactionHistory.tsx:18`; `useCompactions.ts` (no diff) | If engine kept: mount the panel in the shell, add the `diff` action, decide "compact now" activation endpoint. See BUG-7 |
| 2.4 | No profile-edit tool (Agent.md) | `internal/profile/store.go`; `cmd/aura/main.go:188-247` (no profile tool) | Add a deferred, mutating `profile` builtin backed by `internal/profile.Store`. Correct the false memory record (file DOES exist on disk). See BUG-4 |
| 2.5 | `fs_edit`/`fs_grep`/`fs_glob` bypass the box | `cmd/aura/main.go:232-234` (no Router) | Give them a `Router` like `fs_read`/`fs_write` — **blocker before any sandbox enablement** (fails open under strict today) |
| 2.6 | docker_integration = 0% CI coverage | `internal/sandbox/usersandbox/docker_backend_*` (all `//go:build docker_integration`) | Add a `docker_integration` CI job (or daemon-free unit tests for pure logic) **before** sandbox rollout |
| 2.7 | RLS backstop missing on 7 tables (LATENT per red team — DiD gap, not open leak) | migrations; missing on `assets`/`documents`/`storage_objects`/`scheduler_tasks`/`pending_notifications`/`provisioning_saga` (+ `document_chunks`/`embeddings` have no identity column → transitively scoped) | Add owner-isolation policies to close the defense-in-depth gap ADR-0039 already claims. App-level `*ForIdentity` scoping is the primary control and is present; this is belt-and-suspenders |
| 2.8 | Memory→task cascade | no linkage exists (`internal/cron` has zero memory refs) | Provenance edge + delete hook that cancels the linked task. New feature → PRD amendment. See BUG-3b |
| 2.9 | Dead `sandbox_exec` reference (live-reachable) | `internal/skills/skill_read.go:140` | Remove the dead escalation instruction + stale docblock (`snippet.go:15-23`) |
| 2.10 | Orphan/unwired cleanup | `RuntimeHealthPanel.tsx:128`; `share_export.go:39`; `governance_write_skills_api.go:44-45` | Delete the dead panel; wire export button + skills PATCH/DELETE UI, or remove the endpoints |
| 2.11 | Operator forgot-password recovery seed absent (moved from Wave 0 by red team) | memory `aura-phase-43-breakglass-status`; `cmd/aura/recover_operator.go` | Run the manual `aura identity recover-operator` on the host. **Not a live lockout** — operator retains host-CLI + known-password login; this only restores the web forgot-password path |

---

## Wave 3 — Hardening / prod-readiness (P2-P3)

| # | Item | Evidence | Fix summary |
|---|------|----------|-------------|
| 3.1 | Strict-profile dormant (LATENT per red team — leak cannot arm today) | `internal/config/config_validate.go:88-274,245-253`; `.env:40`; guard `internal/agui/onboarding_provision.go:160-162` | Set a strict profile + real creds + `AURA_MUSR_ISOLATION=true`; boot fails fast until satisfied. The cross-identity read is currently **unarmable** (provisioning a 2nd identity hard-refuses while MUSR is off; no CLI identity-create; single-operator live). Becomes real the instant a strict profile + a 2nd identity are introduced — do this **before** that switch |
| 3.2 | Unbounded sidecar memory on RAM-constrained host | `compose.yaml` (only `aura` has `mem_limit`) | Add `mem_limit` to Neo4j/embed/rerank/ocr/markitdown so an OOM is contained, not host-wide |
| 3.3 | SSRF enforce off for HTTP MCP | ⚠︎VERIFY `internal/mcp/transport.go:46-53` (`AURA_MCP_SSRF_ENFORCE` set nowhere) | Enable private-range block for trust-approved remote_http servers |
| 3.4 | `changeme` PIM admin token ungated | `internal/config/config.go:222,498` | Add a profile gate rejecting the default (parity with object-store sample-cred rejection) |
| 3.5 | Coverage blind spots | `runner/*_test.go` `//go:build live_e2e`; docker_integration (2.6); HTTP-boundary owner tests (documents/assets) | Bring `live_e2e` + `docker_integration` into CI; add handler-level foreign→404/403 red-team tests for documents/assets |
| 3.6 | Compaction telemetry + retention sweeper | `compaction_metrics.go` (no sink); `memory/compaction_policy.go:128` `Expire` (manual-only) | Wire a metric sink; schedule the retention `Expire` sweep (GDPR/retention drift) |
| 3.7 | Per-request identity DB lookup = mass-logout coupling | `internal/agui/auth.go:229` | Short-TTL identity cache so a DB blip doesn't force a site-wide logout |

---

## Cross-cutting fixes (fold into Wave 0-1, not separate waves)

- **Goroutine supervision** (0.2) — one shared supervisor pattern applied to every detached goroutine, kills the whole Pattern-B silent-death class at the root.
- **Honest readiness** (1.4) — `/readyz` that reflects real subsystem health kills the "green while broken" class.
- **Deadlines on blocking reads** (0.3 + 1.6) — no unbounded external read without a ctx deadline.

## Silent-death census (detached goroutines that log-and-exit)

- `cmd/aura/serve.go:218` compaction rollout evaluator — **true silent death** (0.1/0.2); also carries auto-rollback.
- `cmd/aura/serve.go:197` AG-UI `ListenAndServe` — logs+exits, but `/readyz` is served by the same server so its death IS externally caught (lower risk).
- Verified resilient (proper ctx/join, listed so they're not "fixed" needlessly): `asset_processing_worker.go:83`, `conversations/sweeper.go:74`, `gateway/reconcile.go:122`, `cron/heartbeat.go:26`, `knowledge/client.go:95`.

## Execution mapping

Each wave item is a GSD-phase candidate: `/gsd-spec-phase` → `/gsd-discuss-phase` → `/gsd-plan-phase` → `/gsd-execute-phase` → Gate-3 (verify/review/secure/coverage). Waves are sequential; items within a wave can parallelize by subsystem. Every close obeys CLAUDE.md gates (85% coverage floor on the `db_integration neo4j_integration` matrix, quality snapshot, no skip-as-green, E2E score >9.8). The compaction SPIKE is the one hard ordering dependency (blocks 1.9 and 2.3).

## Open decisions to confirm before execution

1. Compaction: spike outcome (wire vs remove) — **gates 1.9 + 2.3**.
2. Reminders (1.2): catch-up-once vs explicit retire — product choice.
3. AG-016 force-gate of every agent_job (`task.go:216-218`) — keep as-is (deliberate security posture) or make conditional. Policy decision, not a bug.
4. Strict-profile timing (3.1): stays Wave 3 only while the box is single-operator; promotes to P0 the moment a 2nd identity is provisioned.
