# Phase 9: Swarm (Minimal) - Context

**Gathered:** 2026-06-04
**Status:** Ready for planning

<domain>
## Phase Boundary

Deliver CAP-03 — the minimal swarm capability: a single deferred tool `swarm_spawn {goals:[...]}` that blocks, fans the goals out as real `LlmAgent` workers via the **already-shipped** `ParallelAgent` (`internal/agent/workflow/parallel.go`), and returns an ordered array of per-child reports. Plus the phase's live validation surface: the **first real Mount** of the `internal/agent/mcptools` seam (mail + WhatsApp MCP servers, boot-level env-gated) exercised by a natural-prompt live E2E with a dual gate (ground-truth assertions + judge rubric ≥90%).

`internal/swarm/` is currently **EMPTY** (the skeleton stub was deleted in the tabula-rasa rewrite) — this is greenfield, sized far below the PRD's original file targets because the discussion cut everything no industrial system ships (see Decisions).

**Research grounding (3 researcher passes + live self-test on Claude Code + ruflo study):** the convergent industrial shape across Claude Code/Agent SDK, Anthropic's multi-agent research system, OpenAI Agents SDK, Codex CLI, nanobot, ADK is — ONE blocking delegation tool whose result IS the child's summarized output; no sibling messaging; child = fresh context + injected brief; depth capped by tool-exclusion; parallelism = concurrent execution inside the runtime. Codex V2's 7-tool mailbox system and ruflo's persistent coordinator/consensus machinery are the documented over-engineering anti-patterns ("no atomic bombs" — Phase 8 lesson).

**Out of scope (v1, confirmed cuts):** message bus, `swarm_talk`, DM-by-ID, `swarm_join` as a separate tool, tier-mapped models / `tier.go` dispatcher, `SpawnInteractive`/`ResumeChild`/Responder live-channel machinery, nested spawn (flat v1), event-forwarding seam to the parent stream, calendar MCP scenario (→ Phase 16).
</domain>

<decisions>
## Implementation Decisions

