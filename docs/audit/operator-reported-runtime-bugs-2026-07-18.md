# Aura — Operator-Reported Runtime Bug Audit

Audit date: 2026-07-18

Audited repository: `d:\Repo\Aura` (live stack running: `aura:local` + Postgres 18.4 + Neo4j 5.26 + sidecars).

## Source & method

This audit does **not** originate from a static code sweep. Its input is what the **running Aura agent recorded in its own memory** during a live operator session on 2026-07-18, cross-checked against live DB/container state, then root-caused in source by four read-only code-audit subagents. No production code was modified.

- **Bug source**: Aura's Neo4j agent-memory (`Fact` / `Entity` / `Preference` nodes), extracted from the `aura-neo4j` container. Conversation `03b9c7c2-eb3f-5583-b13f-39b23bf4de8b` (166 turns) is the origin.
- **Live corroboration**: `aura-postgres` (`aura.*` schema), `aura` container filesystem/env, `aura` container logs (`docker logs aura`).
- **Root-cause**: four parallel subagents (scheduler, memory-tools/profile, compaction, MCP-inventory), each citing `file:line`.

A notable meta-finding: **Aura's self-diagnosis in memory is sometimes wrong** (see BUG-4). The memory is a symptom log, not a verified defect record.

## Findings summary

| ID | Bug (as Aura recorded it) | Verdict | Severity | Root-cause locus |
|----|---------------------------|---------|----------|------------------|
| BUG-1A | Scheduler jobs go to `pending_approval` even at normal risk / already confirmed | **By-design** (not a defect) | — | `internal/agent/tools/task.go:216-218` (AG-016 override) |
| BUG-1B | The approval request never reaches the configured notify channel; stuck internally | **VALID** | High | `internal/cron/dispatch.go:203-246` vs `task.go:271-290` (no approval-delivery seam) |
| BUG-2 | `notify` supports only whatsapp/email/stdout — no Telegram | **VALID** | Medium | `task.go:119`, `internal/cron/notify.go:34-38,107-118` |
| BUG-3 | No tool to delete/forget a memory (only create/read/update) | **VALID** | High (fn) / Med (GDPR) | `docker/agent-memory/src/neo4j_agent_memory/mcp/_tools.py` (no delete tool) |
| BUG-3b | Deleting a memory should also cancel the linked scheduler task | **VALID gap** (feature) | Medium | No memory↔task linkage anywhere (`internal/cron` has zero memory refs) |
| BUG-4 | Agent.md is not on disk, can't be read/edited | **PARTIALLY VALID** (premise wrong) | Medium | File **exists**; no LLM-facing profile tool + hidden path |
| BUG-5 | Too many superfluous tool calls instead of `shell_exec` for discovery | **VALID** (behavioral) | Low | Prompt/heuristic, not a code defect |
| BUG-6 | Compaction (OpenRouter) "already gives errors" | **VALID** (two distinct problems) | High (subsystem) | `internal/db/queries/compaction_rollout.sql:12-17` (no `ON CONFLICT`) + `internal/llm/capabilities.go:100-104` |
| BUG-7 | The compact command is missing from the (web) UI | **VALID** | Medium | `web/src/conversations/CompactionHistory.tsx` built but **not mounted**; no manual activation endpoint anywhere |

---

## BUG-1 — Scheduler `pending_approval` / approval never reaches the channel

### BUG-1A (sub-claim: normal-risk jobs still gated) — **BY-DESIGN**

`internal/agent/tools/task.go:216-218` unconditionally forces **every** `agent_job` to `pending_approval`, *after* the tier gate already ran (lines 202-210). For `agent_job` the computed tier is Normal (`internal/scoring/scoring.go:99-103`; `GateRecommended(Normal)==false` at `scoring.go:140`), so the tier gate would **not** have gated it — but the AG-016 override does, regardless of tier and regardless of `schedule_kind` (`at`/`cron`/`every` all key on `kind`). The override is deliberate (comment lines 211-218: gate every `agent_job` rather than trust keyword scoring). Reminders/backups (Safe tier) are **not** force-gated and go `active` correctly.

→ Accurate behavior, intentional posture. **Do not "fix" without a policy decision.**

### BUG-1B (approval doesn't reach the notify channel) — **VALID, High**

When gated, `actionSchedule` returns `scheduledApprovalRequiredResult` (`task.go:251-253,271-290`): a plain-text `ToolResult` whose `Preview` is a JSON *instruction to the model* to call `ask_user`.

