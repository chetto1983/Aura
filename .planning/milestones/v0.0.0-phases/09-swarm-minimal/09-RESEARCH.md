# Phase 9: Swarm (Minimal) - Research

**Researched:** 2026-06-04
**Domain:** Go agent-runtime composition (swarm coordinator over shipped ParallelAgent) + first production MCP mount (mail + WhatsApp) + live dual-gate E2E
**Confidence:** HIGH (all claims are codebase ground-truth, file:line cited; this is an implementation-research pass, not a domain survey)

## Summary

This is **implementation research**: the domain (industrial swarm shape) was settled across 3 researcher passes + 2 live spikes and is frozen in CONTEXT D-01..D-25. My job was to read the shipped code the planner will build tasks against and report exactly what exists, what differs from CONTEXT's line references, and what concretely must change. The phase is genuinely small: `internal/swarm/` is **empty (verified — directory exists, zero files)**, the concurrency engine (`ParallelAgent`), budget tree (`Budget.Child`/`Remaining`), pause Event (`Actions.AwaitingInput`), spillover (`tools.NewResult`), deferred-discovery (`tool_search`/`bm25`), and MCP bridge (`mcptools.Mount`) are all shipped and tested. Phase 9 is a thin wrapper + 5 small surgical edits to existing files + a doc-amendment Wave-0 + a live E2E.

**Primary recommendation:** Build an **ephemeral per-call swarm runner** inside `swarm_spawn.Execute` that **bypasses** ParallelAgent's escalate/first-error-cancels-siblings semantics (D-02 needs the OPPOSITE: no sibling cancellation) by running each child `LlmAgent.Run` in its own `errgroup`-managed goroutine with a per-child `recover`-and-collect wrapper, while **preserving ParallelAgent's documented leak-safety invariants** (multi-arm selects, `defer cancel()`, spawn-loop `egCtx.Err()` guard). The five surgical edits: (1) flip `mcptools` bridged tools to `Deferred: true`; (2) add an allowlist param to `Mount`; (3) make `buildRegistryWithMCP` fail-soft (per-server WARN-and-drop); (4) wire `proxied_*` through `askuser.InsertParams` + `ask_user` Spec + Runner; (5) add `mail`/`whatsapp` recipes to `mcpRecipes`.

**Three CONTEXT claims the code refines (flag — do NOT silently treat as written):**
1. **CONTEXT D-21 / lifecycle-study cite `main.go:122-124` for the abort-on-failure.** The actual abort is `main.go:121-128` (`MountServer` error → `closeMCPServers` + `return nil,nil,err`); `bootChat` exit is `chat.go:139-144`. Close enough but the planner should cite the verified lines.
2. **CONTEXT says `bridge.go:88` Deferred:false must flip.** Verified: `bridge.go:88` literally is `Deferred: false`. ✓ Accurate. But note the flip must ALSO update the doc-comment at `bridge.go:62-68` (which currently *justifies* non-deferred) and the spike-validated assertion that namespaced names render in the manifest.
3. **CONTEXT D-05 says columns "already exist" and `proxied_*` "flow Runner→store".** Half-true: the **SQL columns + sqlc params exist** (`InsertPausedStateParams.ProxiedFromChildID/ProxiedToolCallID`, `paused_states.sql.go:88-89`), BUT the **domain layer drops them** — `askuser.InsertParams` (`store.go:86-95`) has NO proxied fields, `Store.Insert` (`store.go:108-117`) never sets them, and `persistPause` (`runner_persist.go:140-148`) never passes them. D-05 is therefore NOT "columns already wired" — it is "wire the columns through 3 untouched layers." This is real work, not a trivial populate.

## User Constraints (from CONTEXT.md)

### Locked Decisions
Copied verbatim from CONTEXT.md `## Implementation Decisions`. **The planner MUST honor every D-row; research did not explore alternatives to these.**

**Tool surface (Area A)**
- **D-01 Array-of-goals:** ONE deferred tool `swarm_spawn {goals:[...]}` — blocking; internal fan-out wraps the shipped `ParallelAgent`; returns when all children finish. NO changes to the core sequential dispatcher (`llm_agent.go` D-14 untouched). No `swarm_talk`/`swarm_join`/bus.
- **D-02 Per-child failure isolation:** a real child error becomes an `{id, status:"failed", error}` entry alongside sibling reports — NO sibling cancellation. The swarm wrap must adapt around `ParallelAgent`'s first-error-cancels-siblings errgroup semantics for the failure path.
- **D-03 No `tier` param in v1 schema:** schema is `{goals}` only. `AURA_SWARM_MODEL_*` env vars stay documented as no-ops; re-add in v2 SWARM-V2-01.

**Child pause / HITL (Area B)**
- **D-04 Pause-as-report:** a child whose `ask_user` fires **terminates**; its report entry = `{child_id, tool_call_id, status:"needs_user_input", question, options}`. The coordinator detects this from the child's shipped `Event{Actions.AwaitingInput}` — no new sentinel handling. The parent LLM decides whether to relay via its OWN `ask_user` and re-spawn that goal enriched with the answer. NO parked live children, NO ResumeChild/Responder, NO volatile pendings.
- **D-05 `proxied_*` as optional `ask_user` args:** `ask_user` Spec gains optional `proxied_from_child_id` + `proxied_tool_call_id` the model MAY fill when relaying a child question. Runner stamps them into `aura.paused_states`. Best-effort, documented as model-discretionary.
- **ROADMAP SC#3 re-spec:** 5-children multi-pause = 5 `needs_user_input` report entries; resume = re-spawn with answers; cancel = parent doesn't re-spawn; goroutine-leak assertion stays.

**Child prompt & KV discipline (Area C)**
- **D-06 Parent base + static worker overlay:** worker `messages[0]` = parent `systemMessage()` + appended static worker section. ONE source of truth; byte-stable across ALL workers → DeepSeek implicit cache ~90% hit from worker #2.
- **D-07 Goal as structured first USER message:** the Anthropic 4-part brief — objective, output format, tool guidance, task boundaries. NEVER in `messages[0]`. Supersedes PRD OQ1.
- **D-08 Tool inheritance:** full inherit MINUS `swarm_spawn` (D-10 flat). `ask_user` stays (D-04 converts it). `text_response` remains the worker terminal.

**Budget, depth, timeout (Areas H/I/J)**
- **D-09 Pre-spawn budget guard + parent reserve:** reject the spawn with a structured model-readable error when `Budget.Remaining() < len(goals) + ~3` (steps reserved for parent synthesis). Forced-finalization (Phase 7.1) stays as the safety net. Children inherit via the shipped `Budget.Child()` shared `*atomic.Int32`. Snapshot `Remaining()` once before the fan-out loop for equal sibling shares.
- **D-10 Flat v1 (no nesting):** workers do NOT get `swarm_spawn` in their registry. `AURA_SWARM_MAX_DEPTH=2` env + a code guard retained forward-compat; ROADMAP SC#2 re-specced (worker attempting spawn = tool-not-available; the code guard unit-tested with a synthetic depth ≥ cap → PRD error message).
- **D-11 Per-child timeout:** `AURA_SWARM_CHILD_TIMEOUT_SEC` (default ~120) as a per-worker ctx deadline; timeout → `{status:"failed", error:"timeout"}` entry, siblings unaffected. Shared Budget wallclock remains the global ceiling.

**Concurrency & overflow (Area D + M)**
- **D-12 Internal waves:** goals beyond `AURA_SWARM_MAX_CONCURRENT` (default 4, operator-tuned: 2 on the mini-PC) run in sequential waves within the same call.
- **D-13 Goals cap:** `len(goals) > AURA_SWARM_MAX_GOALS` (default 8, NEW env) → model-readable tool error.
- **D-14 Burst accepted:** NO per-tool semaphores in v1 — `MAX_CONCURRENT` is the single cap. Add a semaphore only if live E2E shows real contention.

