# Roadmap: Aura

## Overview

v2.1.0 HERMES-CLAUDE_PARITY brings the agent harness to parity with hermes-agent and
Claude Code. The organising insight, from auditing two live 2026-08-04 sessions: Aura's own
long-term memory holds nine `learned_lesson` facts, eight of which are workarounds for a
defective surface, six of which restate rules the system prompt already contains. She wrote
her own bug report. This milestone is done when those lessons become unnecessary — Phase 53
uses her memory as the oracle for that.

Phase numbering continues from v2.0.0 (which closed at Phase 44) — this milestone is Phases
45-54. All 77 v1 requirements from REQUIREMENTS.md map to exactly one of these 10 phases; the
v2 items (CTX-V2-01, CTX-V2-02, TOOL-V2-01, TOOL-V2-02) and the Out of Scope table are not
scheduled here.

**Build order and why it isn't negotiable** (ARCHITECTURE.md section 4, traced to specific
call sites, not stylistic preference):

1. **Phase 45 (harness correctness) is foundational.** It introduces the `ReplayPolicy`
   vocabulary every later phase's tool specs need to declare correctly from day one, rather
   than retrofitting it after the fact.
2. **Phase 46 (MCP trust/facade) needs Phase 45's vocabulary** — `applyMCPOperationMetadata`
   can't assign anything but the uniform default to a bridged tool without it. Doing this
   right after Phase 45 leaves exactly one remaining metadata-assignment site (native tool
   files) to audit in Phases 47-48, not two moving targets.
3. **Phases 47-48 (tool-surface) need both prior phases settled** — every touched/merged spec
   needs the correct `Mutating`/`ReplayPolicy`/`OperationScope`/`Multiplexed` combination from
   Phase 45, and Phase 46's facade shape must be settled first since facade tools count toward
   the ~26-tool budget Phase 48 sizes against. Tool-surface work is split across two phases
   (47: ceremony strip, 48: un-defer/merges) rather than one giant phase, because it touches
   live persisted state (COMPAT-01/02/03) and PITFALLS.md documents distinct blast radii for
   each kind of change — a parameter drop is lower-risk than a rename/merge.
4. **Phase 49 (memory tiers) is a secondary track** that emerged from the operator's
   post-briefing decisions, not from the original architecture research. It depends on
   Phase 45 (the entity-resolution baseline in the same `internal/arcadedb/memory.go` code
   MEM-04/05 touch) and Phase 46 (the `bridgePolicy` generalization its new short-term/
   reasoning-tier tools reuse for hide-list/risk/replay classification).
5. **Phase 50 (context ladder) has zero package overlap with 45-49** (`internal/conversations`
   + `internal/runner` only) and could run in parallel. Sequenced last among the technical
   phases so its real-token budget (CTX-01) tunes against the *final* manifest shape rather
   than one still being renumbered by Phase 48's un-defer/merges.
6. **Phase 53 (the spike) needs Phase 49's retrieval mechanism** to exist so it can measure
   "does retrieval recover what the ladder drops" against a prototyped summarization
   approach, on the real audit corpus already in hand.
7. **Phases 51-52 (delegation, steering) were added by operator decision on 2026-08-05**, after
   reading hermes' delegation against Aura's. Both implement designs that already exist in the
   repo and were never built: the durable swarm-messaging substrate (approved) and the
   mid-turn steering study. Delegation precedes steering because the steering design
   reconciles with the substrate in its own section 8, and both attach to the same run identity.
8. **Phase 54 (milestone exit) depends on everything** — it retires the compensating lessons
   only once the defects they compensate for are actually fixed, and validates the whole
   milestone by replay, not by a green test suite (ACC-01).

**On evidence:** every phase's success criteria below are checked by having a real
conversation with Aura against the live stack and reading OpenTelemetry traces,
`aura.tool_invocations`, `aura.conversation_turns`, and `aura.context_rot_events` — never by a
green test suite (ACC-01, ACC-02, established as day-one policy in Phase 45). No new eval
harness is built; `internal/eval/` stays deleted.

## Phases

**Phase Numbering:**
- Continues from v2.0.0's Phase 44. This milestone is Phases 45-54.
- Decimal phases (45.1, 45.2, ...) would be urgent insertions between these, if needed.

- [ ] **Phase 45: Harness correctness** - Idempotency replay fix and memory-write guardrails close the two headline audit defects
- [ ] **Phase 46: MCP trust and facade** - Drop the untrusted-MCP wrapper where it's earned; curate calendar/WhatsApp into one surface
- [ ] **Phase 47: Tool-surface ceremony strip** - Host-fills drop from schemas, approvals resolve without a resume payload, files auto-deliver and auto-index
- [ ] **Phase 48: Tool-surface un-defer and merges** - the manifest lands on exactly 14 loaded tools; the system prompt regenerates to match
- [ ] **Phase 49: Memory tiers** - Short-term searchable retrieval and a PRD-amendment-gated reasoning tier
- [ ] **Phase 50: Context ladder legibility** - Real token accounting, eviction, and per-category visibility
- [ ] **Phase 51: Durable delegation** - The approved swarm substrate gets built; workers get a real brief, real limits, and a turn that no longer blocks
- [ ] **Phase 52: Mid-turn steering** - The operator can type into a running turn and redirect it at the next round boundary
- [ ] **Phase 53: Summarization spike** - Evidenced decision on retrieval vs. summarization vs. both
- [ ] **Phase 54: Milestone exit** - Retire the compensating lessons and skill; validate parity live