1. **The approval is never routed to `notify`.** The `notify`/`NotifyRoute` machinery (`internal/cron/notify.go`, `deliver.go`) runs **only at fire time**, inside `Dispatch.notify` (`internal/cron/dispatch.go:203-246`), to deliver task *output*. No code path pushes a `pending_approval` prompt to the notify route.
2. **The pause depends on a soft model relay.** `ask_user` is the only tool that pauses the turn (`task.go:249-250`) and the scheduler tool cannot call it itself — it emits a directive and hopes the model relays. If not relayed, the task sits in `pending_approval` with no notification anywhere.
3. **When relayed, it surfaces in the origin conversation, not the notify channel** (`cmd/aura/serve_adapters.go:354-380`). Telegram renders it via `bot_dispatch_hitl.go:145 promptPendingPause`; a CLI/web-scheduled task can only be approved from the cockpit board (`internal/cron/store_manage.go:47 ApproveTask` → `internal/agui/governance_write_scheduler.go` → `web/src/governance/SchedulerBoard.tsx`).

**Blast radius**: every scheduled `agent_job` (the most common non-trivial scheduled task). A gated job never relayed/approved **never fires, silently.**

**Fix direction**: add an approval-delivery seam that, on `pending_approval` creation, pushes the prompt through the same `ChannelDeliverer`/`Notifier` used at fire time (touch `task.go` emit + `cmd/aura/serve_adapters.go` wiring). Optionally make the AG-016 override conditional on `GateRecommended(tier)` OR destructive payload — **policy decision, confirm intent first.**

---

## BUG-2 — `notify` missing Telegram — **VALID, Medium**

Telegram is absent from the `notify` enum at every layer:

- Tool schema: `internal/agent/tools/task.go:119` → `enum: ["whatsapp","email","stdout"]`.
- Delivery constants: `internal/cron/notify.go:34-38` → only `RouteWhatsApp/RouteEmail/RouteStdout`.
- Route resolution: `notify.go:107-118` (`resolveRoute`) + `buildSend` (142-161). **An unknown value like `"telegram"` silently degrades to stdout** (line 116).
- Cockpit UI: `web/src/governance/SchedulerEditDialog.tsx:205-207` (dropdown whatsapp/email/stdout).
- Cockpit server write: `internal/agui/governance_write_scheduler.go:119,149` passes `body.Notify` with **no enum validation** → inherits the stdout-degrade.

**Nuance**: task *output* can still reach Telegram implicitly via the origin-channel path (`internal/cron/deliver.go:48-56 originGate`) — but only when `notifyRoute` is empty/`stdout` and the task was scheduled *from* Telegram. There is no way to **explicitly** select Telegram, and no Telegram branch in the Notifier.

**Fix direction**: add `RouteTelegram` to `notify.go:34-38`, `resolveRoute`, and a `buildSend` branch that routes through the `ChannelDeliverer` (not the MCP `send_message` self-send); add `"telegram"` to the `task.go:119` enum and the cockpit dropdown; add server-side enum validation so bad routes 400 instead of degrading.

---

## BUG-3 — No memory delete/forget tool — **VALID, High**

The `aura-agent-memory` MCP server is a vendored fork of neo4j-labs `neo4j-agent-memory` (pinned `c1c2d65`) in-repo at `docker/agent-memory/src/neo4j_agent_memory/mcp/_tools.py`. **Every registered `@mcp.tool()` is create/read/update — there is no delete/forget/remove tool.** The only generic escape hatch, `graph_query`, blocks `CREATE/MERGE/DELETE/SET/REMOVE` (`_tools.py:867-891`) **and** is disabled entirely for user-scoped sessions (`_tools.py:877-883`) — which, post-authula cutover (Phase 36), is every operator turn.

Delete capability **exists in the library but is never wired**: `long_term.delete_entity_provenance` (`memory/long_term.py:2150`), `short_term.delete_message` (`memory/short_term.py:1035`), `graph/client.py:226 delete_node_by_id`, `core/memory.py:60 delete`. On the Aura side, `cmd/aura/main.go:188-247` registers **no** memory builtin, so nothing supplements the MCP surface.

Live confirmation: `aura.tool_invocations` shows only `memory__memory_add_fact/add_entity/add_preference/get_entity/store_message/graph_query` ever invoked — no delete.