**Report contract & lifecycle & observability (Areas E/F/G/K)**
- **D-15 ChildReport array:** ordered by goals index: `{goal_index, child_id, status: ok|failed|needs_user_input, summary (per-child cap ~2-4KB), error?, question?/options?/tool_call_id?}`. PER-CHILD cap only; larger content spills via the SHIPPED `tools.NewResult` preview+sidecar+`read_tool_output`. No custom metrics.
- **D-16 Ephemeral per-call runner:** constructed inside `swarm_spawn.Execute`, builds N LlmAgents + ParallelAgent wrap, drains, collects reports, returns — GC. ZERO cross-call state. Child IDs deterministic `w1..wN` by goal index.
- **D-17 Silent-until-done + slog:** 3 structured slog lines per child (`child.spawned` / `child.completed` / `child.failed`). Failures surface in the report array, never swallowed. No polling tool / event bus / forwarding seam in v1.
- **D-18 Transcript dump always-on:** per-child Event transcript to `$AURA_RUN_DIR/<conv>/swarm/<w_i>.jsonl` via `Event.MarshalJSON`. GC via the existing run-dir TTL sweep. Best-effort.

**MCP mounts & live E2E (Areas P/Q/R + swap) — CORRECTED by spikes 001/002**
- **D-19 Servers:** `mail` = **martinzarfl/mail-mcp** (spike 001 VALIDATED; `search_emails` takes `{query}` required) + `whatsapp` = **lharries/whatsapp-mcp** (spike 002 VALIDATED with required bridge patch, maintained at fork `chetto1983/whatsapp-mcp@6de1dcd`). Self only. Self-chat JID duality. Calendar DROPPED → Phase 16. Both MANDATORY in Gate 3; E2E needs bridge bring-up + REST :8080 health-check.
- **D-20 Allowlist at Mount:** `mcptools.Mount` gains an optional per-server tool allowlist. Mail v1: `send_email, fetch_emails, search_emails, get_thread`. WhatsApp v1: `send_message, list_messages, list_chats, search_contacts`. PLUS flip bridged tools to `Deferred: true`.
- **D-21 Boot-level mount (SUPERSEDED by existing code):** boot mounting ALREADY EXISTS. No `AURA_MCP_MAIL_SERVER`/`AURA_MCP_WHATSAPP_SERVER` env vars — managed config is the registration path. Phase 9 makes `buildRegistryWithMCP` fail-soft (per-server WARN-and-drop). Optional: lazy reconnect-on-use. EXPLICIT non-goals: ping ticker, restart supervisor, lazy mount, all OpenClaw plugin-host machinery. Bridge daemon: preferred = compose service with healthcheck; alternative weighed = single-binary daemon-free fork.
- **D-22 Dual scoring gate:** live E2E (`cot_eval` pattern, OPENROUTER-gated, operator-run, NOT CI) = deterministic ground-truth assertions (N workers via tool_use blocks; expected facts; WhatsApp/mail read-back; timing < 1.5× single-worker) + judge rubric ≥90% (autonomous parallelization with NO "swarm" in prompt, sub-answer correctness, aggregation quality, no over-spawn on a simple control). Numbers land in `docs/aura-quality-snapshot.md`.

**Process & quality (Areas L/N/O)**
- **D-23 Amendment Wave-0:** plan 09-01 is a doc-only PRD-amendment-gate plan committed BEFORE any code. Supersedes Slice-3 Talk/broadcast acceptance, OQ1, OQ5 Responder, #34(B/C), file targets `bus.go`/`tier.go`/`swarm_talk.go`/`swarm_join.go`, flat nesting. Adds env catalog: `AURA_SWARM_MAX_GOALS`, `AURA_SWARM_CHILD_TIMEOUT_SEC`, `AURA_MCP_MAIL_SERVER`, `AURA_MCP_WHATSAPP_SERVER`. Re-specs SC#2 + SC#3 (+ Gate-3 E2E/MCP as SC#5). **[FLAG: the D-23 env-add list itself contains a contradiction — see Open Question #1.]**
- **D-24 Anti-over-spawn load-bearing literal:** the `swarm_spawn` Description includes the ≥2-independent-subtasks / each-goal-a-complete-brief / worker-cannot-see-conversation phrasing; a test asserts the key phrases (the `finalizeNudge` pattern).
- **D-25 Property-based:** rapid properties — report length+order = goals; total tree steps ≤ parent remaining at spawn; goleak-clean after return; per-child isolation holds.

### Claude's Discretion
- Exact worker-overlay prompt text and the structured-brief template (constraints in D-06/D-07; load-bearing-literal tests per D-24).
- `errgroup`/wave implementation details inside the ephemeral runner; how the wrap isolates child errors from ParallelAgent's cancel semantics (D-02) — adapt or bypass ParallelAgent's error slot as needed, keeping its leak-safety invariants.
- Judge rubric exact dimension weights and the control-prompt set (gate fixed at ≥90%, dimensions fixed in D-22).
- WhatsApp MCP bridge selection (criteria: whatsmeow-based, stdio, supports read-back of the self-chat).
- `Mount` allowlist signature shape; where the per-child summary cap constant lives (reuse `AURA_CONTEXT_PREVIEW_CAP_BYTES` vs a dedicated knob — prefer reusing the existing knob unless tests show it too small).
- Reserve size in D-09 (~3 steps) — tune to what finalize actually needs.

### Deferred Ideas (OUT OF SCOPE)
- swarm_talk / inter-agent bus / DM-by-ID → v2 SWARM-V2-01.
- Tier-mapped models → v2 SWARM-V2-01.
- spawn/join async pair — only if interleaved parent work is ever needed.
- Nested spawn (1-level) — `AURA_SWARM_MAX_DEPTH` env + code guard retained; enable post-v1.
- Event-forwarding seam child→parent stream — for the AG-UI Phase 12 consumer.
- Calendar MCP scenario → Phase 16 (recipes/doctor/risky-tool labeling for mail/WhatsApp also Phase 16).
- Per-tool semaphores under fan-out — only if live E2E shows real contention.
- Hybrid per-child summarizer (LLM-compressed reports) — v1 truncates.

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| CAP-03 | Swarm coordinator minimale: riusa `ParallelAgent` da Slice 0.9 + cap `MAX_SPAWN_DEPTH=2` per v1. NO DM-by-ID, NO tier-mapped models in v1. Child budget inheritance dal parent's remaining. (`REQUIREMENTS.md:34`) | `ParallelAgent` shipped (`parallel.go`); `Budget.Child` shared atomic shipped (`budget.go:285-299`); depth guard via tool-exclusion (D-10) + `AURA_SWARM_MAX_DEPTH` env (PRD env catalog line 4765); worker construction via `NewLlmAgent` (`llm_agent.go:78`). All building blocks verified present. |

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Fan-out concurrency (run N children in parallel) | Agent runtime (`internal/swarm` wrapping `internal/agent/workflow.ParallelAgent`) | — | The concurrency engine is shipped; swarm only wraps it (waves + isolation + collection). |
| Per-child worker loop | Agent runtime (`internal/agent.LlmAgent`) | — | Workers are plain `LlmAgent`s; no new agent type. |
| Budget bounding (tree total ≤ parent remaining) | Agent runtime (`internal/agent.Budget`) | — | `Budget.Child` shares the `*atomic.Int32` by pointer — already proven (`budget.go:285`). |
| Tool registration / allowlist / deferral | Tool plumbing (`internal/agent/tools`, `internal/agent/mcptools`) | — | `swarm_spawn` is a deferred tool; MCP allowlist lands in `mcptools.Mount`. |
| Pause persistence + proxied-id stamping | Persistence (`internal/askuser` + `aura.paused_states`) | Orchestration (`internal/runner`) | The Runner is the SOLE `paused_states` writer (`runner_persist.go:120` doc); askuser.Store owns the row shape. |
| MCP sidecar lifecycle (boot mount, fail-soft) | CLI composition root (`cmd/aura/main.go` + `chat.go`) | Transport (`internal/mcp`, `internal/agent/mcptools`) | Boot mounting already exists in `buildRegistryWithMCP`; Phase 9 only changes its failure policy. |
| Live E2E ground truth (mail/WhatsApp read-back) | Eval harness (`internal/eval`, tag `cot_eval`) | External MCP servers (mail-mcp, whatsapp bridge) | Operator-run paid gate, not CI — mirrors the shipped CoT harness. |

