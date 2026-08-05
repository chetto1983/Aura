# Architecture Research — v2.1.0 HERMES-CLAUDE_PARITY

**Domain:** Agent-harness correctness + tool-surface + context-management integration on an
existing ~98k-LOC Go monolith (Aura)
**Researched:** 2026-08-05
**Confidence:** HIGH (every claim below is grounded in a read of the named file:line; no
training-data assertion about this codebase's behavior is used unverified)

This is not an ecosystem survey — it is an integration-point map for four already-scoped
changes against the existing architecture (`.planning/codebase/ARCHITECTURE.md`,
`.planning/codebase/STRUCTURE.md`), driven by `docs/audit/live-conversations-2026-08-04/`.

---

## 1. The replay defect (F-1)

### 1.1 Two dedup layers exist today, and only one of them is broken

Aura already runs **two independent anti-duplication mechanisms** stacked in front of every
mutating tool call, and the audit's F-1 (`docs/audit/live-conversations-2026-08-04/FINDINGS.md:13-84`)
implicates only one of them.

**Layer A — the per-dispatch reservation ledger** (`internal/gateway/reserve.go`). Keyed by
`ReservationKey{ConversationID, RequestID, ToolCallID}` (`internal/gateway/gateway.go:105-109`),
written via `g.store.Reserve` (`reserve.go:220-250`). This is GATE-03/04's original job:
protect against the *exact same wire dispatch* (same `tool_call_id`) being processed twice —
crash-recovery of one turn, a duplicate SSE frame, etc. Since an LLM never reuses a
`tool_call_id` for a call it did not literally re-emit verbatim, Layer A never conflates two
*different* logical actions. **It is not the source of F-1** and needs no change.

**Layer B — the idempotency operation registry** (`internal/idempotency`, driven from
`internal/agent/idempotency_operation.go`). The child key is built at
`idempotency_operation.go:32-46` from `parent scope + parent key + parent fingerprint + tool
scope + ARGS FINGERPRINT` — explicitly **excluding** `tool_call_id` and any step counter
(the comment at line 16 states this is deliberate: *"Request and tool-call IDs stay audit-only
and therefore never participate"*). `beginOperation` (`internal/gateway/reserve.go:35-93`) calls
`idempotency.Begin` on this key; a hit against a durably `Completed` row returns
`DecisionReplay`, and `execTool` (`internal/agent/llm_agent_retry.go:141-143`) returns that
recorded result **without calling `tool.Execute`**. This is what F-1 measured: two `shell_exec`
calls with identical args inside one turn collapse onto the same child key and the second is
served the first's stale, already-superseded result.

### 1.2 What Layer B is actually protecting, and why that scope matters

Layer B's parent operation is attached at exactly four places, and tracing all four is what
makes the fix tractable:

| Parent scope | Where attached | Genuinely re-attempted from scratch? |
|---|---|---|
| `ScopeHTTPMutation` (`agent_run`) | `internal/agui/idempotency_http.go:169-248` | **No.** `agent_run` is special-cased (lines 230-248): its parent operation is marked `Indeterminate` the moment the SSE stream finishes, win or lose. A genuine client retry of the same `Idempotency-Key` after that hits `DecisionIndeterminate`/`DecisionInProgress` and is rejected at the HTTP layer (`writeIdempotencyDecision`, lines 352-394) — it **never reaches the tool-dispatch loop a second time**. |
| `ScopeCLICommand` | `cmd/aura/chat_repl.go:388-393`, `cmd/aura/idempotency.go:155-170` | **Yes.** An operator can legitimately re-run the same CLI invocation. |
| `ScopeSchedulerRun` | `internal/cron/dispatch.go:205-241`, key = `task.ID + ":" + claim.RunID` | **Yes.** A crashed dispatcher reclaims the same `RunID` and re-dispatches the same agent turn from scratch — same key, same fingerprint, fresh `tool_call_id`s from the fresh LLM turn. |
| `ScopeApproval` | Declared as an allowed parent scope (`idempotency_operation.go:24`) but **never constructed** as a top-level operation anywhere in the codebase today. | N/A today — see 1.3. |

So the coarse, cross-`tool_call_id` collapsing Layer B performs is **only load-bearing for CLI
and scheduler retries**, where the *entire parent* is legitimately re-run from a cold process.
For the interactive chat path — the one F-1 was measured on — Layer B's cross-attempt
protection is not exercised at all (the HTTP layer already blocks a genuine outer retry). What
F-1 actually hits is the *side effect* of a key scheme built for cross-restart retries: it also
collapses two **different, deliberate** calls made within one continuous `LlmAgent.Run()`
execution, because the key has no notion of "which occurrence is this."

### 1.3 The approval-resume path, traced explicitly

`internal/gateway/decide.go:47-84`: a `gated(tier)` call (Destructive only, per
`decide.go:20-35`) is routed to `routeApprove` (`internal/gateway/approve.go:91-131`) **before**
`beginOperation` is ever reached. `routeApprove`'s withheld branch returns
`Verdict{Approve, ApprovalRequest}` and the caller (`execTool`,
`internal/agent/llm_agent_retry.go:127-136`) returns that as a normal tool result *without*
calling `deriveToolOperationContext`/`beginOperation` at all — the withheld attempt never
touches Layer B. The model relays the request via `ask_user`; the operator accepts; the
**retry** re-enters `execTool` with a **new** `tool_call_id` (a fresh LLM turn) and, because the
operator's resolution is now attached (`resolvedApproval`, `approve.go:65-98`), `routeApprove`
returns `Allow` and `Decide` proceeds to `beginOperation` **for the first time** for this
operation. One Begin call, clean Acquire — F-1's collapse cannot occur on the ordinary
withhold → approve → retry path, because there is only ever one completed Begin.

The one place this *could* matter: if the post-approval retry itself crashes mid-execution and
is retried a **second** time (another fresh `tool_call_id`), the first post-approval attempt's
operation is `InProgress` or `Indeterminate` — never `Completed` — so `Begin` returns
`DecisionInProgress`/`DecisionIndeterminate`, not `DecisionReplay`. **Any fix must only change
behavior on a `DecisionReplay` (Completed) hit**, never on `InProgress`/`Indeterminate`, or it
reopens the exact class of bug the comment at `reserve.go:233-246` documents fixing (a
fabricated success for an ambiguous, possibly-still-running mutation).

### 1.4 Evaluating the three directions against this trace

**Option 1 — add `tool_call_id` (or a step counter) to the child key, universally.**
**Reject.** This is precisely what the design comment at `idempotency_operation.go:16` already
considered and excluded, and the CLI/scheduler trace in 1.2 shows why: a reclaimed scheduler
`agent_job` (same `RunID`, `cron/dispatch.go:226-234`) or a re-run CLI command reissues its
mutating tool calls with **fresh** `tool_call_id`s every time (new LLM turn). Folding
`tool_call_id` into the key universally would make every such reclaim/retry re-execute its
mutating side effects — reopening the double-apply failure mode `reserve.go:233-246` was
written to close, just relocated from the reservation layer to the operation-registry layer.

**Option 2 — a per-spec `ReplayPolicy` that never replays a *terminal completed* result.**
**Recommended.** `tools.Spec.ReplayPolicy` (`internal/agent/tools/spec.go:72-80`) currently has
exactly one value, `ReplayToolResult`, applied uniformly. Add a second value — call it
`ReplayReissueExecutes` — for tools whose real-world effect depends on external state the agent
itself can change between identical-args calls: `shell_exec`
(`internal/agent/tools/shell_exec.go:114-116`, the tool actually measured in F-1), `fs_write`,
`fs_edit`, `task` run_now, `memory_forget`, `skill` create/update, `swarm_spawn` — in practice
most of Aura's native mutating tools, and (see §2) every MCP-bridged mutating tool via
`applyMCPOperationMetadata` (`internal/agent/mcptools/bridge.go:226-236`), which today hardcodes
`ReplayToolResult` for all of them.

The mechanism: leave the derived child key **unchanged** (parent+tool+args, no `tool_call_id`)
so the CLI/scheduler cross-restart protection in §1.2 is untouched. Change `beginOperation`
(`reserve.go:35-93`) so that when `idempotency.Begin` returns `DecisionReplay` **and** the
tool's `ReplayPolicy` is the new value, it does not hand back the stale `tools.ToolResult` —
instead it derives a disambiguated child key (an occurrence discriminator scoped to *this
continuous execution*, following the existing precedent of an in-process, per-run canonical-args
tracker at `internal/agent/budget_dedup.go:35-83`, which already fingerprints
`name+canonical_json(args)` across one run for loop detection) and re-issues `Begin` — a fresh
Acquire, a real `tool.Execute`. Any other decision (`InProgress`, `Indeterminate`, `Conflict`,
`Rejected`) is untouched, which is exactly what §1.3 requires: the approval-resume crash-retry
sub-case never sees a `Completed` row to redirect, so it keeps today's safe deny/wait behavior.

**Option 3 — mark a replayed result in the preview.** **Adopt unconditionally, as a companion,
not a substitute.** `reserve.go` already has the idiom: `resultExpiredMarker`
(`reserve.go:24-28`) is appended when a recorded end's sidecar was GC'd. A parallel
`replayedMarker` appended in `replayResult` (`reserve.go:285-305`) whenever `Verdict.Replay` is
returned costs a few lines and directly targets the failure mode the audit actually measured:
the model built a misdiagnosis (an ArcadeDB-orphan-nodes theory, persisted to memory) on a
result it could not tell was stale (`FINDINGS.md:58-67`). This is cheap enough that it should
ship **regardless of which of Option 1/2 is chosen**, and it is the only one of the three that
also protects tools that legitimately keep `ReplayToolResult`.

### 1.5 Files touched, and size headroom

| File | Current LOC | Change | Headroom |
|---|---|---|---|
| `internal/agent/tools/spec.go` | 205 | add `ReplayReissueExecutes` const | ample |
| `internal/agent/idempotency_operation.go` | 56 | none (key derivation unchanged) | — |
| `internal/gateway/reserve.go` | 305 | new branch in `beginOperation`; `replayedMarker` in `replayResult` | ample (< 600) |
| `internal/agent/mcptools/bridge.go` | 527 | `applyMCPOperationMetadata` needs a policy hook (see §2) — **do this addition in `bridge_memory.go` (69 LOC) generalized, or a new sibling file, not in bridge.go**, which is already within ~70 LOC of the 600 cap |
| every native mutating tool file that should flip policy | — | one-line `ReplayPolicy` change per file | n/a |
| `internal/gateway/guard.go` | — | extend the existing multiplexed-classifier boot-guard pattern (`classify.go:23-31`) to assert every `Mutating` tool declares an explicit `ReplayPolicy` | prevents silent under-classification on future tools |

No migration is required: the `aura.idempotency_operations` schema does not change — only
which key is asked for, and what a `DecisionReplay` hit does with it, changes.

---

## 2. Where the tool facade lives

### 2.1 Current state: calendar/whatsapp are already generic MCP recipes

`internal/mcp/manager/catalog.go:112-173` (`BuiltInCatalog`) already mounts `calendar` (forked
`aura-pim-mcp`, mail+calendar+contacts) and `whatsapp` (whatsmeow bridge) as managed MCP
servers, through the exact same `mcptools.Bridge`/`mcptools.Mount` path
(`internal/agent/mcptools/bridge.go:151-204, 410-435`) the `memory` MCP uses. Every bridged tool
is namespaced `<namespace>__<tool>` (`bridge.go:214`, `namespacedName`), `Deferred` by default
(`bridgePolicy.defaultDeferred()`, `bridge_memory.go:21-23`), registered into the **same**
`tools.Registry` native tools live in, and therefore already indexed by `tool_search`
(`internal/agent/tools/search.go`, which ranks over `Registry.All()`). The milestone's "curated
surface over 28 third-party tools" is not a new integration — it is a curation problem on an
integration that already exists.

### 2.2 The curation pattern already exists, for `memory`, in one file

`internal/agent/mcptools/bridge_memory.go` is a 69-line proof that this exact problem
(narrow a raw multi-tool MCP surface to what the model should see) has already been solved once:

- `bridgePolicy{memory bool, recipeSource string}` (line 10-13), currently keyed only on
  `namespace == "memory"` (`defaultBridgePolicy`, line 15-17).
- `modelFacing(tool string) bool` (line 37-43) hides six raw ArcadeDB tools
  (`memoryHiddenFromModel`, line 28-35) from the model while leaving the read-plan
  (`memory_recall`) and the mutating tools visible.
- `withMemoryUserIdentifier` (line 45-58) transparently injects a derived argument
  (`user_identifier`) into every call whose schema accepts one.

This is precisely the shape "curate a raw third-party MCP surface into a smaller model-facing
set" needs, at a different multiplier (28 tools across two namespaces instead of ~20 across
one).

### 2.3 Recommendation: generalize `bridgePolicy`; do not build a new package or new native tools that delegate to the bridge

Extend `bridgePolicy` into a namespace-keyed table (calendar, whatsapp, memory, ...), each entry
providing:

1. **A hide-list** (`modelFacing`), mirroring `memoryHiddenFromModel`, for the raw tools the
   curated surface should not expose to the model at all.
2. **A per-tool risk override** feeding `mcpToolRisk`/`bridge_risk.go` — today
   `applyMCPOperationMetadata` (`bridge.go:226-236`) gives every mutating MCP tool the same
   `Mutating:true` treatment uniformly; a calendar `delete_event` and a WhatsApp `send_message`
   should not necessarily classify identically, and the current uniform default is a gap this
   milestone should close alongside the curation work, not leave for later.
3. **The same `ReplayPolicy` override §1's fix needs.** `applyMCPOperationMetadata` currently
   hardcodes `ReplayToolResult` (`bridge.go:230`) for every mutating MCP tool. Since this is the
   *other* place mutating-tool metadata is assigned outside `internal/agent/tools/*.go`, giving
   `bridgePolicy` a `ReplayPolicy` override at the same time as the hide-list/risk override means
   there is only ever one remaining metadata-assignment site (native tool files) left to audit
   per-tool, not two moving targets landing at different times.

Do **not** build this as a new package: `bridgedTool`, `specFromToolDefWithPolicy`,
`applyMCPOperationMetadata`, and `registerBridged`'s collision handling
(`bridge.go:426-480`) already do the mechanical work generically; a parallel package would
duplicate all of it (forbidden by the "REUSABLE CODE" rule).

Do **not** build this as hand-authored native tools that delegate to the bridge for simple
hide/rename curation, because that would (a) bypass the bridge's own description-capping and
error-bounding (`capSchemaDescriptions`, `boundedMCPError`, `bridge.go:248-273, 344-352`) unless
reimplemented, (b) require a second, hand-maintained spec per action that drifts from the live
`mcp.ToolDef` the moment the sidecar's schema changes — `refreshSpec` (`bridge.go:63-97`)
already keeps the bridged spec in sync on every reconnect, for free — and (c) fight the
one-bridged-registration-per-server invariant `finishMount`/`registerBridged` enforce.

**One exception:** where the milestone genuinely wants to **merge several raw tools into one
model-facing action** (not just hide/rename — e.g. collapsing several raw
`calendar__list_events`/`calendar__search_events` into one `calendar__events` with an `action`
discriminator), that *is* legitimately a native, hand-authored, multiplexed tool — Aura already
has this pattern for `skill`/`task`/`swarm_spawn` (`tools.Spec.Multiplexed`,
`spec.go:58-64`; `classify.go:22-31`'s `multiplexedClassifiers` map with a boot-guard requiring
every multiplexed+mutating tool to resolve a concrete tier). A merged calendar/whatsapp facade
tool should follow that exact convention — a new small file in `internal/agent/tools/`, an entry
in `multiplexedClassifiers`, and a `guard.go` assertion — not a copy of the bridge's dispatch
logic. Use the bridge-policy extension (2.3.1-3) for the hide/rename majority of the 28 tools,
and the multiplexed-native-tool pattern only for genuine merges.

### 2.4 Integration points unaffected by the facade choice

Because `tool_search`, gateway risk classification, and idempotency scope all operate generically
over `Registry.All()` / `Tool.Spec()`, none of them need to change to accommodate a facade built
either way — the facade only changes **which** tools get registered and **what** their `Spec()`
says.

---

## 3. Where the summarization rung attaches

### 3.1 The constraint that rules out the obvious answer

`internal/conversations/context.go` is 590 LOC (the 600-LOC ceiling leaves ~10 lines of room —
functionally "no room"). `applyContextLadder` (`context.go:288-350`) is explicitly documented as
*"a pure function of (turns, cfg, encoder) except for the L2.5 rot-event write through emit. No
LLM call is made."* Adding an LLM-calling summarization branch directly into this function would
(a) not fit in the file, (b) break the stated purity invariant the existing L1/L2/L2.5 unit
tests rely on for offline testability, and (c) risk the `messages[0]`/`messages[1]`
byte-stability invariant (`alwaysBlockSeq`/`isAlwaysBlock`, `context.go:46-57, 375-381`) if a
network-dependent step gets tangled into the same call path.

### 3.2 The seam already exists elsewhere in this codebase, for the same class of problem

`internal/conversations/title.go` is the precedent to copy, not invent. It lives in the SAME
package as `context.go` and already makes a real LLM call — but as a **stateless, injected-client
function**: `GenerateTitle(ctx, client llm.Client, model string, history []llm.Message)
(string, error)` (`title.go:26-28`), with the worker lifecycle (goroutine, timeout, WaitGroup)
owned entirely by the **Runner** (`internal/runner/runner.go:432-435`,
`r.maybeAutoTitle(ctx, convID, history)`), not by the `conversations` package. `title.go` imports
`internal/llm` for the `Client`/`Request`/`Message` types but holds no LLM state itself — the
Runner supplies the client, model, and timeout per call.

### 3.3 Recommended design

1. **A new sibling file**, e.g. `internal/conversations/summarize.go`, exporting a stateless
   function shaped exactly like `GenerateTitle` — `Summarize(ctx, client llm.Client, model
   string, turns []Turn) (string, error)` — that produces a compact text summary of the turns
   about to be hard-dropped. This file, not `context.go`, carries the LLM dependency, so
   `applyContextLadder` and its existing tests are untouched.
2. **A new field on `ContextConfig`**, distinct from `TransientContext` (which is positioned
   immediately before the *current* user turn and is already claimed by the memory-context
   injection, `internal/runner/runner_context.go:44-66`) — e.g. `PriorSummary string` — consumed
   by a new, still-**pure** step inside `applyContextLadder` that splices it in as a protected
   turn immediately after `alwaysBlockSeq`, using the same "protected head" mechanics
   `injectAlwaysBlock`/`isAlwaysBlock` already implement (`context.go:359-381`). The ladder
   itself gains no I/O — it only gains one more optional string input to place, exactly as it
   already places `AlwaysBlock` and `TransientContext`.
3. **Two-pass orchestration in the Runner**, not the ladder: call `loadTurnHistory` as today
   (`internal/runner/runner_context.go:18-30`); on `conversations.ErrContextWindowExceeded`
   (`context.go:71-75`), invoke the new `Summarize` function on the turns the ladder would
   otherwise hard-drop, then retry `loadTurnHistory` once more with `PriorSummary` populated.
   This is the cheapest possible seam: **zero added cost on the common case** (the pure ladder
   still runs once, unchanged, for every turn that fits), and the network dependency is invoked
   at most once per turn, only under actual pressure — mirroring hermes' own
   `should_compress`-first, compress-only-if-needed order
   (`docs/audit/live-conversations-2026-08-04/CONTEXT-MANAGEMENT.md:16-22`).
4. **Anti-thrash/cooldown state (C-5)** — hermes' `"ineffective"` streak and `"cooldown:<s>"`
   backoff — belongs as Runner-owned, per-conversation, in-process state, consistent with the
   existing "process-local coordination is intentionally bounded" pattern already used for
   conversation locks (`internal/runner/runner_session.go`), in a new file (e.g.
   `internal/runner/runner_context_summarize.go`) rather than a new persisted table. If durable
   cross-restart cooldown is later wanted, `context_rot_events` (migration `0005_conversations`,
   `conversation_id, action, pairs_dropped, tokens_before, tokens_after`) could carry a new
   `action="summarized"` value using the existing columns loosely (turns-summarized in
   `pairs_dropped`, before/after token counts as-is) without a migration; a richer audit (e.g. a
   fallback-used flag) would need one — flag this as an open call for whoever specs the phase,
   not resolved here. The next free migration slot as of this research (`ls
   internal/db/migrations/ | tail -1` → `0093_document_pipeline_convergence`) is **not** to be
   hardcoded into a plan; re-derive it at landing time per project convention.

This design keeps every existing `context_test.go` assertion about L1/L2/L2.5 valid unchanged,
adds the new rung entirely in new files, and only touches `ContextConfig` (an added field, not a
changed one) and the Runner's history-loading call site.

---

## 4. Build order

| Order | Work | Depends on | Why this position |
|---|---|---|---|
| 1 | **F-1 fix** — new `ReplayPolicy` value, `beginOperation` branch, replayed-marker | none | Foundational and lowest-risk: touches only `tools.Spec`, `idempotency_operation.go`, `reserve.go` — no new tools, no new packages. Every subsequent change (facade tools, un-defer/merge, MCP trust drop) creates or edits tool specs that need to declare the *right* `ReplayPolicy` from day one. Landing this first means later work classifies correctly instead of retrofitting every spec touched afterward. Verify against the existing suites first: `internal/gateway/reserve_test.go`, `internal/gateway/idempotency_test.go`, `internal/agent/llm_agent_retry_test.go`, `internal/agent/llm_agent_retry_claim_leak_test.go`. |
| 2 | **MCP bridge**: drop the untrusted-wrapper framing (`frameMCPDescription`/`frameMCPSummary`, `bridge.go:354-390`); generalize `bridgePolicy` for the calendar/whatsapp facade, carrying the new `ReplayPolicy` + risk overrides | (1) | `applyMCPOperationMetadata` needs the new `ReplayPolicy` vocabulary from step 1 to assign anything other than the default to a bridged tool. Doing this right after step 1 leaves exactly one remaining metadata-assignment site (native tool files) to audit, not two moving targets. Independent of the context-ladder work (§3) — no package overlap — so it does not block or get blocked by it. |
| 3 | **Tool-surface un-defer/merge/flatten** (56 → ~26 model-facing tools; `task`/`memory_recall`/`skill`/`web_search` flattening) | (1), (2) | Every touched or newly-merged spec needs a correct `Mutating`/`ReplayPolicy`/`OperationScope`/`Multiplexed` combination (step 1's vocabulary) and the calendar/whatsapp facade shape (step 2) must be settled first, since facade tools count toward the ~26-tool budget this step is sizing against. Highest surface area (every native tool file, `deriveActivated`/`deriveEverLoaded`/`isDeferredUnloaded` in `llm_agent_promote.go`, the registry boot-guard) — better to land it on a settled idempotency + MCP-trust foundation than churn through both at once. |
| 4 | **Context ladder — summarization rung** (audit's own order: C-3 tool_search eviction → C-4 real-token budget → C-2 ghost-skill marker → C-6 breakdown → C-1 summarization) | none of 1-3 | Touches only `internal/conversations` and `internal/runner` — zero package overlap with tools/gateway/mcptools, so it has no hard dependency on 1-3 and could run as a parallel workstream. If sequenced serially, doing it last avoids the tool-surface renumbering in step 3 disturbing the per-turn token counts that C-4's real-`prompt_tokens` budget is measuring against — better to tune against the *final* manifest shape than a moving one. Within this step, keep the audit's internal order: the cheap, deterministic wins (C-3, C-4) de-risk and prove the token accounting before the expensive LLM-summarization rung (C-1) is attempted, and C-5's anti-thrash machinery should be budgeted as part of C-1, not treated as a separate follow-up (it is the cost hermes already paid for this exact rung, per `CONTEXT-MANAGEMENT.md:100-106`). |

**Not scoped by this document:** F-2 (memory `supersedes` closing too many facts) is an
`cmd/arcadedb-mcp` tool-design problem, not an agent/gateway/context-boundary one — but it is
mounted through the same MCP bridge namespace policy this document's §2 touches, so it is a
reasonable candidate to bundle into the same phase as step 2 rather than schedule separately.

---

## Sources

All findings are grounded in direct reads of the following (file:line citations inline above):
`internal/agent/idempotency_operation.go`, `internal/gateway/{reserve,decide,gateway,approve,classify}.go`,
`internal/idempotency/{types,context,store,maintenance}.go`, `internal/agent/llm_agent_{retry,tool,pause,promote}.go`,
`internal/agent/mcptools/{bridge,bridge_memory}.go`, `internal/mcp/manager/catalog.go`,
`internal/conversations/{context,title}.go`, `internal/runner/runner_context.go`,
`internal/agent/tools/{spec,shell_exec,search}.go`, `internal/agent/budget_dedup.go`,
`internal/agui/idempotency_http.go`, `internal/cron/dispatch.go`, `cmd/aura/{chat_repl,idempotency}.go`,
`internal/db/migrations/0005_conversations.up.sql`, `docs/audit/live-conversations-2026-08-04/{FINDINGS,CONTEXT-MANAGEMENT}.md`,
`.planning/codebase/{ARCHITECTURE,STRUCTURE}.md`, `.planning/PROJECT.md`.

No external ecosystem research was needed — this is entirely a same-codebase integration
question, and every claim above traces to a specific read, not to training-data assumptions
about how idempotency/MCP/context-management "usually" work.

---
*Architecture research for: v2.1.0 HERMES-CLAUDE_PARITY milestone integration*
*Researched: 2026-08-05*