→ `cancella dalla memoria il baco` is **unexecutable**.

**Severity**: High (functional) / Medium (data-integrity + GDPR-erasure). Every memory write is permanent; stale/incorrect facts accumulate with no removal path.

### BUG-3b — memory→task cascade — **VALID gap (new feature)**

Cron tasks (`internal/cron`, Postgres) and memory (Neo4j) are fully independent — `internal/cron/dispatch.go` has zero memory refs; no linkage table exists. Deleting a memory could not cancel a related task even if a delete tool existed. This must be orchestrated **Aura-side**: a delete hook that (a) removes the Neo4j node via a new MCP delete tool that **returns removed ids**, then (b) calls the existing cancel path (`internal/agent/tools/task.go:15-49` `cancel` action / `internal/cron/store_manage.go`). Needs a memory↔task provenance edge (`task_id` on the node) → **PRD amendment**.

---

## BUG-4 — Agent.md "not editable / not on disk" — **PARTIALLY VALID (premise is wrong)**

Aura recorded: *"Agent.md non esiste come file su disco … fa parte del prompt di sistema."* **This is factually wrong.**

- Agent.md **is materialized on disk**: `internal/profile/store.go:16` (`agentFile="Agent.md"`), atomic `WriteProfile` at `<root>/<identity>/Agent.md` (`store.go:94-104`). Live deploy sets `AURA_PROFILE_DIR=/var/lib/aura/agents` (`compose.yaml:178`) on the `aura-home` volume. **Verified live**: `/var/lib/aura/agents/c2233b3b-.../Agent.md` (mode 0600, contains the operator profile: Davide / PmSync / Caraglio).
- The prompt injects **rendered content**, not the file: `<profile:Agent.md>…</profile:Agent.md>` (`internal/profile/render.go:45-53`) via the runner `contextBlock` provider (`runner.go:177`). The path is never surfaced to the LLM.
- **No LLM-facing profile tool exists** (`cmd/aura/main.go:188-247` — grep `profile` in the registry = nothing). Only the host CLI `aura profile show|add-fact` (`cmd/aura/profile.go:55-72`, append-only) and onboarding write it. The system prompt even references a nonexistent capability: *"Do not silently update Agent.md unless … a profile tool/command performs that update"* (`prompt.go:57`).
- Under a **strict** profile, `fs_write` routes into the per-identity sandbox box (`fs_write.go:62-67`) where `/var/lib/aura/agents` is **not** mounted — so a raw write silently lands in the box, never touching the real profile.

**Actionable defect**: the absence of a profile-edit tool + hidden path — **not** file non-existence.

**Fix direction**: add a deferred, mutating builtin `internal/agent/tools/profile.go` (show / add-fact / edit-section / remove-fact), backed by `internal/profile.Store` (reuse `WriteProfile`/`AddFact`/`ReadProfile` + `RenderAgentMD`), identity-scoped. Do **not** solve by exposing the raw path to `fs_write` — bypasses the version/changelog/atomic/size-cap contract and breaks under strict sandboxing.

**Also correct the memory record** — its stated root cause is false.

---

## BUG-5 — Too many superfluous tool calls — **VALID (behavioral, Low)**

Aura recorded self-critique: too many structured `fs_glob` calls instead of one `shell_exec` discovery (`find / -name Agent.md`). This is a prompting/heuristic issue, not a code defect. Ironically it intersects BUG-4: Aura hunted for Agent.md on the filesystem and failed because the fs tools are jailed away from `/var/lib/aura/agents`. **Fix**: prompt guidance to prefer `shell_exec` for broad discovery; low priority.

---

## BUG-6 — Compaction errors — **VALID (two independent problems)**

### 6a — Rollout evaluator crash (control-plane, deterministic) — **High**

Live log: `aura serve: compaction rollout evaluator stopped … duplicate key … "compaction_rollout_evidence_scope_id_evidence_digest_key" (SQLSTATE 23505)`.