## Standard Stack

This phase introduces **no new Go dependencies**. Everything is in-tree. The "stack" is the shipped Aura packages the swarm composes.

### Core (shipped, build on these)
| Package / Symbol | Location | Purpose | Why standard here |
|------------------|----------|---------|-------------------|
| `workflow.ParallelAgent` / `NewParallel` | `internal/agent/workflow/parallel.go:43,51` | errgroup fan-out + ack backpressure + escalate-cancels-siblings | The shipped concurrency engine (D-01/D-02). |
| `agent.Budget` / `.Child` / `.Remaining` / `.ConsumeStep` | `internal/agent/budget.go:285,238,225` | shared `*atomic.Int32` tree budget + snapshot-before-fan-out | D-09 pre-flight + child inheritance. |
| `agent.LlmAgent` / `NewLlmAgent` / `LlmAgentConfig` | `internal/agent/llm_agent.go:78,63` | the worker loop | D-06/07/08 worker construction. |
| `agent.Event{Actions.AwaitingInput,OriginAgent}` | `internal/agent/event.go:67,81` | the D-04 pause signal | shipped + round-trip tested. |
| `tools.NewResult` / `WithToolCallContext` | `internal/agent/tools/result.go:94,26` | preview+sidecar spillover | D-15 the ONLY spillover mechanism. |
| `tools.Spec{Deferred}` / `Registry` | `internal/agent/tools/spec.go:31,63` | deferred-tool pattern + immutable registry | `swarm_spawn` is deferred; worker registry = parent minus `swarm_spawn`. |
| `tools.ToolSearch` + `bm25` | `internal/agent/tools/search.go`, `bm25.go` | BM25 discovery of deferred tools (incl. arg fields) | D-24 — discovers `swarm_spawn` + the now-deferred MCP tools. |
| `mcptools.Mount` / `MountServer` / `Bridge` | `internal/agent/mcptools/mount.go:16`, `bridge.go:69,102` | mount an MCP server's tools into the registry | D-19/D-20/D-21 first production exercise. |
| `mcp.ManagedConfig` / `EnabledServers` | `internal/mcp/managed_config.go:15,92` | `~/.aura/mcp/servers.json` registry | D-21 the registration path (NO new boot env vars). |
| `askuser.Store` + `aura.paused_states` | `internal/askuser/store.go`, `migrations/0003_paused_states.up.sql` | pause persistence | D-05 proxied-id wiring. |
| `eval` cot_eval harness | `internal/eval/harness_cot_eval_test.go` etc. | live paid eval, OPENROUTER-gated | D-22 extends it. |
| `agenttest.FakeClient` / mocks | `internal/agent/agenttest` (used by `eval`, `runner`) | unit/property fixtures | D-25 property tests + unit tier. |

### Supporting
| Symbol | Location | When to use |
|--------|----------|-------------|
| `golang.org/x/sync/errgroup` | already imported (`parallel.go:35`) | the swarm runner's wave goroutine management (Claude's discretion). |
| `log/slog` | stdlib | D-17 structured child lifecycle lines. |
| `pgci.../rapid` (`pgregory.net/rapid`) | already a direct dep (Phase 2, `02-01-PLAN`) | D-25 property tests. |
| `go.uber.org/goleak` | already wired in `internal/agent/workflow` TestMain | D-25 goleak-clean assertion. |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Wrapping `ParallelAgent` directly with its escalate semantics | A bespoke errgroup in the swarm runner that DOES NOT cancel siblings | **RECOMMENDED — see Pattern 1.** D-02 explicitly wants partial-results (no cancel); ParallelAgent's `cancel()`-on-escalate + first-error-cancels-siblings is the OPPOSITE behavior. Adapting it (suppressing escalate, swallowing the error channel) fights the design; a fresh per-child collector that reuses only `Budget.Child` + the leak-safety idioms is cleaner. Keep ParallelAgent for the timing/leak invariants you copy, not the instance. |
| `AURA_SWARM_*_SERVER` boot env vars (PRD original) | managed config `~/.aura/mcp/servers.json` + `mcp` recipes | D-21 — the managed path already exists; new env vars would be dead weight. |
| A new per-child summary cap env knob | reuse `AURA_CONTEXT_PREVIEW_CAP_BYTES` (`config.ToolPreviewCap`, default 2048) | D-15/Discretion — prefer reuse; 2048 may be tight for a 2-4KB summary, measure in E2E. |

**Installation:** none. `go build ./...` after the edits.

**Version verification:** N/A — no registry packages added. The whatsmeow bridge dependency bump (go-sqlite3 1.14.44, whatsmeow `20260603132417-6a7ac9915382`) lives in the **external fork** (`chetto1983/whatsapp-mcp@6de1dcd`), NOT in Aura's `go.mod` — Aura spawns the bridge as a subprocess via managed config, it does not import it.

## Package Legitimacy Audit

> No packages are installed into Aura's `go.mod` this phase. The external MCP servers run as out-of-process subprocesses registered in `~/.aura/mcp/servers.json`. They are still listed here because they execute on the operator's machine and were validated live in spikes 001/002.