## Phase Details

### Phase 45: Harness correctness
**Goal**: Aura's harness never reports a tool result it did not produce this call, and a
memory correction touches exactly the fact it names — the two headline defects the
2026-08-04 audit found are closed at the root, not patched at the symptom.
**Depends on**: Nothing (first phase of this milestone)
**Requirements**: HARN-01, HARN-02, HARN-03, HARN-04, HARN-06, HARN-07, HARN-08, HARN-09, MEM-04, MEM-05, ACC-01, ACC-02
**Rationale**: Foundational and lowest-risk (ARCHITECTURE.md §1/§4) — touches only
`tools.Spec`, `idempotency_operation.go`, `reserve.go`, and `internal/arcadedb/memory.go`; no
new tools, no new packages. **Corrected 2026-08-13 (measured against commit `09f91a865`,
prd.md Amendment #121): this phase does NOT introduce a new `ReplayPolicy` value.**
`ReplayToolResult` remains the only value; the discriminator is a `RoundOrdinal` field added
to the child operation key's `FingerprintTyped` struct in `deriveToolOperationContext`, and
the round-ordinal mechanism it depends on already exists in `internal/agent/model_round.go`.

**The open `tool_call_id` question is answered, and the answer is "do not key on it"**
(hermes `agent/message_sanitization.py:536-566`, `run_agent.py:4601-4648`). Models and
providers reuse one call id across different calls in a single batch — observed on Kimi
Responses replays, Ollama-compatible endpoints, and degraded models at long context — and
strict providers, **DeepSeek among them and DeepSeek is Aura's default**, reject duplicate ids
outright. Hermes derives a deterministic id from `(fn_name, arguments, index)` when the API
omits one, and repairs collisions with an `<id>_d<n>` suffix, never a UUID, because random ids
break prompt-cache prefix stability. Uniqueness is a property the harness must ENFORCE, not one
it can key at-most-once on.

**The discriminator is the round boundary.** Hermes drops identical `(name, arguments)` pairs
*within one assistant message* (a model error) but lets identical calls in *different rounds*
both execute (a deliberate re-issue). That is exactly the distinction Aura collapses: audit
turns `[058]` and `[062]` were separate assistant messages with an `fs_write` between them, and
the second was served from the operation registry. The evidenced fix direction is therefore a
per-turn ROUND ORDINAL in the child operation key — deterministic, reproducible across a
replayed dispatch (so the CLI/scheduler at-most-once protection the architecture research
worried about survives), and discriminating in exactly the place hermes discriminates. Confirm
it against Aura's own dispatch loop before building; do NOT adopt `tool_call_id`.

HARN-08 and HARN-09 land here because both are the same seam. Ship the `replayedMarker` (mirroring `resultExpiredMarker`)
regardless of which direction is chosen — it is the cheapest, highest-leverage fix in the
whole milestone (it directly targets the misdiagnosis Aura wrote into her own memory as
fact). For HARN-04, prefer an explicit-fact-identifier path over a pure count-threshold
refusal (Pitfall 6) — a guardrail that only checks match count still misses the
subject-mismatch variant of the same silent-data-loss symptom. MEM-04 and MEM-05 are bundled
here, not with the later Memory tiers phase, because they touch the identical entity/fact
upsert logic in `internal/arcadedb/memory.go` the F-2 guardrail touches, and because
entity-resolution correctness is a precondition for HARN-04's guarantee to mean anything — if
the subject can't be reliably identified, "exactly the fact it names" can't be reliably kept.
ACC-01/ACC-02 are established here, in the first phase, because every subsequent phase's
validation depends on this methodology already being lived practice, not policy text.
**Success Criteria** (what must be TRUE):
  1. In a live conversation, asking Aura to re-run the same mutating command twice in one turn (after changing the world in between) produces two distinct executions in `aura.tool_invocations`, not one recorded replay served twice.
  2. A genuinely retried dispatch (e.g., a scheduler restart reclaiming the same run) still shows exactly one real execution for that operation in `aura.tool_invocations` — no duplicated side effect.
  3. When a call is legitimately replayed, the tool result surfaced to the model carries a visible replay marker, observable in the transcript / OTel span attributes.
  4. Asking Aura to correct one fact among several sharing the same subject and predicate leaves the sibling facts valid — inspecting the ArcadeDB graph afterward shows only the named fact's validity window closed.
  5. Across this scenario, Aura's replies are in the operator's language, no raw deliberation leaks into user-facing text, and every stated intention either ran or the turn says plainly it didn't.
**Plans**: 8 plans in 5 waves
- [ ] 45-01-PLAN.md — BLOCKING D-08 amendments (ROADMAP §45/§46 + prd.md) and the two fix-on-touch doc corrections *(wave 1)*
- [ ] 45-02-PLAN.md — TRACER: `RoundOrdinal` in the child operation key, fail-closed, proved with SQL against `aura.tool_invocations` *(wave 2)*
- [ ] 45-03-PLAN.md — `replayedMarker` on both replay layers, OTel replay attributes, boot-time operation-metadata guard *(wave 3)*
- [ ] 45-04-PLAN.md — deterministic tool-call-id repair and same-message `(name, args)` dedup *(wave 3)*
- [ ] 45-05-PLAN.md — completion critic on every voluntary termination, veto budget 2, reply-discipline prompt rule *(wave 3)*
- [ ] 45-06-PLAN.md — `fact_key` exact-match supersede, ambiguity refusal, prose guard on the object endpoint *(wave 3)*
- [ ] 45-07-PLAN.md — MCP surface: `supersedes_fact_key`, refusal payload, operator-subject canonicalization *(wave 4)*
- [ ] 45-08-PLAN.md — live scenario scored >9.8, full gate matrix, quality-snapshot re-attestation *(wave 5)*

### Phase 46: MCP trust and facade
**Goal**: MCP servers Aura ships or bundles are trusted in the model's own context, without
losing the two independent guardrails (per-call result fencing, fail-closed risk
classification) that never depended on that trust in the first place; calendar and WhatsApp
collapse into a single always-loaded `comms` slot, replacing 28 raw tools.
**Depends on**: Phase 45 — narrowed 2026-08-13 (prd.md Amendment #121): `applyMCPOperationMetadata`
(`internal/agent/mcptools/bridge.go:230-240`) already fills `OperationScope`,
`OperationNormalizer` and `ReplayPolicy` unconditionally for every `Mutating` bridged tool with
the uniform default, and that default is correct — there is no vocabulary to wait for. The
remaining dependency on Phase 45 is the risk-override and hide-list work only.
**Requirements**: MCP-01, MCP-02, MCP-03, MCP-04, MCP-05, TOOL-14
**Rationale**: Trust-wrapper scoping and facade groundwork are the same code change
(generalizing `bridgePolicy` into a namespace-keyed table), so they land together
(ARCHITECTURE.md §2). **Operator decision, 2026-08-05:** wrapper removal is UNCONDITIONAL —
every mounted server, present and future, including ad hoc mounts and any MCP server Aura
mints through self-extension. This roadmap originally scoped removal to
`bridgePolicy.recipeSource != ""` (the three code-reviewed recipes) per Pitfall 2; the
operator was shown that recommendation explicitly and chose the blanket reading. The residual
prompt-injection risk is therefore carried entirely by MCP-02's two independent guardrails and
by the operator's control over what gets mounted — which makes verifying MCP-02 the
load-bearing task of this phase, not a checkbox beside it. Independent of the
context-ladder work (Phase 50) — no package overlap. Verify explicitly that `trust.go`'s
nonce-wrapped result-fencing and `mcpToolRisk`'s fail-closed classification are untouched by
this change (MCP-02) — they are a separate mechanism from the description wrapper and a
reviewer could otherwise assume removing one weakens the other.
**TOOL-14 lands here, before Phase 48 needs it.** Tool tiering is PRD-declared, not an
implementation detail: `prd.md:154` states the rule, each slice carries a "Deferred-tool
partition", and two named amendments fix specific tools' tiers — A4 makes `read_tool_output`
non-deferred (which TOOL-13 changes) and #44 makes `sandbox_exec` non-deferred with live
evidence. Phase 46 is the earliest consumer because MCP-04 changes which MCP tools exist at
all, so the amendment gates this phase and Phase 48 both.

**Also settle the MCP-trust question here, by reading rather than assuming.** A targeted PRD
search found the untrusted-data doctrine to be about RESULT fencing (`prd.md:1904`, `:2018` —
*"its returned content is still fenced as untrusted reference data"*), which MCP-02 preserves,
and found nothing establishing the description wrapper, which lives in `bridge.go` as an
implementation defense. On that reading MCP-01/MCP-03 need no amendment. The search was
targeted, not exhaustive: confirm it in this phase's discussion, and if the PRD does establish
description wrapping somewhere, add a second amendment before touching `bridge.go`.

**Success Criteria** (what must be TRUE):
  1. In a live conversation, the tool manifest / `tool_search` index shows calendar and WhatsApp reachable through a small curated action set, not the raw 28 underlying MCP tools.
  2. A calendar or WhatsApp mutating action (e.g., delete an event, send a message) still produces the fail-closed risk gate / approval flow live — the removal of the description wrapper has not weakened authorization.
  3. Reading the rendered tool descriptions in a live turn shows every mounted MCP server — bundled recipes and any ad hoc mount alike — presented as ordinary text, with no untrusted-data framing anywhere.
  5. With the description wrapper gone everywhere, a live turn whose MCP tool result carries instruction-shaped text — e.g. an inbound WhatsApp message written to read as a directive overriding Aura's standing orders — does not act on it, proving the result-fencing envelope rather than the description wrapper was what carried the defense.
  4. `accountId` never appears in the model's dispatched arguments for a live calendar/WhatsApp call — inspecting `aura.tool_invocations` shows it was host-injected, exactly like `user_identifier`.
**Plans**: TBD

### Phase 47: Tool-surface ceremony strip
**Goal**: The lowest-risk tool-surface debt is paid off: parameters the host already knows
stop being asked of the model, a withheld destructive action is resolved without the model
ever touching a resume payload, and a file the operator needed reaches them and becomes
searchable without a remembered follow-up action.
**Depends on**: Phase 45 — the `ask_user`/approval trim (TOOL-03) shares state with Phase 45's idempotency/reservation machinery (`internal/agent/idempotency_operation.go`, `internal/gateway/reserve.go`); sequencing after Phase 45 avoids two uncoordinated changes racing on the same state machine.
**Requirements**: TOOL-02, TOOL-03, TOOL-08, TOOL-09, TOOL-10, AUTO-01, AUTO-02, SURF-02, SURF-03, COMPAT-02
**Rationale**: Lower blast radius than Phase 48 — dropping declared-but-host-overwritten
parameters, host-mediating approval, and merging `document_index`+`document_describe` are
well-evidenced, low-risk changes (FEATURES.md). AUTO-01 (automatic delivery) and AUTO-02
(automatic findability) piggyback naturally on the same document-tooling change TOOL-08
touches. SURF-02 (per-tool operational rules live in the tool's own description) and SURF-03
(undelivered/unindexed files surfaced next to the turn) are text/legibility changes in the
same vein, not schema renames, so they belong in the lower-risk phase. COMPAT-02 protects
specifically the `ask_user` shape change this phase makes: a pause created under the old
shape must resume correctly or fail loudly, never silently (Pitfall 4).
**Success Criteria** (what must be TRUE):
  1. Asking Aura to produce a file for the operator results in it reaching their channel (Telegram/web) automatically in that same turn, without Aura calling a separate "send" action.
  2. That same file is already findable via `document_search`/`document_open` on a following turn, without Aura having called a separate indexing action.
  3. A live destructive action withheld for approval, once approved, resumes and completes without the model ever being shown or asked to reproduce a resume/fingerprint payload.
  4. A live `web_fetch` against a known bot-blocked or consent-wall URL is reported to the model as a failed read in the transcript, never handed back as if it were page content.
  5. A conversation paused mid-approval under the pre-Phase-47 `ask_user` shape (seeded before this phase lands) either resumes correctly or fails with an explicit, actionable message — never silently.
**Plans**: TBD

### Phase 48: Tool-surface un-defer and merges
**Goal**: The model's manifest lands on exactly **14** loaded tools (TOOL-01 names them) and the
system prompt is regenerated to match exactly what's loaded — the manifest the model reasons over each turn
becomes something it can actually hold in its head, safe against conversations and schedules
that predate the change.
**Depends on**: Phase 45 (ReplayPolicy vocabulary for newly merged/un-deferred specs), Phase 46 (the facade must collapse to the single `comms` slot first, since that slot is one of the 14 this phase sizes against), Phase 47 (shares the same native tool files; landing ceremony-strip first avoids double-touching them, and settles the ask_user/approval state before further tool-registry churn).
**Requirements**: TOOL-01, TOOL-04, TOOL-06, TOOL-07, TOOL-11, TOOL-12, SURF-01, SURF-06, SURF-07, SURF-08, COMPAT-01, COMPAT-03, AUTO-04
**Rationale**: Highest surface area of the milestone — every native tool file,
`llm_agent_promote.go`'s promotion machinery, the registry boot-guard (ARCHITECTURE.md §4).
Pitfall 1 recommends restoring a tool-choice-accuracy eval harness before un-deferring;
the operator's ACC-02 decision (no new eval harness — evidence comes from OTel/
`aura.tool_invocations`/`aura.conversation_turns`/`aura.context_rot_events`) supersedes that
recommendation. Substitute: verify tool-choice accuracy by running the SAME real scenarios
live, before and after the flatten, and comparing tool selection in `aura.tool_invocations` —
a live before/after comparison, not a new automated harness. `TOOL-05` (memory_recall
unification) deliberately does NOT ship here alongside its `task`/`skill` flatten siblings —
its one-call shape is meaningless until Phase 49's short-term+long-term unified retrieval
exists to back it, so it ships there instead. SURF-01 (system prompt regeneration) is
inside this phase, not after it — the prompt's own governing rule is "the prompt names a
tool only if that tool is loaded," so un-deferring without regenerating in the same phase
ships a wrong prompt. COMPAT-01 and COMPAT-03 protect specifically the renames/merges this
phase makes: test against a REHYDRATED pre-flatten conversation fixture, not just a fresh
one, and keep renamed tools rejecting with a clear message for at least one deploy cycle
(Pitfall 4).
**`tool_search` is infrastructure and stays loaded** — hermes states the rule outright:
*"Core tools are never deferred. Always-load means always-load. No exceptions."* Without it the
deferred tail is unreachable, so the 14 domain tools are really 15 loaded. SURF-08 puts the
deferred roster's names and its three anti-failure sentences where the decision happens; TOOL-12
lets a known name skip the search step. **The three-tool bridge (`tool_search`/`tool_describe`/
`tool_call`) was evaluated and deliberately deferred to TOOL-V2-03** with a written trigger — it
would insert an unwrapping step into the gateway's name-keyed risk classification and the
idempotency key, which are the two paths Phase 45 is fixing, and its payoff scales with a catalog
Aura does not have.

**Success Criteria** (what must be TRUE):
  1. A fresh conversation's system prompt names exactly the tools actually loaded that turn — no phantom name for a deferred/unloaded tool, verified by reading the rendered prompt.
  2. Scheduling a task in a live conversation with a natural-language `when` ("next Tuesday at 9am") succeeds in one call, with no mutually exclusive time fields required.
  3. Applying a skill happens in one call in a live turn — no separate "view" step before "apply" — while creating/updating a skill still works through the separate lifecycle action.
  4. A conversation rehydrated from turns recorded against the pre-flatten tool schema (seeded before this phase) still produces a valid request on its next live turn — no broken wire message, no crash.
  5. A scheduled `agent_job` created before this phase still fires and resolves its tools against the current, post-flatten registry, verified via a live scheduler run.
**Plans**: TBD

### Phase 49: Memory tiers
**Goal**: Aura's memory grows a searchable short-term tier and a reasoning tier that only
enters context on demand — gated by a PRD amendment committed before any of the
reasoning-tier code, so the boundary between scratch-work and durable fact is a decision on
record, not an implementation detail.
**Depends on**: Phase 45 (the entity-resolution baseline MEM-04/05 already fixed in the same `internal/arcadedb/memory.go` code), Phase 46 (the `bridgePolicy` generalization this phase's new short-term/reasoning-tier tools reuse for hide-list/risk/replay classification).
**Requirements**: MEM-01, MEM-02, MEM-03, MEM-06, TOOL-05, AUTO-03, CTX-05, HARN-05
**Rationale**: MEM-06 gates MEM-03 — the PRD amendment extending amendment #91 (reasoning
persisted to the graph, retrieved only on demand, never summarized or harvested) is its own
committed step, landing before any commit that touches reasoning-tier implementation, per
CLAUDE.md's PRD-amendment-before-code rule. A short-term memory tier in ArcadeDB is settled,
operator-decided scope regardless of Phase 53's spike outcome — Postgres stays the system of
record for turns, ArcadeDB gets a derived, searchable projection (MEM-01). TOOL-05 lands here
rather than with its flatten siblings in Phase 48 because it's the model-facing shape of
this exact mechanism (MEM-02's unified retrieval call). HARN-05 (atomic multi-op memory
write) lands here rather than in Phase 48 because it needs the same ArcadeDB memory-bridge
atomic-commit path this phase's other work touches — net-new transactional semantics, not a
schema widening. AUTO-03 and CTX-05 are the same boundary this phase must enforce now that
a reasoning tier exists: a durable fact is captured as part of doing the work, and reasoning
content never reaches a summarizer or fact extraction — hermes' own failure mode (a
speculative reasoning conclusion preserved as a fact) is one Aura's own audited session
already exhibited once.
**Success Criteria** (what must be TRUE):
  1. `git log` shows the PRD amendment extending #91 committed as its own commit, dated before any commit touching the reasoning-tier implementation.
  2. A live question about something said several turns back — past what the deterministic ladder still holds in context — is answered correctly via one `memory_recall` call spanning recent conversation and long-term facts; the tool result / OTel span shows which retrieval path (graph traversal or hybrid search) was actually used.
  3. After a turn involving extended reasoning, the ArcadeDB graph shows the reasoning trace persisted with edges to the entities it touched, and a later turn's injected context does NOT include that reasoning content unless explicitly retrieved.
  4. A durable fact revealed mid-task (stated during a live shell/file task) is captured as a memory fact by the time the task completes — checking its recorded provenance shows it was captured directly, never sourced from a reasoning-trace summarizer.
**Plans**: TBD

### Phase 50: Context ladder legibility
**Goal**: The context ladder's existing deterministic machinery becomes legible and
accurate — the eviction/budget decisions the model is already subject to are now visible to
the operator and correct against the provider's real token count.
**Depends on**: Nothing structurally (zero package overlap with Phases 45-49: touches only `internal/conversations` + `internal/runner`); sequenced after Phase 48 so its real-token budget (CTX-01) tunes against the FINAL tool-surface shape rather than one still being renumbered by the un-defer/merge work.
**Requirements**: CTX-01, CTX-02, CTX-03, CTX-04, CTX-07, CTX-08, TOOL-13, SURF-04
**Rationale**: `internal/conversations/context.go` is at 590/600 LOC — new work lands in
sibling files (`context_summary.go`/`context_breakdown.go`), never appended to it. The
cheapest, highest-value item in the whole milestone lives here: the real `prompt_tokens`
count is already persisted every turn (`AppendTurnParams.InputTokens`) and already queryable
— it's used today only for the cockpit's display gauge, never fed into the ladder's own
budget decision (CTX-01). Order matters within this phase (CONTEXT-MANAGEMENT.md's own
stated order): evict superseded `tool_search` results first (CTX-02), then wire the real
token budget (CTX-01), then the ghost-skill marker (CTX-03) — about 30 lines, independent of
any LLM rung — then the per-category breakdown (CTX-04, a new sibling file, categories
reflecting Aura's actual manifest shape, not hermes' 8 categories copied verbatim). SURF-04
(model can tell memory was injected, dropped tool still callable) is bundled here as the
same class of legibility signal as the ghost-skill marker. CTX-07 (stated reason when
context can't be reduced) closes the ladder's failure-mode legibility gap.
**Success Criteria** (what must be TRUE):
  1. In a live long conversation, the context-budget trigger visibly uses the real reported `prompt_tokens` from the last provider response (inspectable via the ladder's logged trigger value / `aura.context_rot_events`), not a tiktoken estimate.
  2. A `tool_search` result whose schema was reloaded later is evicted from context on a subsequent turn — verified by inspecting what's actually sent on the wire that turn.
  3. A skill pruned from context by the ladder leaves a visible marker in a later turn, and Aura reloads it via `skill` rather than acting as if it's still loaded.
  4. Asking the operator-facing diagnostic (or the operator inspecting a live turn) what's consuming the context window shows a breakdown by category — tools, memory, skills, history — not just a single fullness percentage.
  5. When context is over threshold and cannot be reduced further, the turn states the reason rather than failing silently or truncating without explanation.
**Plans**: TBD

### Phase 51: Durable delegation
**Goal**: A delegated worker gets a brief worth acting on and limits it can see, a worker can
orchestrate workers of its own, and a top-level delegation stops holding the operator's turn
hostage — results re-enter the conversation when the work is actually done.
**Depends on**: Phase 48 (the worker registry is the parent's minus the delegation tool, so the flattened surface must be settled before workers are verified against it — SWARM-08), Phase 49 (SWARM-07 needs the memory and reasoning tiers to exist before deciding what concurrent workers may write into them), Phase 47 (SWARM-06's relay rides the `ask_user` shape that phase reworks).
**Requirements**: SWARM-01, SWARM-02, SWARM-03, SWARM-04, SWARM-05, SWARM-06, SWARM-07, SWARM-08, SWARM-09, SWARM-10, SWARM-11
**Rationale**: Operator decision, 2026-08-05, taken after reading hermes' `delegate_task`
against Aura's `swarm_spawn`.

**Do not design the durable half — it is already designed and approved.**
`docs/superpowers/specs/2026-06-29-durable-swarm-messaging-design.md` (551 lines) specifies a
Postgres-first substrate: claimable tasks, short leases (1m — crash-recovery latency, not task
duration) extended by a heartbeat, fencing on `attempt_count` + `locked_by` so a zombie worker
whose lease expired matches zero rows and gets a typed `ErrLeaseLost`, at-least-once delivery
with idempotency keys, transient-vs-permanent retry with exponential backoff and full jitter,
and an A2A lifecycle in which `waiting_input` is a non-terminal pause woken transactionally by
the arriving reply. It was approved and never built. SWARM-03's background delegation is that
substrate's first consumer (SWARM-09), not a second mechanism beside it — the same
inventory-before-invention call that killed `make_document`. SWARM-11 lands its PRD amendment
first. SWARM-10 (a tail-able live transcript per child) is hermes' answer to waiting blind for
a consolidated report, and is what makes a backgrounded delegation observable at all. Three of these are cheap surface fixes against a defect Aura
already documents in her own tool description (*"the worker cannot see the conversation, the
user, the other workers, or anything outside the goal text you give it"*): SWARM-01 splits
`goal` from `context`, SWARM-02 rebuilds the schema per manifest render so the model reads the
operator's real `AURA_SWARM_*` caps instead of discovering them by failing, and SWARM-06 keeps
the child-question relay alive through Phase 47's approval rework.

SWARM-03/04 are the substantial change and are NOT a surface clean: today the parent blocks
until every worker reports. Hermes makes top-level delegation background-by-default with the
flag deprecated so the model cannot opt out, and keeps a depth>0 orchestrator synchronous
because it needs its workers inside its own turn. **Inventory before invention applies hard
here** — Aura already has machinery for "work happens later and reports back": the scheduler's
`agent_job` runs a fresh agent turn at fire time, and AG-UI carries run-detach with
Last-Event-ID resume (Amendment #90). Establish whether background delegation is a new
execution path or a second caller of those before designing one.

SWARM-05 opens nesting the PRD already designed (Slice 3's 2-deep cap) but the implementation
forecloses by handing workers `Without(reg, "swarm_spawn")`. SWARM-07 is the one nobody asked
for and everybody needs: AUTO-03 fires fact-capture inside every worker, so N concurrent
workers writing one identity's graph is a concurrency surface that does not exist today and
lands squarely on the memory correctness Phase 45 and 49 just established.
**Success Criteria** (what must be TRUE):
  1. Delegating in a live conversation returns Aura's turn immediately — the operator can keep talking, and the consolidated worker result arrives in the conversation when the work finishes, observable in `aura.conversation_turns`.
  2. A worker that itself delegates receives its own workers' results within its turn — its delegation does not return early, verified in a live nested run.
  3. A live worker brief carries its context separately from its goal, and the rendered tool schema shows the operator's configured concurrency and depth caps, not framework defaults.
  4. A worker that needs the operator surfaces the question in the operator's channel, naming which worker raised it, and answering it resumes that worker's line of work.
  5. After a live fan-out where several workers each learn something durable, the graph holds one correctly-attributed fact per worker — no duplicates, no lost writes, no fact attributed to the parent.
**Plans**: TBD

### Phase 52: Mid-turn steering
**Goal**: The operator can type into a running turn and have it land — redirecting work at
the next round boundary instead of waiting for the turn to end or killing it and starting
over.
**Depends on**: Phase 51 — the steering design reconciles with the durable swarm substrate in its own §8, and both touch the same run-identity and pause/resume machinery; building the substrate first means steering has one addressable run model to attach to rather than two.
**Requirements**: STEER-01, STEER-02, STEER-03, STEER-04, STEER-05, STEER-06
**Rationale**: Operator decision, 2026-08-05. The design study already exists
(`docs/superpowers/specs/2026-07-23-mid-turn-steering-design.md`, 664 lines, its three
operator-level open questions already resolved to "Claude Code parity") and is explicitly
marked STUDY ONLY — no code, no amendment. STEER-06 lands that amendment first; §11 of the
study is already an amendment checklist.

The study's own finding is why this is a phase and not an epic: **the seam is unusually
clean.** The agent loop already injects user-role messages mid-run on three established
paths — recovery nudge, empty-response nudge, completion-gate feedback
(`llm_agent.go:331`, `:439`, `llm_agent_finalize.go`) — so a steer is a fourth,
operator-sourced instance of an existing in-loop pattern rather than a new message
discipline. The runner persists turns incrementally per round
(`runner_persist.go:187-204`, `:144-151`), so a steer appended at the drain point lands at
the right `seq` with no in-flight-assistant-row conflict. And Amendment #90's RunRegistry
already gives every live run an owner-scoped addressable identity plus a replay ring, which
makes STEER-03's resume-exact echo close to free.

Today the composer is dead while the agent runs: the thread lock returns 409 `ErrThreadBusy`
and the client blocks send while `live_run_id` is set. STEER-02 is the guardrail that keeps
this from becoming an escape hatch — a steer redirects the work, it does not buy more budget.
STEER-04 exists because the failure mode of any queue-into-a-running-thing is silent loss.
**Success Criteria** (what must be TRUE):
  1. Typing a redirect while Aura is mid-task changes what she does next, live — observable as her next round acting on the new instruction, with no tool killed mid-execution.
  2. That steer appears in the persisted conversation at the point it actually landed — reloading the thread or resuming the run shows it in the right place, not appended at the end.
  3. Steering a run that has just finished returns the message to the operator to send normally — it is never silently swallowed.
  4. A steered turn consumes no more steps or wallclock than an unsteered one — the budget is unchanged by steering.
  5. The same steer works from a channel, not only the cockpit.
**Plans**: TBD

### Phase 53: Summarization spike
**Goal**: The milestone has an evidenced answer, not a guess, to whether an LLM
summarization rung is worth building on top of the short-term retrieval tier Phase 49
shipped.
**Depends on**: Phase 49 — needs the short-term retrieval mechanism (MEM-01/MEM-02) to exist as the "retrieval" arm being measured against a prototyped summarization arm.
**Requirements**: CTX-06
**Rationale**: This is a research/measurement phase, not an implementation phase — its own
output determines whether a follow-on phase for CTX-V2-01 (LLM summarization with hermes'
anti-thrash, cooldown, and fallback machinery) gets scheduled at all. The spike measures, on
the real exported 234+10-turn audit corpus already in hand
(`docs/audit/live-conversations-2026-08-04/`), whether retrieval over indexed history
recovers what the deterministic ladder drops, against summarization, and against both —
using a "known-correct answer" methodology this phase must define first, since none exists
yet. **This phase's result is not assumed.** If it selects the summarization arm (in whole
or in part), a follow-on phase must be added (an integer phase after 54, or inserted as a
decimal) carrying the design ARCHITECTURE.md §3 already prepared (a sibling
`context_summary.go`, a `PriorSummary` field, Runner-owned two-pass orchestration, a
dedicated second `llm.Breaker`) plus hermes' anti-thrash/cooldown/fallback guards budgeted
into that same phase, not a follow-up (Pitfall 3). If it selects retrieval alone, no further
phase is needed for CTX-V2-01 and it stays deferred. That decision is NOT made by this
roadmap — it is this phase's deliverable.
**Success Criteria** (what must be TRUE):
  1. The spike runs against the real audit corpus with a defined "known-correct answer" methodology (documented before scoring), not an assumed or informal one.
  2. The spike produces a written, evidenced decision — with measured recall/continuity numbers per arm — on whether retrieval, summarization, or both best recover what the ladder drops.
  3. The decision explicitly states whether CTX-V2-01 is promoted into a future phase, backed by the measured numbers, not asserted without evidence.
**Plans**: TBD

### Phase 54: Milestone exit
**Goal**: The nine `learned_lesson` facts and the `always-deliver-files` skill — Aura's own
bug report on herself — are retired, and a live replay of the audited scenarios proves the
defects they compensated for are actually gone, not just believed gone.
**Depends on**: Phases 45-53 — this is the milestone exit gate; retiring the compensating lessons presupposes the defects they compensate for are fixed, and the replay validates everything shipped, not one phase in isolation.
**Requirements**: SURF-05, ACC-03
**Rationale**: This is the milestone's own stated finish line (PROJECT.md, REQUIREMENTS.md
organising insight): the milestone is done when Aura's memory no longer needs to hold
workarounds for a defective surface. SURF-05 (retire the lessons/skill) and ACC-03 (validate
the retirement holds under replay) are the same closing action — the deletion is a
precondition of the validation, not a separate cleanup step. ACC-01/ACC-02's policy
(established in Phase 45) governs this phase too: evidence is a live replay against OTel and
the four named signals, never a green test suite.
**Success Criteria** (what must be TRUE):
  1. The nine `learned_lesson` facts and the `always-deliver-files` skill are deleted from Aura's live memory/skill store — confirmed by querying ArcadeDB and the skills directory directly.
  2. Replaying the audited 2026-08-04 scenarios (or an equivalent live re-run of the same tasks) against the current stack produces correct tool choice on the first attempt, automatic file delivery, and successful self-retrieval of what she needed — verified via `aura.tool_invocations`/`aura.conversation_turns`.
  3. Across a fresh live run of those scenarios, Aura does not re-learn or re-write any of the nine retired lessons back into memory — confirmed by inspecting ArcadeDB after the run.
**Plans**: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 45 → 46 → 47 → 48 → 49 → 50 → 51 → 52 → 53 → 54
(Phase 50 has no hard dependency on 45-49 and could run in parallel if desired; kept serial
and last-among-technical-phases here so its token budget tunes against the final manifest.)

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 45. Harness correctness | 0/TBD | Not started | - |
| 46. MCP trust and facade | 0/TBD | Not started | - |
| 47. Tool-surface ceremony strip | 0/TBD | Not started | - |
| 48. Tool-surface un-defer and merges | 0/TBD | Not started | - |
| 49. Memory tiers | 0/TBD | Not started | - |
| 50. Context ladder legibility | 0/TBD | Not started | - |
| 51. Durable delegation | 0/TBD | Not started | - |
| 52. Mid-turn steering | 0/TBD | Not started | - |
| 53. Summarization spike | 0/TBD | Not started | - |
| 54. Milestone exit | 0/TBD | Not started | - |

## Notes on conditional scope

Phase 53's spike may promote `CTX-V2-01` (LLM summarization rung) from the v2 deferred list
into a scheduled phase — this roadmap deliberately does NOT pre-schedule that phase, since
its design, dependency set, and even which package it touches depend entirely on the spike's
still-unknown outcome (SUMMARY.md's own research flag: "cannot be planned in detail until the
spike concludes"). If promoted, add it as Phase 55 (or a decimal insertion if urgent) via
`/gsd-phase`, carrying ARCHITECTURE.md §3's prepared design and Pitfall 3's mandatory
anti-thrash/cooldown/fallback guards in the same phase, not a follow-up.