Root cause chain:
- `AppendCompactionRolloutEvidence` (`internal/db/queries/compaction_rollout.sql:12-17`) is a bare `INSERT … RETURNING *` **with no `ON CONFLICT`** (contrast `CreateCompactionRolloutState` at line 6, which *has* `ON CONFLICT (scope_id) DO NOTHING`). Wrapper: `internal/conversations/compaction_rollout_store.go:236-242`.
- The digest is `SHA-256(json.Marshal(Input))` (`internal/conversations/compaction_eval/evaluator.go:55-60`) and `Input` (evaluator.go:31-37) has **no version/timestamp/nonce** → deterministic.
- Feedback loop: `CASRollbackCompactionRollout` (`compaction_rollout.sql:47-59`) zeroes the windows to `'{}'`; next tick `EvaluateOnce` (`compaction_rollout.go:115-148`) reads all `Gates`=0, so `rollbackReason` (`:83-105`, line 94 `g.L0Retention < 1`) returns `safety_gate_failed` → another rollback → **byte-identical Input → identical digest → 23505**. The seed `EnsureDisabledDefault` (`compaction_rollout_store.go:125-141`) seeds `{}` windows, so a fresh `default` scope rolls back on tick 1 and crashes on tick 2 (~1 min later).
- One error kills the loop: `Run` (`compaction_rollout.go:157`) `return err` on any error; goroutine `cmd/aura/serve.go:217-223` logs "evaluator stopped" and exits. **Permanent** — on restart it re-seeds and dies again; immutability triggers (migration `0039_compaction_rollout.up.sql:49-55`) prevent deleting the offending rows.

**Blast radius**: the daemon stays up (detached goroutine, fail-soft), but the **entire compaction-rollout control plane freezes** — no evidence, no canary promotions, no rollbacks; `active_config` frozen.

**Fix direction** (layered):
1. Make the append idempotent: `ON CONFLICT (scope_id, evidence_digest) DO UPDATE SET scope_id=EXCLUDED.scope_id RETURNING *` (the decision INSERT needs `evidence.ID` at `compaction_rollout_store.go:212`, so keep the RETURNING).
2. Tolerate 23505 in `Run` — classify and `continue` instead of `return err`; reserve termination for `ctx` cancellation.
3. Root fix: treat empty `'{}'` windows as *no observation yet*, not `L0Retention=0` failing the safety gate — skip evaluation on unpopulated windows. (1)+(3) is the real fix; (2) is defense-in-depth.

### 6b — "OpenRouter compaction errors" — **separate, partially corroborated**

Unrelated to 6a. Two distinct candidate error paths — the error text decides which:

- **Case (a) — fail-closed capability preflight.** `internal/llm/capabilities.go` — `CapabilityFor`→`ValidateForCompaction` (lines 93,100-104) returns `ErrCompactionCapabilityUnavailable` if the capability row is incomplete (`ContextWindow<=0` at 75-76, unknown provider at 90-91, or any missing field). Note the per-provider token-estimation constants are hardcoded (`capabilities.go:108-113`, `1.15`/`256`) and ignore each provider's `Estimator` — so estimation is least reliable exactly on **llama.cpp** (`ProviderTokenizer:true` but the fields stay 0). This whole preflight is dark today (the durable engine is unwired) so it is moot until the compaction SPIKE lands.
- **Case (b) — hard context-overflow at inference.** A `400 "context length exceeded"` when the local lexical trim (`internal/conversations/context.go:400` `dropOldestPairs`, gated by the token estimate) under-counts and sends an over-length request. This is the more likely live cause of "compaction openrouter dà errori."

**Mitigation for case (b) — provider-scoped, NOT a compaction strategy.** OpenRouter offers a provider-side `transforms:["middle-out"]` / context-compression that lossily truncates from the middle when the request exceeds context. Enabling it **on the OpenRouter provider only** is a cheap fail-safe against the hard 400. Hard constraints:
1. **It covers only the OpenRouter path.** Aura also runs **llama.cpp (local, for privacy)**, which has no provider-side net — so primary context management must stay Aura-side and provider-agnostic (fix the token estimator, case (a) note; keep the lexical ladder correct).
2. **It is lossy truncation, not summarization**, and drops messages with no tool-call/tool-result pairing awareness — risky for a tool-heavy agent. Aura's `dropOldestPairs` drops whole rounds (pair-safe), so middle-out is a backstop, never the mechanism.
3. **Privacy:** never route the llama.cpp path's context through OpenRouter to "compress" it. See the live-attack-plan items 1.10 (token estimator) and 1.11 (OpenRouter overflow fail-safe).

---

## BUG-7 — Compact command missing from the UI — **VALID, Medium**

The operator used `/compact` in chat (conversation turn 149). That command is first-class in **Telegram** (`internal/channels/telegram/commands.go:156`, help text line 190: `/compact [history|preview|diff|restore]`) but has **no reachable equivalent in the web cockpit**.