### Tool surface (Area A)
- **D-01 Array-of-goals:** ONE deferred tool `swarm_spawn {goals:[...]}` — blocking; internal fan-out wraps the shipped `ParallelAgent`; returns when all children finish. NO changes to the core sequential dispatcher (`llm_agent.go` D-14 untouched). No `swarm_talk`/`swarm_join`/bus (amendment #12 stands; research-unanimous).
- **D-02 Per-child failure isolation:** a real child error becomes an `{id, status:"failed", error}` entry alongside sibling reports — NO sibling cancellation (Anthropic partial-results pattern; ruflo#1872 battle-tested fix). The swarm wrap must adapt around `ParallelAgent`'s first-error-cancels-siblings errgroup semantics for the failure path.
- **D-03 No `tier` param in v1 schema:** schema is `{goals}` only. `AURA_SWARM_MODEL_*` env vars stay documented as no-ops (amendment #12); re-add in v2 SWARM-V2-01.

### Child pause / HITL (Area B)
- **D-04 Pause-as-report:** a child whose `ask_user` fires **terminates**; its report entry = `{child_id, tool_call_id, status:"needs_user_input", question, options}`. The coordinator detects this from the child's shipped `Event{Actions.AwaitingInput}` — no new sentinel handling. The parent LLM decides whether to relay via its OWN `ask_user` (normal durable shipped pause) and re-spawn that goal enriched with the answer. NO parked live children, NO ResumeChild/Responder, NO volatile pendings. Empirically confirmed: Claude Code subagents have no user channel and signal `needs input:` inside their final report.
- **D-05 `proxied_*` as optional `ask_user` args:** `ask_user` Spec gains optional `proxied_from_child_id` + `proxied_tool_call_id` the model MAY fill when relaying a child question (ids are ground-truth from the swarm report). Runner stamps them into `aura.paused_states` (columns shipped in migration 0003). Best-effort, documented as model-discretionary.
- **ROADMAP SC#3 re-spec (amendment):** 5-children multi-pause = 5 `needs_user_input` report entries; resume = re-spawn with answers; cancel = parent doesn't re-spawn; goroutine-leak assertion stays.

### Child prompt & KV discipline (Area C)
- **D-06 Parent base + static worker overlay:** worker `messages[0]` = parent `systemMessage()` + appended static worker section (headless executor, produce a final report, the parent reads only your text). ONE source of truth; byte-stable across ALL workers → DeepSeek implicit cache ~90% hit from worker #2 (cache rewards token-identical repetition, not parent lineage). Empirically what Claude Code does live (same base prompt + background-job overlay).
- **D-07 Goal as structured first USER message:** the Anthropic 4-part brief — objective, output format, tool guidance, task boundaries. NEVER in `messages[0]`. **Supersedes PRD OQ1** ("system prompt parametrizzato dal goal").
- **D-08 Tool inheritance:** full inherit (PRD OQ2) MINUS `swarm_spawn` (D-10 flat). `ask_user` stays (D-04 converts it). `text_response` remains the worker terminal.

### Budget, depth, timeout (Areas H/I/J)
- **D-09 Pre-spawn budget guard + parent reserve:** reject the spawn with a structured model-readable error when `Budget.Remaining() < len(goals) + ~3` (steps reserved for parent synthesis — Codex `reserve_spawn_slot` pattern). Forced-finalization (Phase 7.1) stays as the safety net for mid-swarm exhaustion. Children inherit via the shipped `Budget.Child()` shared `*atomic.Int32` (tree total ≤ parent remaining — already proven in `ParallelAgent` SC#3). Snapshot `Remaining()` once before the fan-out loop for equal sibling shares (budget.go documented requirement).
- **D-10 Flat v1 (no nesting):** workers do NOT get `swarm_spawn` in their registry (total tool-exclusion — Claude Code/nanobot/Codex-V1 line). `AURA_SWARM_MAX_DEPTH=2` env + a code guard retained forward-compat; **ROADMAP SC#2 re-specced** (worker attempting spawn = tool-not-available; the code guard unit-tested with a synthetic depth ≥ cap → PRD error message).
- **D-11 Per-child timeout:** `AURA_SWARM_CHILD_TIMEOUT_SEC` (default ~120) as a per-worker ctx deadline; timeout → `{status:"failed", error:"timeout"}` entry, siblings unaffected. Shared Budget wallclock remains the global ceiling.

### Concurrency & overflow (Area D + M)
- **D-12 Internal waves:** goals beyond `AURA_SWARM_MAX_CONCURRENT` (default 4, operator-tuned per target: 2 on the mini-PC) run in sequential waves within the same call (PRD #34A "accodata"; bounds RAM/FD per D00.6).
- **D-13 Goals cap:** `len(goals) > AURA_SWARM_MAX_GOALS` (default 8, NEW env) → model-readable tool error (Anthropic over-spawn failure mode #1).
- **D-14 Burst accepted:** NO per-tool semaphores in v1 — `MAX_CONCURRENT` is the single cap. sandbox-agent is process-based (no per-session lock — **#34(B) wording superseded**). Add a semaphore only if live E2E shows real contention.

### Report contract & lifecycle & observability (Areas E/F/G/K)
- **D-15 ChildReport array:** ordered by goals index: `{goal_index, child_id, status: ok|failed|needs_user_input, summary (per-child cap ~2-4KB), error?, question?/options?/tool_call_id?}`. PER-CHILD cap only (the goals cap bounds the total); larger content spills via the SHIPPED `tools.NewResult` preview+sidecar+`read_tool_output` — no second spillover mechanism. No custom metrics.
- **D-16 Ephemeral per-call runner:** constructed inside `swarm_spawn.Execute`, builds N LlmAgents + ParallelAgent wrap, drains, collects reports, returns — GC. ZERO cross-call state (no children map / RWMutex / registry — those served Join/Talk/ResumeChild, all cut). Child IDs deterministic `w1..wN` by goal index.
- **D-17 Silent-until-done + slog:** 3 structured slog lines per child (`child.spawned{w_i,goal}` / `child.completed{w_i,status,dur}` / `child.failed{w_i,error}`). Failures surface in the report array, never swallowed. No polling tool / event bus / forwarding seam in v1.
- **D-18 Transcript dump always-on:** per-child Event transcript to `$AURA_RUN_DIR/<conv>/swarm/<w_i>.jsonl` via `Event.MarshalJSON` (Claude Code persists subagent transcripts). GC via the existing run-dir TTL sweep. Best-effort — a dump failure never fails the swarm.

### MCP mounts & live E2E (Areas P/Q/R + swap) — CORRECTED by spikes 001/002 (2026-06-04)
- **D-19 Servers (spike-validated):** `mail` = **martinzarfl/mail-mcp** (Node, stdio, SMTP/IMAP env config — spike 001 VALIDATED: send-to-self + IMAP read-back + bridged Execute; NOTE `search_emails` takes `{query}` required, not a criteria object) + `whatsapp` = **lharries/whatsapp-mcp** (Go whatsmeow bridge in WSL + Python/uv MCP server spawned via `wsl.exe` stdio — spike 002 VALIDATED **with a required bridge patch**: whatsmeow bump to latest (servers 405 old clients) + 5 context.Context call-site fixes + REST-send persistence so agent-sent messages are read-back-able; patch at `.planning/spikes/002-whatsapp-mcp-pairing/bridge-patch.diff`). User's own number/mailbox, **messages to self only**. Self-chat JID duality: bridge-sent rows under `<phone>@s.whatsapp.net`, phone-sent rows under `<lid>@lid` — E2E assertions must target the right JID per direction. Calendar DROPPED → Phase 16 (MarimerLLC/calendar-mcp noted candidate). Both MANDATORY in Gate 3; pre-req DONE for the dev machine (both paired/registered), E2E needs a bridge bring-up + health-check step (REST :8080 alive).
- **D-20 Allowlist at Mount (need spike-confirmed):** `mcptools.Mount` gains an optional per-server tool allowlist. Mail v1: `send_email, fetch_emails, search_emails, get_thread` (spike 001 confirmed the server also ships `delete_mailbox`/`move_message`/`create_mailbox` footguns). WhatsApp v1: `send_message, list_messages, list_chats, search_contacts`. **PLUS (spike finding): `bridge.go:88` currently mounts bridged tools `Deferred: false` — 16 mail + 12 whatsapp non-deferred tools would flood every manifest into the 30-50-tool degradation zone. Phase 9 flips bridged tools to `Deferred: true`** (the 8.1 BM25 `tool_search` then discovers them).
- **D-21 Boot-level mount (SUPERSEDED by existing code, spike-discovered):** boot mounting ALREADY EXISTS — `buildRegistryWithMCP` (`cmd/aura/main.go:104`) mounts every enabled server from the managed registry `~/.aura/mcp/servers.json` (`internal/mcp/managed_config.go`), managed by the shipped `aura mcp {install,add,list,doctor,tools,enable,disable,remove}` CLI (Codex commit `ae11737a`). **No `AURA_MCP_MAIL_SERVER`/`AURA_MCP_WHATSAPP_SERVER` env vars** — the managed config is the registration path (the `AURA_MCP_*_SERVER_JSON` vars remain test-tier overrides used by `internal/mcp/*_integration_test.go`). Phase 9 verifies fail-soft boot behavior (a dead server must not kill `aura chat`) rather than building the wiring. Recipes/doctor labeling stay Phase 16 (note: a basic `aura mcp doctor` already exists).
- **D-22 Dual scoring gate:** live E2E (build tag `cot_eval` pattern, OPENROUTER-gated, operator-run, NOT CI) = deterministic ground-truth assertions as hard floor (N workers spawned via tool_use blocks; expected facts present; WhatsApp/mail message exists on read-back via MCP; timing < 1.5× single-worker) + judge rubric ≥90% average (autonomous parallelization with NO "swarm" in the prompt, sub-answer correctness, aggregation quality, no over-spawn on a simple control prompt). Numbers land in `docs/aura-quality-snapshot.md`.

### Process & quality (Areas L/N/O)
- **D-23 Amendment Wave-0:** plan 09-01 is a doc-only PRD-amendment-gate plan (precedent 05-01/08-01) committed BEFORE any code. It supersedes: Slice 3 acceptance Talk/broadcast items; OQ1 (D-07); OQ5 Responder design (D-04); #34(B) sandbox-session wording (sandbox-agent process model); #34(C) stub note (stub deleted); file targets `bus.go`/`tier.go`/`swarm_talk.go`/`swarm_join.go`; flat nesting (D-10); spawn-depth approval governance note (n/a flat v1). It ADDS to the env catalog: `AURA_SWARM_MAX_GOALS`, `AURA_SWARM_CHILD_TIMEOUT_SEC`, `AURA_MCP_MAIL_SERVER`, `AURA_MCP_WHATSAPP_SERVER`. It re-specs ROADMAP SC#2 + SC#3 (and notes the Gate-3 E2E/MCP addition as SC#5).
- **D-24 Anti-over-spawn load-bearing literal:** the `swarm_spawn` deferred Description includes — use ONLY for ≥2 independent self-contained subtasks; a simple single task = answer directly; each goal is a complete brief (objective + output format + boundaries; the worker cannot see the conversation). A test asserts the key phrases (the `finalizeNudge` pattern).
- **D-25 Property-based (PRD Slice-3 explicit requirement):** rapid properties — for any goals array (1..8) and any per-child outcome mix: report has same length and order as goals; total tree steps ≤ parent remaining at spawn; goleak-clean after return; per-child isolation holds (a failed/timed-out child never affects sibling entries).

### Claude's Discretion
- Exact worker-overlay prompt text and the structured-brief template (constraints in D-06/D-07; load-bearing-literal tests per D-24).
- `errgroup`/wave implementation details inside the ephemeral runner; how the wrap isolates child errors from ParallelAgent's cancel semantics (D-02) — adapt or bypass ParallelAgent's error slot as needed, keeping its leak-safety invariants.
- Judge rubric exact dimension weights and the control-prompt set (gate fixed at ≥90%, dimensions fixed in D-22).
- WhatsApp MCP bridge selection (criteria: whatsmeow-based, stdio, supports read-back of the self-chat).
- `Mount` allowlist signature shape; where the per-child summary cap constant lives (reuse `AURA_CONTEXT_PREVIEW_CAP_BYTES` vs a dedicated knob — prefer reusing the existing knob unless tests show it too small).
- Reserve size in D-09 (~3 steps) — tune to what finalize actually needs.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope (in-repo)
- `.planning/ROADMAP.md` §"Phase 9: Swarm (Minimal)" (lines ~267-285) — goal + 4 success criteria (SC#2/SC#3 re-specced per D-10/D-04 via the D-23 amendment).
- `prd.md` §"Slice 3" (lines 1388-1475) — amendments #12/#34; the acceptance list contains STALE Talk/broadcast items that the D-23 amendment supersedes. Env catalog §"Caps & Limits" lines ~4765-4768 (`AURA_SWARM_*`).
- `.planning/REQUIREMENTS.md` CAP-03 (line 34).

### Shipped code this phase builds on (ground-truth read during scout)
- `internal/agent/workflow/parallel.go` — ParallelAgent (errgroup fan-out + ack backpressure + escalate-cancels-siblings; D-02 must isolate per-child failures around its first-error semantics).
- `internal/agent/budget.go` — `Budget.Child` shared `*atomic.Int32`; snapshot-Remaining-before-fan-out requirement (lines ~270-285); wallclock deadline.
- `internal/agent/llm_agent.go` + `llm_agent_finalize.go` — `LlmAgentConfig`/`NewLlmAgent` (workers are constructed through this), forced-finalization interplay with D-09.
- `internal/agent/llm_agent_pause.go` + `internal/agent/event.go` — `Actions.AwaitingInput` + `OriginAgent` (the signal D-04 converts to a report entry); `Event.MarshalJSON` (D-18 transcripts).
- `internal/agent/tools/spec.go`, `result.go` (`NewResult` spillover, `WithToolCallContext`), `ask_user.go` (D-05 args), `search.go`/`bm25.go` (deferred discovery of bridged tools).
- `internal/agent/mcptools/bridge.go` — `Bridge`/`Mount` + 8.1 namespacing; D-20 allowlist lands here; D-21 wires it into `cmd/aura/chat.go` `bootChat`.
- `internal/db/migrations/0003_paused_states.up.sql` + `internal/askuser/store.go` — `proxied_from_child_id`/`proxied_tool_call_id` columns (D-05 populates via Runner).
- `internal/eval/` (build tag `cot_eval`) — the live-eval harness D-22 extends.

### Industrial references (research evidence)
- `D:/tmp/system_prompts_leaks/Anthropic/claude-code.md` lines ~259-396 — the Agent tool contract (single tool, blocking, parallel via multiple tool_use, depth via tool-exclusion, no child→user channel).
- `D:/tmp/codex/codex-rs/core/src/tools/handlers/multi_agents_v2/` + `agent/registry.rs` — the heavy-outlier anti-pattern; `reserve_spawn_slot` (D-09); goal-as-first-user-turn (`spawn.rs:122-141`).
- `D:/tmp/nanobot/nanobot/agent/subagent.py` + `tools/spawn.py` — minimal single-tool reference; tool-exclusion depth cap; soft NL overflow error.
- `D:/tmp/ruflo` — cautionary maximal reference; steal ONLY: `format=summary` default, artifact-by-path, per-child failure isolation (`SwarmCoordinator.ts:212-222`, ruflo#1872).
- <https://www.anthropic.com/engineering/multi-agent-research-system> — orchestrator-worker, 4-part brief, over-spawn failure mode, "subagents can't coordinate".
- <https://code.claude.com/docs/en/agent-sdk/subagents> — "subagent does NOT receive the parent's system prompt"; "subagents cannot spawn their own subagents".
- <https://openai.github.io/openai-agents-js/guides/human-in-the-loop/> — nested interruptions surface on the outer run (the D-04 semantic precedent).
- <https://github.com/martinzarfl/mail-mcp> — chosen mail MCP (D-19): stdio, IMAP/SMTP env-var config, send-to-self ground truth.
- <https://github.com/MarimerLLC/calendar-mcp> — calendar MCP candidate DEFERRED to Phase 16 (ICS/JSON fixture backend noted).
- Auto-memory `feedback_no_atomic_bombs_minimal_industrial_shape` — the governing lens for this phase.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `ParallelAgent` — the concurrency engine; the swarm runner only wraps it (waves + per-child isolation + report collection).
- `Budget.Child()` — budget inheritance is ALREADY proven (depth-3 fan-3 tree ≤ max_steps total); Phase 9 adds only the D-09 pre-flight guard.
- `NewLlmAgent(LlmAgentConfig)` — workers are plain LlmAgents; `UserTurns` carries the D-07 brief; `SessionID` keys the sidecar dir.
- `tools.NewResult` — the ONLY spillover mechanism for the swarm report (D-15).
- `Event{Actions.AwaitingInput, OriginAgent}` — the D-04 pause signal, shipped and round-trip tested.
- `internal/eval` cot_eval harness — D-22 extends it; `agenttest.FakeClient` for unit/property tiers.
- `mcptools.Bridge/Mount` with 8.1 namespacing — D-19/D-20/D-21 are its first production exercise.

### Established Patterns
- Deferred-tool pattern: `swarm_spawn` is `Deferred=true`; the D-24 description is its searchable document (BM25 indexes name+description+arg fields post-8.1).
- `messages[0]` byte-stable invariant (CAP-04 validated) — D-06/D-07 designed around it; never parametrize the system prompt per goal.
- Pause = Event-only, never the iter error slot; Runner is the sole `paused_states` writer; resume = fresh Run over rehydrated history — D-04 composes with this, adds nothing to it.
- Load-bearing literal + asserting test (`finalizeNudge`) — reuse for D-24.
- Doc-only PRD-amendment Wave-0 plan (05-01/08-01 precedent) — D-23.
- No-skip-as-green: the cot_eval tier is operator-run (not CI) by design, like the existing live tiers; CI gates stay on unit/property/race/goleak.

### Integration Points
- `cmd/aura/chat.go` `bootChat` — registers `swarm_spawn` (parent registry only) + D-21 MCP mounts.
- `cmd/aura/main.go` — `aura swarm-demo` subcommand (mock-LLM fixture, `aura agent dry-run` 02-07 pattern).
- Worker registry construction — a copy of the parent registry minus `swarm_spawn` (registries are immutable per run; workers get their own instance).
- `$AURA_RUN_DIR/<conv>/swarm/` — D-18 transcript dumps, covered by the existing TTL sweep.
</code_context>

<specifics>
## Specific Ideas

- "Panoramica completa senza bombe atomiche" — the user explicitly invoked the Phase 8 lesson (bespoke design rewritten twice, then replaced off-the-shelf). Every D-decision above traces to a named industrial precedent; every cut traces to "no surveyed system builds it". The planner must NOT re-introduce cut machinery.
- The live E2E must use **natural prompts with no mention of "swarm"** (PRD §Test discipline) — the model chooses to parallelize on its own; the judge scores that choice.
- WhatsApp/mail E2E sends go ONLY to the user's own account/number; ground truth = read-back via the same MCP server.
- The user wants the MCP mounts to be daily-usable (`aura chat`), not test-only — D-21 reflects that.
</specifics>

<deferred>
## Deferred Ideas

- **swarm_talk / inter-agent bus / DM-by-ID** → v2 SWARM-V2-01 (research: strongest cut — keep cut).
- **Tier-mapped models** → v2 SWARM-V2-01.
- **spawn/join async pair** — only if interleaved parent work is ever needed.
- **Nested spawn (1-level)** — `AURA_SWARM_MAX_DEPTH` env + code guard retained; enable post-v1 if a real need appears.
- **Event-forwarding seam child→parent stream** — for the AG-UI Phase 12 consumer; `{childId, phase}` progress shape noted as the minimal addition for future async/background swarms.
- **Calendar MCP scenario** → Phase 16 (MarimerLLC/calendar-mcp candidate; ICS/JSON fixture backend for deterministic tests); recipes/doctor/risky-tool labeling for mail/WhatsApp also Phase 16.
- **Per-tool semaphores under fan-out** — only if live E2E shows real contention (D-14).
- **Hybrid per-child summarizer (LLM-compressed reports)** — v1 truncates; revisit if reports prove low-signal.

### Reviewed Todos (not folded)
None — `.planning/todos/pending/` is empty (todo.match-phase returned 0).
</deferred>

---

*Phase: 09-swarm-minimal*
*Context gathered: 2026-06-04*