| "Package" | Registry | Age | Source Repo | slopcheck | Disposition |
|-----------|----------|-----|-------------|-----------|-------------|
| martinzarfl/mail-mcp (Node, stdio MCP) | npm/git (not Aura's go.mod) | live-validated 2026-06-04 (spike 001) | github.com/martinzarfl/mail-mcp | n/a (out-of-process) | Approved — spike 001 VALIDATED end-to-end (16 tools, IMAP read-back) |
| lharries/whatsapp-mcp + chetto1983 fork | git (not Aura's go.mod) | fork commit `6de1dcd`, 2026-06-03 | github.com/chetto1983/whatsapp-mcp | n/a (out-of-process) | Approved — spike 002 VALIDATED with bridge patch; canonical source is the user's own fork |

**Packages removed due to slopcheck [SLOP] verdict:** none.
**Packages flagged as suspicious [SUS]:** none. slopcheck does not apply — these are operator-installed MCP subprocesses, not registry deps pulled by `go get`. The whatsapp fork is the **user's own** repo (chetto1983), mirroring the existing `recipe:calculator → chetto1983/calculator-mcp-server` trust pattern already in `mcpRecipes` (`cmd/aura/mcp.go:24-40`).

**Footgun census (spike 001 — informs D-20 allowlist):** mail-mcp advertises 16 tools including destructive `delete_mailbox`, `move_message`, `create_mailbox`. An unfiltered mount hands these to swarm workers — D-20 allowlist (`send_email, fetch_emails, search_emails, get_thread`) is mandatory.

## Architecture Patterns

### System Architecture Diagram

```
aura chat (parent LlmAgent loop)
        │  model emits tool_use: swarm_spawn{goals:[g1,g2,g3]}
        ▼
swarm_spawn.Execute(ctx, args)                    ← deferred tool, Deferred:true
        │
        ├─ D-09 pre-flight: Budget.Remaining() < len(goals)+reserve? ──► structured error string (model self-corrects)
        ├─ D-13: len(goals) > AURA_SWARM_MAX_GOALS? ─────────────────► structured error string
        │
        ▼  ephemeral runner (D-16, GC'd at return)
   snapshot rem := Budget.Remaining()             ← ONCE before loop (D-09 / budget.go:277-284)
        │
        ▼  waves of ≤ AURA_SWARM_MAX_CONCURRENT (D-12)
   ┌──────────────────────── per wave ───────────────────────────┐
   │ for each goal gᵢ in wave:                                    │
   │   childBudget := parentBudget.Child(len(wave))   ← shared    │
   │                                                    *atomic    │
   │   worker := NewLlmAgent(LlmAgentConfig{                       │
   │       Registry:  parentRegistry MINUS swarm_spawn (D-08/D-10)│
   │       UserTurns: [structured 4-part brief]  (D-07)           │
   │       SessionID: <conv>/swarm/wᵢ            (D-18 sidecar)   │
   │   })   // messages[0] = parent systemMessage()+overlay (D-06)│
   │   go: per-child ctx (AURA_SWARM_CHILD_TIMEOUT_SEC, D-11)     │
   │       for ev,err := range worker.Run(childIC) {              │
   │         if err != nil    → report{failed, error}  (D-02)     │
   │         if ev.AwaitingInput → report{needs_user_input} (D-04)│
   │         if final Event   → report{ok, summary}              │
   │         dump ev → $RUN_DIR/<conv>/swarm/wᵢ.jsonl (D-18)      │
   │       }                                                       │
   └──────────────────────────────────────────────────────────────┘
        │  collect reports ordered by goal_index (D-15)
        ▼
   tools.NewResult(ctx, json(reports))   ← per-child summary cap; >cap spills to sidecar (D-15)
        │
        ▼  RoleTool result back into parent history → parent LLM aggregates → text_response
```

Boot path (orthogonal, D-21):
```
aura chat boot → bootChat → buildRegistryWithMCP
   ├─ EnabledServers() from ~/.aura/mcp/servers.json
   └─ for each: MountServer (mcp.Open → mcptools.Mount[+allowlist, Deferred:true])
        TODAY:  any error → closeMCPServers + return err → os.Exit(1)   ← THE BUG
        PHASE9: any error → slog.Warn + drop that server, continue      ← FAIL-SOFT (D-21)
```

### Recommended Project Structure
```
internal/swarm/                    # currently EMPTY — greenfield
├── swarm.go            # ephemeral runner: fan-out + waves + per-child collect (the engine; ≤~250 LOC, refactor-on-touch keeps <600)
├── report.go           # ChildReport struct + ordered collection + JSON marshal + per-child cap (D-15)
├── brief.go            # D-07 structured 4-part brief builder + D-06 worker overlay constant (load-bearing literals)
├── swarm_test.go       # unit + race+goleak (SC#1, SC#3, SC#4)
└── swarm_property_test.go  # rapid properties (D-25)
internal/agent/tools/swarm_spawn.go   # Deferred tool: schema {goals}, D-24 description literal, calls internal/swarm
# Surgical edits (NOT new files):
internal/agent/mcptools/bridge.go     # bridge.go:88 Deferred:false→true; allowlist param threaded
internal/agent/mcptools/mount.go      # Mount/MountServer gain allowlist arg
cmd/aura/main.go                      # buildRegistryWithMCP fail-soft (per-server WARN-and-drop)
cmd/aura/mcp.go                       # mcpRecipes += mail, whatsapp (point at forks)
internal/agent/tools/ask_user.go      # Spec += optional proxied_from_child_id/proxied_tool_call_id
internal/askuser/store.go             # InsertParams += proxied fields; Store.Insert sets them
internal/runner/runner_persist.go     # persistPause reads proxied_* off the Event, passes to Insert
internal/eval/                        # swarm E2E scenario + judge rubric (cot_eval tag)
cmd/aura/main.go                      # OPTIONAL aura swarm-demo subcommand (agent dry-run precedent)
```

### Pattern 1: Bypass ParallelAgent's cancel semantics; reuse its leak idioms (D-02, Discretion)
**What:** ParallelAgent (`parallel.go`) is built to **cancel siblings** on the first real error (`errgroup.WithContext`, `parallel.go:77`) AND on any child Escalate (`parallel.go:123-124` captured `cancel()`). D-02 needs the OPPOSITE: a failed child becomes a report entry, siblings keep running.

**Recommendation:** Do NOT instantiate `ParallelAgent` and try to defang it. Build a small per-child collector in `internal/swarm` that:
1. Forks each child's budget with `parentBudget.Child(len(wave))` (`budget.go:285`) — preserves the shared-atomic tree bound (SC#4).
2. Runs each `worker.Run(childIC)` in its own goroutine under a plain `errgroup` (or `sync.WaitGroup`) where a child error is **captured into that child's report slot, never returned to the group** (so no `egCtx` cancellation fires).
3. **Copies ParallelAgent's leak-safety invariants verbatim** (these are the part you MUST preserve — `parallel.go:14-22` documents them): per-child `defer cancel()` on the timeout ctx; a `case <-ctx.Done()` / `case <-done` exit on every blocking op; the spawn-loop `if egCtx.Err() != nil` guard (Go #61611, `parallel.go:96`); the iterator-never-yields-from-a-spawned-goroutine rule (your collector channel is drained by the calling goroutine).

**Why not adapt:** ParallelAgent yields a single serial Event stream upward (it IS an Agent). The swarm tool does NOT yield Events upward — it collects N reports and returns ONE ToolResult. The shapes don't match; wrapping the Agent and re-deriving N reports from one interleaved Event stream (with branch-tag demux) is strictly more code and more fragile than N independent collectors. **CONTEXT explicitly grants this in Discretion: "adapt or bypass ParallelAgent's error slot as needed, keeping its leak-safety invariants."**

**Example (the leak-safety idioms to copy):**
```go
// Source: internal/agent/workflow/parallel.go:95-101,117,137-165 (verbatim invariants)
eg, egCtx := errgroup.WithContext(ctx)
egCtx, cancel := context.WithCancel(egCtx); defer cancel()   // parallel.go:79-80
for i, sub := range wave {
    childBudget := parentBudget.Child(len(wave))             // parallel.go:93 / budget.go:285
    eg.Go(func() error {
        if egCtx.Err() != nil { return nil }                 // parallel.go:96 — #61611 guard
        childCtx, ccancel := context.WithTimeout(egCtx, childTimeout) // D-11
        defer ccancel()
        // collect into reports[i]; a worker error sets reports[i].Status="failed"
        // and returns NIL so siblings are never cancelled (D-02 divergence)
        return nil
    })
}
_ = eg.Wait()
```

### Pattern 2: Worker construction = parent registry minus swarm_spawn (D-06/07/08/10)
**What:** Each worker is `NewLlmAgent(LlmAgentConfig{...})` (`llm_agent.go:78`). Key fields:
- `Registry` — **a copy of the parent registry with `swarm_spawn` removed** (D-10 flat). NOTE: `tools.Registry` (`spec.go:63`) has no `Remove`/`Clone` method today — the planner needs a helper that builds a fresh `NewRegistry()` and re-registers all parent tools except `swarm_spawn`. `Registry.All()` (`spec.go:80`) gives the source set. **This is a small new helper, flag it.**
- `UserTurns []llm.Message` — the D-07 4-part brief as a single RoleUser message (`llm_agent.go:79-81` appends `UserTurns` after `messages[0]`).
- `messages[0]` is `systemMessage()` = the package-constant `SystemPrompt` (`prompt.go:14-25`) — **NOT parametrizable** (it's a hardcoded constant). D-06's "parent base + static worker overlay" therefore can't be done by mutating `SystemPrompt`. Options the planner must pick: (a) add a worker-overlay constant appended to `systemMessage()` only for workers via a new `LlmAgentConfig.SystemOverlay` field, or (b) keep `messages[0]` identical to the parent's and put the worker-role framing in the D-07 first user message. **(b) is simpler and fully satisfies D-06's byte-stability goal (identical messages[0] across workers → cache hit); the "static worker overlay" can ride in messages[1]/UserTurns.** Flag this as a design choice for the planner — `NewLlmAgent` currently has no system-overlay hook (`llm_agent.go:80`).
- `SessionID` — keys the sidecar dir (`llm_agent.go:99`, used by `WithToolCallContext`). Set to `<conv>/swarm/wᵢ` so child tool spillover and the D-18 transcript co-locate. **Caveat:** `validateID` (`result.go:45`) rejects `/` in session_id — so the sidecar dir convention `<conv>/swarm/wᵢ` cannot be a single SessionID string; use a flat session id like `<conv>-swarm-wᵢ` OR a distinct RunDir-relative path. **Flag — D-18's `<conv>/swarm/<w_i>.jsonl` path is a SEPARATE transcript file the swarm writes directly, not the tool-spillover sidecar; don't conflate them.**

### Pattern 3: Pause-as-report, not pause-as-suspend (D-04)
**What:** A worker that calls `ask_user` emits `Event{Actions.AwaitingInput}` (`llm_agent_pause.go:104-117`) carrying `Question/Options/Kind/Priority/ToolCallID/OriginAgent`. In the parent loop this Event would suspend the turn; in a **worker** the swarm runner intercepts it: the worker's `Run` has already terminated after `emitPauses` (`llm_agent_pause.go:90-98` returns; `llm_agent.go:210-214` returns after emitting pauses — the loop does not continue). The swarm runner reads the AwaitingInput payload off the Event and writes a `{status:"needs_user_input", question, options, tool_call_id, child_id}` report entry. No `paused_states` row is written for the CHILD (D-04: no parked children). The PARENT LLM, seeing the report, may call its OWN `ask_user` with `proxied_from_child_id`/`proxied_tool_call_id` (D-05) — that durable pause flows through the normal shipped path.

### Pattern 4: proxied_* wiring (D-05) — 3-layer plumb
The columns + sqlc params exist (`paused_states.sql.go:88-89`) but the domain layer drops them. Plumb:
1. `ask_user.go` Spec (`ask_user.go:87-107`): add optional `proxied_from_child_id` (uuid string) + `proxied_tool_call_id` (string) properties; parse into `askUserArgs` + carry on `ErrAwaitingUserInput` (`ask_user.go:67-73`) + the Event `AwaitingInput` (`event.go:81-88`).
2. `askuser.InsertParams` (`store.go:86-95`) + `Store.Insert` (`store.go:108-117`): add `ProxiedFromChildID *string` + `ProxiedToolCallID string`, set `arg.ProxiedFromChildID`/`arg.ProxiedToolCallID` (pgtype conversion at the boundary like `parseUUID`, `store.go:334`).
3. `runner_persist.go:140-148` `persistPause`: read `ai.ProxiedFromChildID`/`ai.ProxiedToolCallID` off the `*agent.AwaitingInput` and pass into `InsertParams`.

### Anti-Patterns to Avoid
- **Re-introducing cut machinery** (bus.go/tier.go/swarm_talk/swarm_join/ResumeChild/Responder/children-map+RWMutex). The PRD Slice-3 acceptance list (`prd.md:1421-1457`) is **STALE** — D-23's amendment supersedes it. The planner must plan against CONTEXT, not the PRD acceptance bullets.
- **Mutating `SystemPrompt`** per worker — busts the KV cache invariant (CAP-04, `cache_invariant_audit.sh`). Worker framing rides in messages[1]/UserTurns (D-06/D-07).
- **Writing a second spillover mechanism** for big reports — D-15 mandates the shipped `tools.NewResult` only.
- **A periodic ping ticker or restart supervisor** for MCP servers — explicitly forbidden by D-21 + the lifecycle study (`mcp-sidecar-lifecycle-study.md:44-46`), violates the mini-PC no-busy-poll rule.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Concurrent child execution + backpressure | a bespoke channel choreography | reuse `errgroup` + the `parallel.go` leak idioms | The footguns (Go #61611 spawn race, iter-yield-from-goroutine, ack backpressure) are already solved and documented (`parallel.go:14-27`). |
| Tree budget bounding | a per-child step counter | `Budget.Child` shared `*atomic.Int32` (`budget.go:285`) | The depth-3 fan-3 ≤ max_steps proof is shipped (Phase 2 SC#3). |
| Large report truncation + sidecar | manual byte-slicing + file write | `tools.NewResult` (`result.go:94`) | Rune-boundary truncation, traversal-safe sidecar path, degrade-clean on write failure all handled. |
| Deferred-tool discovery | a custom keyword matcher | `tool_search` + `bm25` (`search.go`, `bm25.go`) | BM25 already indexes name+description+arg-field names (`bm25.go:34-49`) — D-24's `swarm_spawn` description is discoverable for free. |
| MCP tool namespacing + collision | per-server prefix logic | `mcptools.Mount` (`mount.go`, `bridge.go:62-93`) — 64-byte cap + collision hash shipped (Phase 8.1) | First production exercise; the machinery is done, only allowlist+Deferred flip remain. |
| Pause persistence + FIFO ordering | a new table | `askuser.Store` + `aura.paused_states` (`store.go`) | Total-order FIFO + crash recovery + AM-02 resolution shipped (Phase 4). |
| Live eval scoring + report emit | a new harness | extend `internal/eval` cot_eval (`harness_cot_eval_test.go`) | OPENROUTER gating, capture extractor, judge, report writer all shipped. |

**Key insight:** Phase 9 is ~90% composition. The genuinely-new code is the ephemeral runner (`internal/swarm/swarm.go`), the `swarm_spawn` tool, the structured-brief builder, and the swarm E2E scenario. Everything else is 5 small surgical edits to shipped files.

## Runtime State Inventory

> Not a rename/refactor/migration phase — but Phase 9 DOES create runtime state (sidecar dirs, MCP subprocesses, a companion daemon). Documented for completeness.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | Per-child transcript dumps `$AURA_RUN_DIR/<conv>/swarm/<w_i>.jsonl` (D-18). | GC'd by the existing run-dir TTL sweep (`conversations.ScanOrphans`, `chat.go:126`) — verify the swarm subdir is covered; if not, the sweep glob may need extending. **Flag for planner.** |
| Live service config | `mail` + `whatsapp` entries in `~/.aura/mcp/servers.json` (managed config, NOT git). The whatsmeow bridge stores `store/messages.db` + session in WSL (NOT git). | Operator registers via `aura mcp install mail` / `aura mcp install whatsapp` (new recipes). Bridge session persists (spike 002: re-auth without QR). |
| OS-registered state | The whatsmeow bridge is a long-lived process (REST :8080) in WSL. | E2E needs a bring-up + health-check step (REST 405 on GET /api/send = alive, `002 README:67`). Preferred: compose service with healthcheck (lifecycle-study recommendation). |
| Secrets/env vars | mail-mcp creds (`MAIL_MCP_PASSWORD` Gmail app password) live in the managed config Env / `.env`, NOT git. `AURA_MCP_*_SERVER_JSON` are TEST-tier overrides only. | None new in code — managed config Env carries them (mcp `--env KEY=VALUE`, `mcp.go:106`). |
| Build artifacts | The whatsapp bridge binary (CGO, built in WSL from the fork) is not an Aura artifact. | Operator builds the fork; Aura spawns it via `wsl -e bash -lc`. |

## Common Pitfalls

### Pitfall 1: The PRD Slice-3 acceptance list is a trap
**What goes wrong:** A planner reading `prd.md:1421-1457` plans `swarm_talk`, `swarm_join`, `tier.go`, bus backpressure, `ResumeChild`, `SpawnInteractive`, children-map RWMutex — all CUT.
**Why:** The PRD predates amendment #12 + the discuss-phase. D-23's Wave-0 amendment supersedes it but hasn't been committed yet.
**How to avoid:** Plan 09-01 (doc amendment) FIRST; plan all code against CONTEXT D-rows, treating the PRD acceptance bullets as superseded.
**Warning signs:** any plan task mentioning a bus, a talk tool, a join tool, tier mapping, or a persistent coordinator.

### Pitfall 2: ParallelAgent will cancel your siblings
**What goes wrong:** Reusing `ParallelAgent` as-is → the first failing/escalating child cancels all the others (`parallel.go:77,123-124`), the exact opposite of D-02.
**Why:** ParallelAgent is a workflow primitive where escalate = "stop the branch."
**How to avoid:** Bypass (Pattern 1) — independent collectors, child errors captured to report slots, never returned to the errgroup.
**Warning signs:** a swarm test where one failing goal zeroes out sibling reports.

### Pitfall 3: proxied_* are NOT pre-wired
**What goes wrong:** Assuming D-05 is a one-line populate because "columns exist."
**Why:** Columns + sqlc params exist (`paused_states.sql.go:88-89`) but `askuser.InsertParams` (`store.go:86`), `Store.Insert` (`store.go:108`), and `persistPause` (`runner_persist.go:140`) all silently drop them.
**How to avoid:** Plan the full 3-layer plumb (Pattern 4) + the `ask_user` Spec args.
**Warning signs:** a "wire proxied" task estimated at <30 min.

### Pitfall 4: SessionID can't contain a slash
**What goes wrong:** Setting worker `SessionID = "<conv>/swarm/w1"` → `validateID` (`result.go:45-58`) rejects the `/` and every worker tool-spillover errors.
**Why:** `validateID` blocks path separators in session_id/tool_call_id (traversal defense T-03-07).
**How to avoid:** Use a flat session id (`<conv>-swarm-w1`); write the D-18 transcript to `<RUN_DIR>/<conv>/swarm/w1.jsonl` as a SEPARATE direct write (not via the tool-spillover sidecar).
**Warning signs:** worker tool results returning `tools.NewResult: ... contains a path separator`.

### Pitfall 5: Per-child summary cap reuse may be too small
**What goes wrong:** Reusing `AURA_CONTEXT_PREVIEW_CAP_BYTES`=2048 (`config.go:154`) for a D-15 "2-4KB" summary truncates legitimate child output.
**Why:** The shipped preview cap is 2048; D-15 wants 2-4KB.
**How to avoid:** Discretion says prefer reuse "unless tests show it too small" — measure in the E2E; if 2048 truncates real summaries, add a dedicated `AURA_SWARM_SUMMARY_CAP_BYTES`. Either way the OVERFLOW path is the shipped `tools.NewResult` sidecar, so nothing is lost — only the inline preview shrinks.
**Warning signs:** E2E aggregation quality drops because child summaries are cut mid-fact.

### Pitfall 6: Fail-soft must not break the ≥1-non-deferred guard
**What goes wrong:** After flipping bridged MCP tools to `Deferred: true` (D-20) AND making boot fail-soft (D-21), a boot where ALL capability tools came from a now-dropped MCP server could leave only deferred tools → `Registry.Validate` (`spec.go:94`) fails closed (`ErrNoNonDeferredTool`).
**Why:** The base registry (`buildBaseRegistry`, `main.go:83-102`) always registers non-deferred built-ins (`text_response`, `current_time`, `web_*`, `sandbox_exec`, `ask_user`), so this is currently safe — but the planner must keep `buildBaseRegistry`'s `reg.Validate()` (`main.go:97`) running BEFORE MCP mounts, and not move the non-deferred built-ins behind MCP. Verify the guard still holds when every MCP server is dropped.
**Warning signs:** boot exits with `ErrNoNonDeferredTool` only when an MCP server is misconfigured.

## Code Examples

### Worker construction (D-06/07/08)
```go
// Source: internal/agent/llm_agent.go:63-103 (LlmAgentConfig + NewLlmAgent)
worker := agent.NewLlmAgent(agent.LlmAgentConfig{
    Client:     parentClient,        // ONE shared llm.Client (privacy-by-construction, PRD #34)
    LLM:        parentCfg,           // D-03: all tiers = AURA_LLM_MODEL
    Registry:   workerRegistry,      // parent registry MINUS swarm_spawn (D-08/D-10) — needs a clone helper
    PreviewCap: cfg.ToolPreviewCap,  // config.go:154 = AURA_CONTEXT_PREVIEW_CAP_BYTES
    RunDir:     cfg.RunDir,
    SessionID:  fmt.Sprintf("%s-swarm-w%d", convID, i+1), // flat — NO slash (result.go:45)
    UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: structuredBrief(goal)}}, // D-07
})
// messages[0] = systemMessage() (prompt.go:25) is appended by NewLlmAgent — byte-stable (D-06)
```

### Child budget fork with snapshot (D-09)
```go
// Source: internal/agent/budget.go:277-299 (Child + the snapshot requirement)
rem := parentBudget.Remaining()            // ONCE before the fan-out loop (budget.go:283-284)
if rem < len(goals)+reserve {              // D-09 pre-flight; reserve ~3 (tune to finalize())
    return tools.NewResult(ctx, fmt.Sprintf(
        "error: insufficient budget — %d goals need %d steps but only %d remain; reduce goals or answer directly",
        len(goals), len(goals)+reserve, rem))
}
// per wave, per child:
childBudget := parentBudget.Child(len(wave))  // shares the *atomic.Int32 (budget.go:287)
```

### Detecting the child pause Event (D-04)
```go
// Source: internal/agent/event.go:81-88 (AwaitingInput) + llm_agent_pause.go:104-117 (emit)
for ev, err := range worker.Run(childIC) {
    if err != nil { report.Status, report.Error = "failed", err.Error(); break } // D-02
    if ai := ev.Actions.AwaitingInput; ai != nil {                                // D-04
        report.Status = "needs_user_input"
        report.Question, report.Options = ai.Question, ai.Options
        report.ToolCallID = ai.ToolCallID   // ground-truth id for D-05 proxied_tool_call_id
        // worker.Run has already returned after emitPauses (llm_agent.go:210-214) — no suspend
    }
    if fr := finalReason(ev); fr != "" { report.Status, report.Summary = "ok", ev.LLMResponse.Content }
    dumpTranscript(ev)  // D-18, best-effort, never fails the swarm
}
```

## State of the Art

| Old Approach (PRD original) | Current Approach (CONTEXT v1) | When Changed | Impact |
|-----------------------------|-------------------------------|--------------|--------|
| `swarm.Coordinator` + bus + DM-by-ID + tier dispatcher (`prd.md:1396-1442`) | ONE blocking `swarm_spawn{goals}` tool, ephemeral runner, partial-results | amendment #12 (2026-05-29) + discuss-phase D-01..D-25 (2026-06-04) | −500 LOC; matches Claude Code/Anthropic/Codex-V1/nanobot industrial shape |
| `AURA_SWARM_*_SERVER` boot env vars | managed config `~/.aura/mcp/servers.json` + `mcp` recipes | spike 001/002 discovery (Codex commit `ae11737a`) | no new boot env vars; reuses shipped CLI |
| Bridged MCP tools `Deferred: false` (`bridge.go:88`) | `Deferred: true` + allowlist | spike 001 finding | protects the 30-50-tool degradation threshold 8.1 defends |
| Boot fail-HARD on any MCP mount error (`main.go:121-128`, `chat.go:139-144`) | fail-soft per-server WARN-and-drop | lifecycle study (2026-06-04) | a dead bridge no longer kills `aura chat` |
| `SpawnInteractive`/`ResumeChild`/`Responder` pause machinery (`prd.md:1452-1457`) | pause-as-report (D-04) | OpenAI Agents SDK precedent + Claude Code observed behavior | no parked children, no volatile pendings |

**Deprecated/outdated:**
- PRD Slice-3 acceptance list (`prd.md:1421-1457`) — superseded by D-23 Wave-0.
- `internal/swarm/swarm.go` stub with `MaxSpawnDepth = 3` — **already deleted** in the tabula-rasa rewrite (dir verified empty); #34(C) note in the PRD is stale.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Bypassing ParallelAgent (Pattern 1) is cleaner than adapting it | Architecture Patterns | If the planner prefers adapt, the leak-safety copy still applies — low risk; CONTEXT grants both. |
| A2 | Worker framing in messages[1]/UserTurns satisfies D-06 (vs a new SystemOverlay field) | Pattern 2 | If D-06's "static worker overlay" is interpreted as literally part of messages[0], a `LlmAgentConfig.SystemOverlay` field is needed — small. Flag for the planner to decide. |
| A3 | Reserve ~3 steps for D-09 is enough for finalize() | Code Examples | finalize() rides OUTSIDE the budget gate (`llm_agent_finalize.go:86-87`, ceiling = max_steps+2), so the reserve is for the PARENT's post-swarm synthesis turns, not the child finalize. Tune in E2E. |
| A4 | The run-dir TTL sweep covers the new `swarm/` subdir | Runtime State | If `ScanOrphans` globs only `conversations/`, the swarm transcripts leak — verify the sweep path. |
| A5 | mail-mcp + whatsapp fork remain reachable/stable for the E2E | MCP / Package audit | Upstream staleness risk (whatsmeow server-enforced 405); the fork pins a working bump but needs pin-and-refresh discipline. |

**These are the rows the planner/discuss-phase should confirm before locking. All other claims are file:line-verified ground truth.**

## Open Questions

1. **D-23's env-add list contradicts D-21.** D-23 says the amendment ADDS `AURA_MCP_MAIL_SERVER` + `AURA_MCP_WHATSAPP_SERVER` to the env catalog — but D-21 says there are **NO** `AURA_MCP_MAIL_SERVER`/`AURA_MCP_WHATSAPP_SERVER` env vars (managed config is the path). These two D-rows conflict.
   - What we know: D-21 is the spike-corrected, later truth (managed config). D-23's env list looks like a pre-spike artifact.
   - What's unclear: whether the amendment should add those two env names at all.
   - Recommendation: the Wave-0 amendment should add ONLY `AURA_SWARM_MAX_GOALS` + `AURA_SWARM_CHILD_TIMEOUT_SEC` (+ note `AURA_SWARM_MAX_CONCURRENT` already exists at PRD line 4766) and explicitly DROP the two `AURA_MCP_*_SERVER` entries, citing D-21. The planner should surface this to the user.

2. **Worker registry clone helper.** `tools.Registry` has no `Clone`/`Without` method (`spec.go:63-86`). D-08/D-10 need "parent registry minus swarm_spawn." Recommendation: add a tiny `func (r *Registry) Without(names ...string) *Registry` (or build it in `internal/swarm`); plan it as an explicit task.

3. **D-06 system-overlay mechanism.** `NewLlmAgent` has no hook to append a worker overlay to messages[0] (`llm_agent.go:80`). Decide: (a) new `LlmAgentConfig.SystemOverlay` field, or (b) worker framing in the D-07 user brief (recommended, simpler, preserves byte-stability). See A2.

4. **Compose service vs single-binary fork for the whatsmeow bridge.** Lifecycle study (`mcp-sidecar-lifecycle-study.md:48-51`) leaves this to planning: compose service (reuses our patch) vs single-binary fork (deletes the daemon, needs re-patch). Recommendation: compose service for v1 (established pattern, patch already applied), note the single-binary option in the amendment as a Phase-16 follow-up.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain (WSL, CGO) | build + race | ✓ (CLAUDE.md: WSL primary) | Go 1.25+ | — |
| `mail-mcp` (Node, stdio) | D-19 mail E2E | ✓ spike 001 VALIDATED, built at `D:/tmp/mail-mcp` | — | E2E mail scenario blocks; unit/property tiers unaffected |
| whatsapp bridge + MCP (WSL, fork) | D-19 WhatsApp E2E | ✓ spike 002 VALIDATED, fork `chetto1983/whatsapp-mcp@6de1dcd` | whatsmeow `20260603132417` | E2E WhatsApp scenario blocks; rest unaffected |
| whatsmeow bridge REST :8080 | D-19 read-back | ✓ (operator brings up; health = 405 on GET /api/send) | — | E2E needs bring-up step before scenarios |
| `OPENROUTER_API_KEY` | D-22 live cot_eval | operator-supplied | — | cot_eval `t.Skip`s (the one legitimate skip), CI unaffected |
| Postgres (`aura.paused_states`) | D-05 proxied wiring test | ✓ (db_integration tier) | PG 17 | proxied unit test uses fake store; integration needs stack up |
| `mcp-neo4j-cypher` / Neo4j | NOT needed this phase | n/a | — | — |

**Missing dependencies with no fallback:** none block the CORE swarm code (unit/property/race tiers are pure-Go + fakes). The MCP/E2E tier is operator-gated by design (D-22, like every shipped live tier).

## Validation Architecture

> Nyquist validation is ENABLED (`workflow.nyquist_validation` not set to false). This section maps each re-specced ROADMAP success criterion + D-25 properties to concrete tiers + invocations.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `pgregory.net/rapid` (property) + `go.uber.org/goleak` (leak) + `cot_eval` build-tag harness (live) |
| Config file | none — Go convention; tag matrix per CLAUDE.md (`db_integration`, `neo4j_integration`, `cot_eval`, `live_e2e`) |
| Quick run command | `go test ./internal/swarm/ ./internal/agent/tools/ -run TestSwarm` |
| Full suite command | WSL: `export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; make quality-full` (vet+build+lint+race+vuln+coverage; stack up for integration tiers) |

### Phase Requirements → Test Map
| Req / SC | Behavior | Test Type | Automated Command | File Exists? |
|----------|----------|-----------|-------------------|-------------|
| SC#1 (re-spec D-23) | 3 parallel children, wall-clock < 1.5× single-worker, race+goleak clean | race + goleak (timing advisory via fake-client) | `go test -race ./internal/swarm/ -run TestSwarmParallelTiming` | ❌ Wave 0 |
| SC#2 (re-spec D-10) | worker has no `swarm_spawn` (tool-not-available) + code guard at synthetic depth ≥ cap → PRD error string | unit (load-bearing literal) | `go test ./internal/swarm/ -run TestSwarmDepthGuard` | ❌ Wave 0 |
| SC#3 (re-spec D-04) | 5 children all pause → 5 `needs_user_input` reports; resume=re-spawn, cancel=no-respawn; no stuck goroutines | unit + goleak | `go test -race ./internal/swarm/ -run TestSwarmMultiPause` | ❌ Wave 0 |
| SC#4 | depth-2 swarm, parent 20 steps → total tree steps ≤ 20 (shared atomic) | unit | `go test ./internal/swarm/ -run TestSwarmBudgetInheritance` | ❌ Wave 0 (reuse Phase-2 `budget_test.go` pattern for the assertion) |
| D-09 | pre-flight rejects when `Remaining() < len(goals)+reserve` → structured error | unit | `go test ./internal/swarm/ -run TestSwarmBudgetPreflight` | ❌ Wave 0 |
| D-13 | `len(goals) > AURA_SWARM_MAX_GOALS` → model-readable error | unit | `go test ./internal/agent/tools/ -run TestSwarmSpawnGoalsCap` | ❌ Wave 0 |
| D-24 | `swarm_spawn` Description contains the anti-over-spawn phrases | unit (literal assert, `finalizeNudge` pattern) | `go test ./internal/agent/tools/ -run TestSwarmSpawnDescriptionLiteral` | ❌ Wave 0 (pattern at `llm_agent_finalize_internal_test.go:92`) |
| D-25 | properties: report len+order=goals; tree steps ≤ remaining; goleak-clean; per-child isolation | property (rapid) + goleak | `go test -race ./internal/swarm/ -run TestSwarmProperties` | ❌ Wave 0 |
| D-05 | proxied_* persisted to `aura.paused_states` | unit (fake store) + integration | `go test ./internal/runner/ -run TestPersistPauseProxied` then `go test -tags db_integration ./internal/askuser/ -run TestInsertProxied` | ❌ Wave 0 |
| D-21 | a bad MCP server entry does NOT abort boot (fail-soft) | unit | `go test ./cmd/aura/ -run TestBuildRegistryFailSoft` | ❌ Wave 0 |
| D-20 | bridged tools mount `Deferred:true`; allowlist filters footgun tools | unit | `go test ./internal/agent/mcptools/ -run TestMountAllowlistDeferred` | ❌ Wave 0 (extend `mount_test.go`) |
| SC#5 (Gate-3, D-22) | live: N workers via tool_use, mail+WhatsApp read-back, timing, judge ≥90%, no over-spawn on control | live cot_eval (operator, paid) | `set -a; . ./.env; set +a; export PATH=...; go test -tags cot_eval -run TestSwarmE2E -timeout 600s -v ./internal/eval/` | ❌ Wave 0 (new scenario in `dataset_cot_eval.go` + judge rubric) |

### Sampling Rate
- **Per task commit:** `go test ./internal/swarm/ ./internal/agent/tools/` + `go vet ./... && go build ./...`
- **Per wave merge:** `go test -race ./internal/swarm/ ./internal/agent/...` + goleak TestMain green
- **Phase gate (Gate 3):** WSL `make quality-full` (coverage ≥85% owned-surface, race, vuln, lint=0, dupl) + the operator-run `cot_eval` swarm E2E (judge ≥90%, mail+WhatsApp read-back) + mutation spot-check ≥70% on `internal/swarm/swarm.go` (WSL go-mutesting) — numbers into `docs/aura-quality-snapshot.md` + the phase VALIDATION.md.

### Wave 0 Gaps
- [ ] `internal/swarm/swarm_test.go` — SC#1/#2/#3/#4, D-09
- [ ] `internal/swarm/swarm_property_test.go` — D-25 (rapid + goleak)
- [ ] `internal/agent/tools/swarm_spawn_test.go` — D-13, D-24 literal
- [ ] `internal/runner/runner_persist_test.go` extension — D-05 proxied (fake store)
- [ ] `internal/askuser/store_integration_test.go` extension — D-05 proxied (db_integration)
- [ ] `cmd/aura/main_test.go` extension — D-21 fail-soft
- [ ] `internal/agent/mcptools/mount_test.go` extension — D-20 allowlist + Deferred flip
- [ ] `internal/eval/dataset_cot_eval.go` + judge rubric — SC#5 live E2E (cot_eval)
- [ ] goleak TestMain in `internal/swarm` (mirror `internal/agent/workflow` TestMain)

## Security Domain

> `security_enforcement` not disabled — included.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | single-user `local`; no new auth surface |
| V3 Session Management | no | — |
| V4 Access Control | yes | D-20 allowlist at `mcptools.Mount` — denies destructive MCP tools (`delete_mailbox`, etc.) to workers; mail/WhatsApp send-to-self ONLY (D-19) |
| V5 Input Validation | yes | `swarm_spawn` args via JSON-schema; `validateID` (`result.go:45`) blocks path traversal in session/tool ids; `ask_user` proxied uuid parsed via `parseUUID` (`store.go:334`) |
| V6 Cryptography | no | no new crypto; reuse shipped trace-id minting |
| V11 (Business Logic / DoS) | yes | Budget tree DoS control (`budget.go:1-3`); D-09 pre-flight + D-13 goals cap + D-12 wave cap + D-11 per-child timeout all bound fan-out resource use (mini-PC RAM/FD) |

### Known Threat Patterns for {Go agent runtime + MCP subprocesses}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Worker fan-out exhausts step/RAM/FD budget | DoS | shared `*atomic.Int32` tree budget + D-09 pre-flight + D-12 `MAX_CONCURRENT` waves + D-13 goals cap + D-11 timeout |
| Destructive MCP tool reachable by a worker (delete_mailbox) | Tampering/EoP | D-20 per-server allowlist at Mount (spike-001 footgun census) |
| MCP server name collision shadows a built-in | Spoofing | `mcptools.Mount` 64-byte-cap + collision-hash + refuse-clobber (`bridge.go:128-133`, shipped 8.1) |
| Secret (Gmail app password) leaking into prose/report | Info disclosure | creds in managed-config Env only; `cot_eval` harness has a release-blocking `secret_redaction` dimension (`scoring_cot_eval.go:25`) |
| Goroutine leak under early break / cancel | DoS (resource) | copy ParallelAgent's multi-arm-select + `defer cancel()` idioms (`parallel.go:14-27`); goleak TestMain |
| Path traversal via worker session/tool ids | Tampering | `validateID` (`result.go:45-58`) — flat session ids (Pitfall 4) |
| Dead MCP server aborts boot (availability) | DoS | D-21 fail-soft per-server WARN-and-drop |

## Sources

### Primary (HIGH confidence — codebase ground truth, this session)
- `internal/agent/workflow/parallel.go` (full) — errgroup/escalate/cancel/ack/leak invariants
- `internal/agent/budget.go` (full) — `Child`/`Remaining`/`ConsumeStep`/snapshot requirement (lines 277-299)
- `internal/agent/llm_agent.go`, `llm_agent_pause.go`, `llm_agent_finalize.go` — worker loop, pause emit, finalize ceiling
- `internal/agent/event.go`, `tools/ask_user.go`, `askuser/store.go`, `migrations/0003_paused_states.up.sql`, `db/sqlc/paused_states.sql.go`, `runner/runner_persist.go` — D-04/D-05 signal + the 3-layer proxied gap
- `internal/agent/tools/spec.go`, `result.go`, `search.go`, `bm25.go`, `text_response.go` — deferred pattern, spillover, BM25 arg-field indexing
- `internal/agent/mcptools/bridge.go`, `mount.go` — Mount/Bridge, `bridge.go:88` Deferred:false, namespacing
- `internal/mcp/managed_config.go`, `cmd/aura/main.go`, `chat.go`, `mcp.go`, `agent.go`, `internal/config/config.go` — boot mount, fail-hard sites, recipes, dry-run precedent, env loading
- `internal/eval/harness_cot_eval_test.go`, `capture_cot_eval.go`, `scoring_cot_eval.go`, `dataset_cot_eval.go` — live harness structure
- `.planning/spikes/001-*/README.md`, `002-*/README.md`, `002-*/bridge-patch.diff` — proven MCP mounts, setup steps, JID duality, footgun census
- `docs/research/mcp-sidecar-lifecycle-study.md` — locked fail-soft supervision model
- `prd.md:1388-1475` (Slice 3, STALE acceptance), `prd.md:4765-4769` (env catalog)
- `.planning/ROADMAP.md` (Phase 9 SC), `.planning/REQUIREMENTS.md:34` (CAP-03), `09-CONTEXT.md` (D-01..D-25)

### Secondary (MEDIUM)
- `docs/research/mcp-sidecar-supervision.md` — note: only an 18-line stub (table cut off); the substantive supervision content is in `mcp-sidecar-lifecycle-study.md`. **Flag: the supervision.md the CONTEXT references is essentially empty.**

### Tertiary (LOW)
- none — every claim traces to a read file.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all symbols read at file:line; nothing inferred.
- Architecture (bypass-vs-adapt, worker overlay): HIGH on the facts, MEDIUM on the recommendation (CONTEXT grants Discretion either way).
- Pitfalls: HIGH — each is a concrete code constraint verified this session (proxied gap, slash-in-sessionID, ParallelAgent cancel, ≥1-non-deferred guard).
- proxied_* gap, D-23 env contradiction, empty supervision.md: HIGH — these are the corrections the CONTEXT did not anticipate.

**Research date:** 2026-06-04
**Valid until:** 2026-06-18 (stable in-tree code; the only volatility is the external whatsmeow fork — pin-and-refresh per spike 002 finding #5)