### What exists vs. what's reachable

- **Backend** (`internal/agui/conversations_api.go:63-67`): `POST /api/conversations/{id}/compact`, `/compact/history`, `/compact/preview`, `/compact/diff` **all map to `handleCompactPreview`** — which `s.compact.Preview()` and, per its own doc comment (`conversations_api.go:108`), *"never changes activation."* Only `/compact/restore` (`handleCompactRestore`) mutates (rollback to a caller-declared safe checkpoint).
- **Web data layer** (`web/src/conversations/useCompactions.ts`): wires `useCompactionHistory` (`/compact/history`), `usePreviewCompaction` (`/compact/preview`), `useRestoreCompaction` (`/compact/restore`). **Missing `diff`** (the `/compact/diff` route exists server-side, unused by the UI).
- **Web component** (`web/src/conversations/CompactionHistory.tsx`): a full panel with history list + preview + restore buttons — **but it is never mounted.** Grep for `CompactionHistory` across `web/src` returns only its own file and its test; **no shell/tab/parent renders `<CompactionHistory>`.** The panel is built but dead — there is no way for the operator to reach it in the cockpit.

### Two layers of the gap

1. **Primary (UI wiring)**: `CompactionHistory` is orphaned — not surfaced in the shell (`web/src/shell/`). The chat/Telegram `/compact` has no cockpit counterpart because the component that would provide it is never rendered.
2. **Deeper (no manual "compact now" anywhere)**: `CompactCoordinator.Preview` always sets `preview.Activated = false` (`internal/runner/runner_compact.go:157`); there is **no `Activate`/`Apply` on the coordinator exposed to the API**. Actual activation runs only in the background engine (`runner_compact.go:36 e.Compact(ctx)`, triggers proactive/idle/boundary/overflow — `:53-64`). So even wiring the UI gives preview/restore/diff, **not** a force-compact. Whether a manual "compact now" *should* exist is a design decision — today it doesn't, in any channel.

### Interaction with BUG-6a (compounding)

Manual preview is gated on the rollout config: `Preview` calls `effective.Read` and `snap.Config.Selected(...)`, returning `CompactStatusDisabled` when the scope is not selected (`runner_compact.go:146-155`). **BUG-6a froze the rollout control plane at `disabled`**, so right now the preview path would return `Disabled` for everyone — the compaction feature is effectively dead end-to-end, UI included, until BUG-6a is fixed.

### Fix direction

1. Mount `CompactionHistory` in the cockpit shell (a conversation-scoped panel/tab under `web/src/shell/` alongside the other conversation views), gated on `conversationId`.
2. Add the missing `diff` action to `useCompactions.ts` (route already exists) and a diff view in the panel for parity with the chat `/compact diff`.
3. Decide the "compact now" question: if manual activation is wanted, add an activation path on `CompactCoordinator` + an authenticated `POST .../compact/activate` endpoint (distinct from preview), then a UI button — this is a **new capability + policy decision**, not just wiring. Otherwise document that manual compaction is preview/restore-only by design.
4. Sequence **after BUG-6a** — with the rollout frozen at `disabled`, any compaction UI shipped now shows only `Disabled`.

---

## Memory MCP tool-surface inventory (parity + prune)

Server: vendored `neo4j-agent-memory` at `docker/agent-memory/src/neo4j_agent_memory/mcp/_tools.py`; wired via `internal/mcp/manager/catalog.go:162-178` (namespace `memory__*`). Runtime (`compose.yaml:485,524-533`): `bolt` backend + `--profile extended` ⇒ **17 live tools** (6 core + 11 extended); the 4 Platinum tools are **dormant** on bolt. (The `catalog.go:162` comment "16-tool surface" is **stale — real count is 17**; worth correcting.)

### CUT
- **`graph_query` (raw Cypher) — CUT.** Footgun + near-duplicate of `memory_search`/`memory_get_facts`/`memory_get_entity`/`memory_export_graph`, and already half-dead (hard-errors on `user_identifier`, i.e. every user-scoped turn). No mature agent (Anthropic/Cursor/Devin/Manus) exposes a query language to the model.
- **`memory_start_trace` / `memory_record_step` / `memory_complete_trace` — CUT-candidate (3 tools).** Duplicate Aura's own `internal/reasoningtrace` subsystem; drift risk; 3 of 17 manifest slots for a rarely-needed sub-feature. Cut unless a concrete consumer exists.

### ADD (priority order)
1. **`memory_forget` / delete-by-id (HIGH)** — delete fact/entity/preference/message/relationship by `id` (all readers already return `id`). Enforce `user_identifier` scoping (like the platinum tools); mark **Mutating** so it routes through `ask_user`. Must **return removed ids** so the Aura-side hook can cascade the scheduler-task cancel (BUG-3b).
2. **`memory_update` (HIGH)** — explicit correct/supersede for contradiction handling; today only implicit merge via `add_*`.
3. **`memory_list_by_type` (MEDIUM)** — enumerate facts/preferences/entities without a query (only `list_sessions` exists); needed to answer "what do you remember about me?" and to drive a delete UI.
4. **`memory_get_provenance` on bolt (MEDIUM)** — "where did I learn this?" (dormant Platinum only).
5. **`memory_merge`/dedupe (LOW)**, **expiry enforcement (LOW)** — `add_fact.valid_until` is accepted but inert; add a reaper or `include_expired=false` default.

### Parity lessons (from `D:\tmp\system-prompts-and-models-of-ai-tools`)
- **One memory verb + `action` enum beats N tools, with delete first-class.** Cursor `update_memory` = single tool `action: create|update|delete` (+`existing_knowledge_id`); Devin same shape. Anthropic memory = file-like primitives incl. **delete**. Aura's 17 flat tools omit the delete/update both make central.
- **Bake contradiction guidance into descriptions** (Cursor: "if the user contradicts an existing memory … use action 'delete', not 'update'"). Aura's `add_*` never tells the model what to do on conflict → stale/duplicate memories.
- **Aura already has the right in-repo pattern**: `internal/agent/tools/task.go:15-23` fronts the whole scheduler with **one non-deferred `task` tool + action enum**, explicitly replacing a "587-LOC five-tool god-class." The memory surface should follow the same house pattern (`memory` verb + `action`, plus a few deferred readers) — matching the CLAUDE.md deferred-tool discipline.
- **Read/write/remove symmetry**: Aura has writers + readers but **zero removers and no long-term-store enumerators** — asymmetric. Delete + update + list close it.

---

## Prioritized action plan

| Prio | Item | Effort | Notes |
|------|------|--------|-------|
| **P0** | BUG-6a: idempotent evidence append + tolerate 23505 + empty-window guard | S–M | Live crash; control plane frozen now. `ON CONFLICT` + loop tolerance is the minimal fix. |
| **P1** | BUG-3: add `memory_forget`/delete (+`memory_update`) to the MCP server | M | Confirmed operator need; GDPR relevance. Shape as `memory` action-verb. |
| **P1** | BUG-1B: route pending-approval prompts through `ChannelDeliverer` | M | Gated jobs silently never fire otherwise. |
| **P2** | BUG-2: add Telegram notify route end-to-end + server-side enum validation | S–M | Also fixes silent stdout degrade. |
| **P2** | BUG-4: add a profile-edit builtin backed by `internal/profile.Store`; **correct the false memory record** | M | Not file non-existence — a missing tool. |
| **P2** | BUG-3b: memory↔task provenance edge + cascade cancel hook | M–L | New feature → PRD amendment. |
| **P2** | BUG-7: mount `CompactionHistory` in the cockpit shell + add `diff` action | S–M | Panel is built but orphaned. Sequence **after BUG-6a** (rollout frozen → shows `Disabled`). "Compact now" = separate policy decision. |
| **P3** | Prune MCP surface: cut `graph_query`; evaluate cutting the reasoning-trace trio; fix `catalog.go:162` "16" comment | S | Parity + footgun reduction. |
| **P3** | BUG-6b: get the real error text → case (a) preflight vs case (b) hard 400. For (b), enable OpenRouter-only `middle-out` fail-safe + fix the token estimator (esp. llama.cpp) | S | Separate from 6a. Middle-out is OpenRouter-only + lossy — NOT the compaction strategy; llama.cpp/privacy needs Aura-side handling. |
| **P3** | BUG-5: prompt guidance to prefer `shell_exec` for broad discovery | S | Behavioral. |

**Policy decision required (not a bug)**: BUG-1A (AG-016 force-gate of every `agent_job`) — confirm whether normal-risk confirmed jobs should bypass the second gate before touching `task.go:216-218`.
