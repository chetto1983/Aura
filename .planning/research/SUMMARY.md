# Project Research Summary

**Project:** Aura — v2.1.0 HERMES-CLAUDE_PARITY (phases numbered from 45)
**Domain:** Agent-harness correctness + tool-surface + context-management hardening on an
existing ~98k-LOC self-hosted Go AI-agent platform
**Researched:** 2026-08-05
**Confidence:** HIGH overall — every one of the four research files grounds its claims in
file:line reads of Aura's own codebase, the on-disk `hermes-agent` reference implementation, or
(for OpenRouter behavior) live-fetched vendor docs. The two places confidence drops to
MEDIUM/LOW are named explicitly below and are not load-bearing for the recommended build order.

## Executive Summary

This is not a "what technology should we use" milestone — all four researchers converge on the
same headline: **zero new Go dependencies are needed anywhere in this milestone.** Every
mechanism this work requires (cooldown-after-failure, offline-testable LLM calls, accurate token
accounting, idempotency-scoped mutation guards) either already exists in Aura in a smaller,
already-tested form, or is a pure aggregation/wiring problem over data structures the codebase
already has. STACK.md's headline finding is the sharpest example: the accurate `prompt_tokens`
count the context ladder needs is **already persisted every turn** and already queryable
(`internal/db/queries/conversations.sql:17-30`, `internal/conversations/store.go:172`) — it is
used today only for the cockpit's display gauge and never fed into the ladder's own budget
decision (STACK.md Q2, citing the audit's own C-4: *"Aura ha il dato e non lo usa per
decidere"*). This is the single cheapest, highest-value item in the whole milestone: wiring one
existing field into one existing struct, no migration, no library.

The recommended approach is to build in the order ARCHITECTURE.md derives from tracing the
actual code paths, not from the milestone's own feature list ordering: fix the idempotency
replay defect (F-1) first because every later phase creates or edits tool specs that need to
declare the *correct* `ReplayPolicy` from day one; generalize the MCP trust/facade policy second
because it needs F-1's new `ReplayPolicy` vocabulary and because trust-scoping and the
calendar/WhatsApp facade are the same code change; flatten the tool surface third because it
needs both prior steps settled before its ~26-tool budget can be sized correctly; and treat the
context-ladder work as a parallel track with no package overlap, landing its cheap deterministic
wins before any LLM-dependent rung is attempted.

The key risk, surfaced independently by both PITFALLS.md and the operator's own post-briefing
decisions, is that two of this milestone's most consequential changes are being asked to ship
without the evidence base that would let anyone tell if they worked: the tool-choice-accuracy
harness that would prove un-deferring 14 tools doesn't quietly make the model worse at picking
between them was deleted (`internal/eval/`, per `.planning/codebase/CONCERNS.md`), and the
summarization rung (C-1) is **not a settled design** — it is gated behind a spike that must
measure, on real exported conversation data with known-correct answers, whether a searchable
ArcadeDB short-term tier recovers what the deterministic ladder drops before any LLM-summarization
machinery is built. Every phase in this milestone should be read against the operator's blanket
policy: a green test suite is not evidence of completion; only a live run against a known-correct
outcome on the running stack is.

## Key Findings

### Recommended Stack

No new core technology anywhere in this milestone (STACK.md, full file). The three
context-management capabilities (LLM summarization rung, accurate token accounting, per-category
breakdown) land entirely on the existing stack:

**Core technologies (all already pinned, unchanged):**
- `github.com/pkoukk/tiktoken-go` v0.1.8 — stays for RELATIVE sizing only (how many pairs to
  drop); the REAL number now comes from the provider response, making it a "~5-10% approximation"
  tool for gating decisions only, never the source of truth for the trigger (STACK.md Q2).
- `jackc/pgx/v5` + `golang-migrate/migrate/v4` + `sqlc` v1.31.1 — the whole persistence stack,
  needed only for one small optional migration (anti-thrash streak columns), not for anything
  else in this milestone.
- `internal/llm.Client` interface (`internal/llm/client.go:89`) — the one wire abstraction every
  new out-of-band LLM call (title generation today, summarization tomorrow) goes through.

**Reused, not added — the mechanisms hermes built as libraries, Aura already has hand-rolled and
tested:**
- `internal/llm.Breaker` (`internal/llm/breaker.go:1-83`) — mint a SECOND, dedicated instance for
  the summarizer call path; do not share the main-model breaker (unrelated failure populations).
- `*openai_compat.HTTPError` + the `errors.As` classifier idiom
  (`internal/agent/llm_agent_stream_retry.go:83-118`) — the literal 429/5xx detection logic to
  mirror for the summarizer, not reinvent.
- The `titleTestClient` hand-rolled fake pattern (`internal/conversations/title_unit_test.go:16-59`)
  — the ONLY pattern in the codebase for offline-testing an `llm.Client` caller; a hard requirement
  to reuse for the summarizer, not a suggestion.
- `AppendTurnParams.InputTokens` / `GetConversationLastInputTokens` — already-persisted,
  already-queried real token counts; the fix is wiring, described above.

**Explicitly rejected as unnecessary:** promoting `sony/gobreaker`, `cenkalti/backoff/*`, or
`golang.org/x/time/rate` from indirect to direct dependency — all three are dependency-graph
noise with zero first-party callers today, and each solves a problem Aura already hand-rolls in a
smaller, already-tested shape (STACK.md "What NOT to Use").

**File-size constraint that shapes where new code lands:** `internal/conversations/context.go`
(590/600 LOC) and `store.go` (574/600 LOC) are both at the 600-LOC ceiling — new work goes in
sibling files (`context_summary.go`, `context_breakdown.go`, `store_context_summary.go`), never
appended to these two.

### Expected Features

FEATURES.md's method was to compare Aura's audited tool surface against 6 independent vendor
reference tool sets (Claude Code, Amp, Cursor, Windsurf, Warp.dev, Devin AI) plus hermes-agent —
HIGH confidence for the cross-verified patterns below, MEDIUM confidence for the exact
param-count targets (one engineer's proposal in `TOOL-SIMPLIFICATION.md`, checked against the
reference set rather than re-derived from scratch).

**Must-have (table stakes, cross-vendor consensus):**
- Host-injected identity/tenancy parameters, never model-facing — Aura's own
  `withMemoryUserIdentifier` (`internal/agent/mcptools/bridge_memory.go:45-58`) already enforces
  this at the handler layer; the bug is that 4 schemas still *declare* `user_identifier` required.
- Host-mediated approval where the model never carries a resume token — no reference vendor makes
  the model relay approval state; Aura's current `ask_user`-relayed `resume_context` is the
  ceremony this milestone should remove.
- Narrow, single-purpose tools for high-frequency primitives (read/write/edit/search/ask) — 5/5
  vendors keep file-name search and content search as two separate tools.
- Collapse mutually-exclusive parameter clusters into one parsed field (hermes' `cronjob.schedule`
  collapsing 4 fields into 1 is the applicable precedent for Aura's `task.when`).
- Separate "use" from "manage" for any capability with an authoring surface (`skill`/`skill_manage`
  split) — no reference vendor mixes capability-invocation with capability-authoring in one tool.

**Should-have (Aura-specific differentiators):**
- Atomic batch memory write (`memory_write{operations[]}`) — the strongest evidence in the whole
  research pass that a batch shape prevents a *class* of error, not just a round trip: it makes
  F-2's data-loss shape (`supersedes:true` closing 8 facts because the model had no way to name
  "exactly this one") structurally impossible, not merely rare.
- Curated `calendar{action:...}`/`whatsapp{action:...}` facade over 28 raw MCP tools — the biggest
  single context win in the whole milestone (~4.5k tokens at one turn alone per the audit), and a
  genuinely Aura-specific integration-shape problem no reference vendor solves directly.
- The 10-slot deferred-tool promotion cap (`maxPromotedDeferredTools=10`,
  `internal/agent/llm_agent_promote.go:33`) as a design constraint feeding merge decisions — a
  legitimate, Aura-specific reason to weigh tool merges that reference vendors never need to
  consider.

**Defer / do not build (evidenced against, not just deprioritized):**
- **Merging `fs_glob`+`fs_grep` into one `fs_search{target:...}`** — `TOOL-SIMPLIFICATION.md` §B1
  proposes this citing hermes as precedent. **This is a real disagreement in the research, not a
  split-the-difference call, and FEATURES.md's evidence should be carried forward as-is:** every
  reference vendor except hermes (Claude Code, Amp, Cursor, Windsurf, Warp.dev — 5 for 5) keeps
  filename search and content search as two separate, narrow tools, because the tool *name itself*
  is a zero-cost disambiguator the model's training data already binds (`grep`→content,
  `glob`/`find`→filenames). Hermes' `target: content|files` enum moves that same decision into a
  *parameter value* competing with seven sibling parameters, and a wrong `target` choice produces
  a silent empty result rather than a diagnosable wrong-tool error. Aura does have one real,
  vendor-absent reason to still want the merge — the 10-slot promotion cap, which no split-tool
  vendor carries — but FEATURES.md's recommendation is explicit: **keep them separate; revisit
  only if the cap is shown to bind in practice (both tools evicting something useful within one
  turn), instrumented first, not merged on "Hermes does it" alone.** Complexity to reverse if
  wrong: LOW; cost of being wrong: HIGH (reintroduces an F-1-shaped silent-mismatch class into the
  highest-call-volume tool pair in the harness).
- A generic `make_document` router tool — root cause was F-1 (stale replay), not a missing router;
  no reference vendor has a generic "pick the right tool for me" meta-tool for single-call routing.
- "Reduce action count" as a blanket goal for every multiplexed tool — undercounts what actually
  causes confusion; hermes' `cronjob` has *more* top-level params than Aura's `task` and is not a
  problem because none of its fields are mutually exclusive. Split by call-frequency tier
  (frequent vs. administrative), not by raw parameter count.

### Architecture Approach

ARCHITECTURE.md is an integration-point map, not an ecosystem survey; every claim traces to a
named file:line in Aura's own codebase, with zero training-data assumptions about how
idempotency/MCP/context-management usually work. Four findings anchor the build order:

1. Two independent anti-duplication layers exist; only one is broken. Layer A (the
   per-dispatch reservation ledger, `internal/gateway/reserve.go`, keyed on
   ConversationID+RequestID+ToolCallID) protects against the same wire dispatch being processed
   twice and is untouched by F-1. Layer B (the idempotency operation registry,
   `internal/agent/idempotency_operation.go:32-46`) deliberately excludes tool_call_id from its
   key; that exclusion is load-bearing for CLI and scheduler cross-restart retries, but it is
   also exactly what collapses two different, deliberate calls made within one continuous
   LlmAgent.Run() onto the same stale result. The fix ARCHITECTURE.md recommends is a per-spec
   ReplayPolicy value (ReplayReissueExecutes) for tools whose real-world effect depends on
   externally-mutable state (shell_exec, fs_write, fs_edit, task run_now,
   memory_forget, skill create/update, swarm_spawn, and every MCP-bridged mutating tool),
   leaving the derived key itself unchanged, NOT widening the key universally, which would
   reopen the exact double-apply failure mode reserve.go:233-246's comment documents fixing.
2. The tool facade is a curation problem on an integration that already exists. Calendar and
   WhatsApp are already mounted as generic MCP recipes through the same mcptools.Bridge path
   memory uses (`internal/mcp/manager/catalog.go:112-173`). bridge_memory.go (69 LOC) is
   already a working proof that narrowing a raw multi-tool MCP surface to what the model should
   see has been solved once; the recommendation is to generalize bridgePolicy into a
   namespace-keyed table (hide-list, risk override, ReplayPolicy override per namespace), not to
   build a new package or hand-authored delegating tools.
3. The summarization rung attaches as a sibling file, not inside the ladder.
   applyContextLadder is documented as a pure function with no LLM call made
   (`context.go:288-350`); the precedent to copy is title.go's
   GenerateTitle(ctx, client, model, history) shape: a stateless, injected-client function in a
   NEW file, with the Runner (not the conversations package) owning goroutine/timeout lifecycle,
   and a two-pass Runner-level orchestration (loadTurnHistory, then on
   ErrContextWindowExceeded, Summarize, then retry once more) that adds zero cost on the common
   case.
4. Build order, with the dependency reason for each edge (ARCHITECTURE.md section 4):
   - F-1 fix first: foundational; touches only tools.Spec, idempotency_operation.go,
     reserve.go; every subsequent phase creates/edits tool specs that need to declare the
     right ReplayPolicy from day one rather than retrofitting it later.
   - MCP bridge/facade second: applyMCPOperationMetadata needs the new ReplayPolicy
     vocabulary from step 1 to assign anything other than the uniform default to a bridged tool;
     doing this right after step 1 leaves exactly one remaining metadata-assignment site (native
     tool files) to audit, not two moving targets. Independent of the context-ladder work.
   - Tool-surface flatten third: every touched/merged spec needs the correct
     Mutating/ReplayPolicy/OperationScope/Multiplexed combination from step 1, and the
     facade shape from step 2 must be settled first since facade tools count toward the ~26-tool
     budget this step sizes against.
   - Context ladder fourth (or parallel): zero package overlap with 1-3
     (internal/conversations + internal/runner only); if sequenced serially, doing it last
     avoids the tool-surface renumbering in step 3 disturbing the per-turn token counts C-4's
     real-prompt_tokens budget measures against.
   - F-2 (supersedes guardrail) is not scoped by the architecture document as its own step; it
     is an arcadedb-mcp tool-design problem mounted through the same MCP bridge namespace
     policy step 2 touches, and is a reasonable candidate to bundle into that phase rather than
     schedule separately.

### Critical Pitfalls

PITFALLS.md's six pitfalls map directly onto the build order above; the top ones for the
roadmapper to weight heavily:

1. Un-deferring tools trades a known, measured cost for an unmeasured one. Anthropic's own
   docs (fetched, HIGH confidence) put the tool-selection-accuracy knee at 30-50 tools; less
   rigorous industry write-ups (MEDIUM/LOW confidence) report a noticeable drop starting around
   15-20 in active rotation. Aura's post-flatten target of ~26 sits inside that lower band, not
   comfortably below it, and the harness that would measure a regression, internal/eval/, is
   deleted. Restoring or rebuilding a minimal tool-choice-accuracy harness must be an explicit
   prerequisite task of the flatten phase, not an afterthought.
2. Removing the untrusted-MCP wrapper globally is broader than the milestone's own stated goal
   requires. This is schema-description framing only; result-level trust fencing
   (internal/agent/trust.go's nonce envelope) and risk classification
   (mcpToolRisk's hardcoded, fail-closed-by-default allowlist) are two separate, untouched
   mechanisms. The evidenced recommendation is to scope the wrapper removal to
   bridgePolicy.recipeSource != "" (the three known, code-reviewed recipes), not to every
   mcptools.Bridge call, leaving a future ad-hoc or self-extension-minted MCP mount wrapped
   until it earns recipe status.
3. Adding LLM summarization without hermes' failure-mode machinery reproduces bugs hermes
   already paid ~11k LOC to fix, one at a time, each cited to a real issue number: thrashing on
   low-savings passes (#14694), freeze loops after 429s (#11529), orphaned tool_call/tool_result
   pairs corrupting the wire contract, the ghost-skill failure (a pruned skill body the model
   still believes is loaded, #32106, and Aura's own C-2 finding shows the IDENTICAL exposure
   already unresolved for tool_search results but not skill bodies), and alternation bugs that
   poison the STORED transcript permanently (Aura persists every turn; a summarization bug here
   is not transient, it is a brick). Aura's structural advantage over hermes: a proven
   deterministic fallback (dropOldestPairs) already exists; the LLM rung must degrade to it on
   ANY failure, never a new bespoke fallback.
4. A tool schema change is invisible to rehydrated conversation history. Nothing revalidates
   a persisted tool_calls payload against the current registry when history replays, so a
   flattened/renamed tool creates a conflicting few-shot exemplar in the model's own prior turns.
   Test against a REHYDRATED pre-change conversation fixture, not just a fresh one, and keep old
   names rejecting with a clear message for at least one deploy cycle.
5. Loosening the idempotency key to fix F-1 can reopen real double-execution if done
   carelessly. The single fact that determines safety (whether the transport-retry path reuses
   the same tool_call_id or mints a fresh one) is currently UNVERIFIED and must be established
   before choosing a fix direction. The cheapest, always-safe companion fix, a replayedMarker
   in the tool-result preview mirroring the existing resultExpiredMarker idiom, should ship
   regardless of which deeper fix direction is chosen, since it directly targets the worst
   downstream harm already observed (Aura diagnosed a stale replay as a real bug and wrote that
   misdiagnosis into long-term memory as fact).
6. A supersedes guardrail that only checks match count is not sufficient. It fixes F-2's
   exact shape (blanket closure of a multi-valued predicate) but not the subtler subject-mismatch
   variant of the same silent-data-loss symptom. Prefer an explicit-fact-identifier path over a
   pure count-threshold refusal.

## Implications for Roadmap

The suggested phase structure below follows ARCHITECTURE.md's dependency-derived build order as
the backbone, folds in F-2 and the ceremony/facade work at the points PITFALLS.md and
ARCHITECTURE.md both indicate, and inserts the operator's four post-briefing decisions at the
points in the sequence where they change what a phase can be scoped to deliver. Decisions
marked SETTLED are not open questions for the roadmapper; decisions marked SPIKE-GATED must not
be scheduled as if already resolved.

### Phase 45: Harness correctness (F-1 replay fix + F-2 supersedes guardrail)

Rationale: Foundational and lowest-risk per ARCHITECTURE.md section 4; touches only tools.Spec,
idempotency_operation.go, reserve.go, and the ArcadeDB memory bridge query. No new tools, no
new packages. Every later phase creates or edits tool specs that need the correct ReplayPolicy
value from day one; landing this first means later work classifies correctly instead of
retrofitting every touched spec afterward.

Delivers: New ReplayPolicy value (ReplayReissueExecutes) applied to shell_exec and the
other tools ARCHITECTURE.md names (section 1.4); a replayedMarker in the tool-result preview
(ship regardless of which key-shape direction is chosen, per Pitfall 5); an explicit-fact-identifier
path (or, as an interim step only, a dry-run count surfaced BEFORE the destructive UPDATE) for the
supersedes guardrail.

Addresses: PROJECT.md Active requirements "the harness never returns a result the tool did not
produce this call" and "a memory correction closes exactly the fact it names."

Avoids: Pitfall 5 (verify transport-retry tool_call_id reuse semantics BEFORE choosing a key
fix direction) and Pitfall 6 (count-only guardrail misses the subject-mismatch variant).

Acceptance criteria to write into the phase, per PITFALLS.md: both-direction adversarial
tests; same tool_call_id retry must still replay, different tool_call_id plus identical args
must re-execute; plus a multi-valued-predicate test AND a subject-mismatch test for the
supersedes fix.

### Phase 46: MCP trust scoping + facade groundwork

Rationale: Depends on Phase 45's ReplayPolicy vocabulary; applyMCPOperationMetadata
needs it to assign anything other than the uniform default to a bridged tool
(`internal/agent/mcptools/bridge.go:230`). Trust-wrapper scoping and facade groundwork are the
same code change (generalizing bridgePolicy), so land them together. Independent of the
context-ladder work; no package overlap.

Delivers: bridgePolicy generalized into a namespace-keyed table (calendar, whatsapp,
memory) carrying a hide-list, a per-tool risk override, and a ReplayPolicy override, per
ARCHITECTURE.md section 2.3.

A place research nuances the milestone's own stated goal, not contradicts it: PROJECT.md
phrases the requirement as "MCP servers are trusted; their descriptions are not wrapped as
hostile data", a blanket framing. PITFALLS.md's evidenced recommendation is to scope the wrapper
removal to bridgePolicy.recipeSource != "" (the three known, code-reviewed recipes: calendar,
WhatsApp, memory), leaving a future ad-hoc or self-extension-minted MCP mount wrapped until it
earns recipe status, because the model loses the one piece of context telling it that an
argument-description hint is not an instruction, and the Summary line reaches tool_search's
BM25 index on every turn regardless of whether the tool is ever called. The roadmapper should
treat the scoped version as the correct implementation of the milestone's requirement, not a
lesser version of it.

Avoids: Pitfall 2. Verification: the nonce-wrapped tool_output result-fencing test
(internal/agent/trust.go) still passes untouched; risk-classification tests unchanged; a
git grep for frameMCPDescription/frameMCPSummary shows a policy-gated call site, not a universal one.

### Phase 47: Tool-surface flatten (P1 set)

Rationale: Needs Phase 45's ReplayPolicy vocabulary and Phase 46's facade shape settled
first, since facade tools count toward the ~26-tool budget this phase sizes against. Highest
surface area of the milestone (every native tool file, llm_agent_promote.go's promotion
machinery, the registry boot-guard).

Prerequisite task, not optional: restore or rebuild a minimal tool-choice-accuracy eval
harness BEFORE the flatten lands (Pitfall 1); internal/eval/ is deleted per
.planning/codebase/CONCERNS.md, and shipping the flatten without it repeats exactly the blind
spot that let F-4 go unnoticed for 234 turns.

Delivers (P1, per FEATURES.md MVP definition): drop user_identifier from 4 memory tool
schemas (LOW risk, do first); ask_user trim to 3 params paired with the gateway
resume-without-model-mediation fix (shares state with Phase 45's idempotency/reservation
machinery, internal/agent/idempotency_operation.go and internal/gateway/reserve.go; sequence
or bundle deliberately, do not let them race on the same code); task.when collapse (needs a new
NL/cron/RFC-3339 parser, net-new); skill/skill_manage split, dropping the info action;
memory_write{operations[]} atomic batch (structurally closes F-2, not just guards it; depends
on an ArcadeDB memory-bridge atomic-commit path, net-new transactional semantics);
document_index+document_describe merge (LOW risk, well-evidenced).

Disagreement to carry forward, not smooth over: keep fs_glob/fs_grep SEPARATE. 5 of 5
non-hermes reference vendors keep filename search and content search as two tools; hermes is the
sole outlier. Aura's one legitimate reason to reconsider, the 10-slot deferred-tool promotion
cap, is real but unproven to bind. Recommendation: P3, instrument first (does this pair
actually evict each other within one turn?), merge only on evidence, not on "Hermes does it."

Avoids: Pitfall 1 (harness prerequisite above) and Pitfall 4 (schema change invisible to
rehydrated history). Acceptance criteria: an integration test seeding a PRE-flatten
conversation fixture and running a POST-flatten turn against it; renamed tools keep the old name
as a REJECTING alias for at least one deploy cycle rather than a silent registry miss.

P2 (sequence after P1 proves clean, per FEATURES.md): curated calendar{action:...}/
whatsapp{action:...} facade (biggest single context win, independent of native-tool changes);
web_search param trim (8 to 3).

### Phase 48: Context ladder, cheap deterministic wins

Rationale: Touches only internal/conversations + internal/runner; zero package overlap
with Phases 45-47, so it can run as a parallel workstream; if sequenced serially, do it last so
it tunes against the FINAL manifest shape rather than one still being renumbered by Phase 47.
Within this phase, order matters: the cheap, deterministic wins de-risk and prove the token
accounting before any LLM-dependent rung is attempted (ARCHITECTURE.md section 4,
CONTEXT-MANAGEMENT.md's own stated order: C-3, then C-4, then C-2, then C-6, before C-1).

Delivers, the cheapest high-value item in the entire milestone is here: wire the
ALREADY-PERSISTED real prompt_tokens (AppendTurnParams.InputTokens,
`internal/runner/runner_persist.go:313`) into ContextConfig and prefer it over the tiktoken-go
estimate in applyContextLadder's L2 trigger (C-4); no migration, no new query, no new library,
purely wiring two already-built pieces together (STACK.md Q2). Alongside it: evict superseded
tool_search results (C-3, a prerequisite that interacts directly with the un-defer work in
Phase 47; un-deferring reduces how often this fires but does not fix the mechanism); the
ghost-skill marker (C-2, about 30 lines, independent of and BEFORE any LLM rung); the per-category
context breakdown (C-6, a new sibling file, pure aggregation over structures the ladder already
has; categories should reflect Aura's ACTUAL manifest shape, not hermes' 8 categories copied
verbatim).

Addresses: PROJECT.md Active requirement "the agent can see what it already has."

Avoids: the Performance Trap of tool_search results being "immortal twice" and the
un-deferred-manifest cost becoming permanent without a before/after measurement.

### Phase 49 (SPIKE, must precede Phase 50): Short-term memory vs. summarization decision

What is SETTLED (operator decision, post-briefing; not open for roadmapper reinterpretation):
- A short-term memory tier in ArcadeDB is IN SCOPE, per the neo4j-labs/agent-memory three-tier
  model (short-term conversation, long-term entities, reasoning traces). Postgres remains the
  system of record for turns; ArcadeDB gets a derived, searchable projection.
- The reasoning tier is IN SCOPE and needs a PRD amendment extending amendment #91. The hard
  constraint: reasoning content must be stripped before it reaches any summarizer, and reasoning
  conclusions must NEVER be promoted into extracted facts. Hermes documents this exact failure
  (scratch-work conclusions preserved as facts) as a real, guarded-against bug; Aura's own audited
  session already exhibits it: a speculative reasoning conclusion became both an indexed
  document and a stored memory fact.

What is NOT SETTLED, the spike must resolve it before Phase 50 is planned in detail:
whether the C-1 summarization rung is best served by (a) LLM summarization (hermes' pattern,
STACK.md Q1's full machinery: dedicated Breaker, titleTestClient-shaped fake, deterministic
fallback, structured system prompt), (b) the searchable ArcadeDB short-term tier, or (c) both
together. The spike must measure this on REAL exported conversation data with KNOWN-CORRECT
answers; the same audit corpus already in hand
(docs/audit/live-conversations-2026-08-04/, 234+10 turns, 2.76M input tokens) is the obvious
source, but the spike phase must define what "known-correct" means for a recall/continuity
question before it can score anything.

Delivers: a decision, evidenced, on which arm(s) Phase 50 builds, not code.

Research flag: this is itself a research/measurement phase; it cannot be pre-planned the way
a normal implementation phase can, because its own output determines the next phase's scope.

### Phase 50: Summarization rung (shape determined by Phase 49)

Rationale: Gated on Phase 49's output. STACK.md's and ARCHITECTURE.md's summarization design
(sibling file context_summary.go/summarize.go, a PriorSummary field on ContextConfig,
Runner-owned two-pass orchestration on ErrContextWindowExceeded, a dedicated second
llm.Breaker instance) remains valid AS PREPARATION for whichever arm(s) the spike selects; it
is not itself the settled requirement.

Delivers, if the LLM-summarization arm is selected (in whole or in part): the design in
ARCHITECTURE.md section 3.3, with dropOldestPairs as the MANDATORY fallback on any failure
(never a new bespoke fallback, Aura's structural advantage over hermes), and anti-thrash plus
cooldown machinery budgeted into this SAME phase, not a follow-up (Pitfall 3 is explicit that
these are not optional hardening; they are the direct, measured cost of adding an LLM call to a
per-turn-re-evaluated loop). Persistence choice for the anti-thrash streak (1-2 new columns on
aura.conversations, restart-durable, versus Runner-scoped in-memory, safe-default-on-restart) is
an open phase-design call per STACK.md's "Stack Patterns by Variant"; not resolved by research,
flagged for the phase spec.

Delivers, if the ArcadeDB short-term tier arm is selected (in whole or in part): a derived,
searchable projection of conversation turns, with reasoning content stripped before anything is
written to it (per the settled reasoning-tier constraint above).

Acceptance criteria, regardless of which arm(s) ship: adversarial fixtures for thrash (2
consecutive low-savings passes), freeze loop (forced 429), an orphaned tool_call/tool_result pair,
and the ghost-skill marker surviving a summarization pass; a green suite with only happy-path
tests has not exercised hermes' hardest-won lessons (Pitfall 3's "Looks Done But Isn't" checklist).

### Cross-cutting: milestone-wide acceptance policy (applies to every phase above)

SETTLED (operator decision): every phase closes only after a real, live end-to-end evaluation
of Aura's actual response against a known-correct outcome on the running stack. A green test
suite is explicitly NOT evidence of phase completion.

Gap this collides with: internal/eval/, the harness this policy needs, is deleted
(PITFALLS.md Pitfall 1, .planning/codebase/CONCERNS.md). Phase 47 already needs a version of
this for tool-choice-accuracy; Phase 49's spike needs a version of this for
recall/continuity-correctness; the acceptance policy needs it for every other phase besides. The
roadmapper should decide whether to stand up a shared minimal internal/eval-tagged harness as
an early, explicit deliverable that later phases reuse, or require each phase to carry its own
narrow live-validation script against the audit corpus, but should not leave this implicit, since
at least three phases in this roadmap independently need some form of "run against real data,
check a known-correct answer."

### Phase Ordering Rationale

- Phases 45 to 46 to 47 are a strict dependency chain on the ReplayPolicy/trust-classification
  vocabulary, traced by ARCHITECTURE.md section 4 to specific function call sites
  (applyMCPOperationMetadata, the registry boot-guard); this is not a stylistic preference, it
  is "phase N+1's spec-correctness depends on a type phase N introduces."
  Phase 48 has zero package overlap with 45-47 and can run in parallel; the only reason to
  sequence it last if serial is that it tunes better against a settled tool manifest.
- Phase 49 (spike) must precede Phase 50 because Phase 50's file structure, dependency set, and
  even which package (internal/conversations versus the ArcadeDB memory bridge) is touched depend
  entirely on the spike's outcome; scheduling Phase 50's implementation details before Phase 49
  concludes would be planning against an unresolved variable.
- The ask_user trim (Phase 47) sharing state with Phase 45's idempotency/reservation machinery
  is the one place two "separate" phases actually touch the same code; FEATURES.md flags this
  explicitly (internal/agent/idempotency_operation.go, internal/gateway/reserve.go) and the
  roadmapper should either land them in the same phase or make the ordering (45 before 47's
  ask_user piece) explicit rather than risk two uncoordinated changes racing on the same state
  machine.

### Research Flags

Needs deeper research during planning (/gsd-plan-phase --research-phase N):
- Phase 47 (tool-surface flatten): the task.when natural-language/cron/RFC-3339 parser is
  net-new (no existing Aura dependency); the memory_write atomic multi-op transactional path
  through the ArcadeDB memory bridge is genuinely new transactional semantics, not a schema
  widening.
- Phase 49 (spike): by construction a research/measurement phase; cannot be scoped as a
  normal implementation phase since its output determines Phase 50's shape. The methodology for
  "known-correct answer" on the audit corpus needs to be designed, not assumed.
- Phase 50 (summarization rung): cannot be planned in detail until Phase 49 concludes; once
  it does, if the LLM-summarization arm is selected, re-read PITFALLS.md Pitfall 3's five
  enumerated failure modes (each with a hermes issue number) as the phase's own acceptance-test
  design input.

Phases with standard, well-documented patterns (skip research-phase):
- Phase 45 (F-1/F-2 fix): ARCHITECTURE.md section 1 traces every relevant call site by
  file:line; the fix is a new ReplayPolicy enum value plus a branch in already-small,
  already-tested files (reserve.go at 305/600 LOC, spec.go at 205/600 LOC).
- Phase 46 (MCP trust/facade): bridge_memory.go (69 LOC) is a working, in-tree precedent for
  the exact generalization needed; this is pattern-extension, not novel design, though the
  trust-scoping DECISION itself (Pitfall 2) needs an explicit design note in the phase spec so it
  is reviewable on its own diff, not folded silently into the flatten.
- Phase 48 (context cheap wins): C-4 is pure wiring of two already-built pieces; C-2/C-3/C-6
  are small, deterministic, well-precedented changes with existing test patterns to extend.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Every recommendation grounded in either hermes-agent file:line reads, Aura codebase file:line reads, or OpenRouter's own current documentation fetched directly on 2026-08-05. Zero new dependencies to evaluate reduces version-compatibility risk to none. |
| Features | HIGH for tool-granularity consensus and approval-flow patterns (cross-verified across 6 independent vendor tool sets plus Aura's own code); MEDIUM for the exact param-count targets in TOOL-SIMPLIFICATION.md (one engineer's proposal, checked against the reference collection rather than re-derived from scratch). |
| Architecture | HIGH; every claim grounded in a direct read of the named file:line; explicitly no training-data assertion about this codebase's behavior is used unverified; this is a same-codebase integration question, not an ecosystem survey. |
| Pitfalls | HIGH for anything with a file:line citation from Aura's own codebase (5 of 6 pitfalls). MEDIUM/LOW explicitly flagged for one sub-claim only: the 15-20 tools in active rotation lower-band degradation figure comes from uncontrolled industry write-ups (Zylos, MachineLearningMastery), not a published methodology; used only to bracket a range, not as the primary claim (the primary claim, Anthropic's 30-50 threshold, is HIGH confidence, official, cross-checked against an existing Aura memory note). |

Overall confidence: HIGH. The one recurring soft spot across all four documents is not a
confidence problem but a MEASUREMENT problem: several recommendations (tool-choice accuracy
post-flatten, whether the spike's chosen summarization arm actually recovers dropped context) are
well-reasoned but literally unmeasurable today because the harness that would measure them
(internal/eval/) does not exist. This is a gap to schedule, not a reason to distrust the
research.

### Gaps to Address

- internal/eval/ is deleted (.planning/codebase/CONCERNS.md, cited independently by
  PITFALLS.md Pitfall 1). At least three phases in this roadmap (47, 49, and the cross-cutting
  acceptance policy) need some form of live-data measurement against a known-correct outcome, and
  none of them currently has a harness to do it with. Address by deciding, at roadmap-creation
  time, whether to stand up a shared minimal harness as an explicit early deliverable or require
  each phase to carry its own narrow validation script.
- Whether the transport-retry path reuses the same tool_call_id or mints a fresh one is
  UNVERIFIED (PITFALLS.md Pitfall 5). This single fact determines whether Phase 45's fix
  direction (adding tool_call_id to the idempotency key, versus the recommended per-tool-class
  ReplayPolicy differentiation) is safe or catastrophic. Must be established empirically before
  the phase's implementation approach is finalized, not assumed either way.
- The next free migration slot is deliberately not hardcoded anywhere; ARCHITECTURE.md notes
  it was 0093_document_pipeline_convergence as of 2026-08-05 research date, but every phase must
  re-derive it via "ls internal/db/migrations/ | tail -1" at landing time per project convention;
  research is explicit this number will drift before Phase 50 lands.
- Anti-thrash streak persistence (restart-durable DB columns versus Runner-scoped in-memory) is an
  open phase-design call, not resolved by research (STACK.md "Stack Patterns by Variant");
  flag for whoever specs Phase 50.
- C-6's category naming is a phase-design decision, not a given; STACK.md explicitly warns
  against treating "copy hermes' 8 categories verbatim" as settled; Aura's manifest shape differs
  (no separate rules or subagent-definitions block; memory is already a distinct field).
- The spike's (Phase 49) own measurement methodology does not yet exist; "known-correct
  answer" for a conversational-recall/continuity question needs to be defined before the spike can
  be run against the audit corpus; this is itself the first task of that phase, not a
  precondition external to it.
- PROJECT.md's Active requirements list does not yet name the short-term-memory tier or the
  reasoning tier as scope items; these are net-new additions from the operator's
  post-briefing decisions and were not present when the four researchers were briefed. The
  roadmapper should ensure REQUIREMENTS.md and PROJECT.md are updated to carry these two items
  explicitly, since the four research files (written before the decisions landed) treat
  summarization as a single-arm design and do not by themselves surface the three-tier memory
  model or the PRD-amendment-#91 requirement.

## Sources

### Primary (HIGH confidence)
- internal/** (extensive file:line citations across agent, agent/tools, agent/mcptools,
  gateway, idempotency, conversations, runner, llm, llm/openai_compat, mcp, arcadedb, cron,
  db/queries, db/sqlc) - read directly by all four researchers, current codebase, ground truth.
- D:/tmp/hermes-agent/agent/context_compressor.py, context_breakdown.py,
  D:/tmp/hermes-agent/tools/file_tools.py, memory_tool.py, clarify_tool.py, cronjob_tools.py -
  read directly, primary reference-implementation source, cited by line number throughout.
- D:/tmp/system-prompts-and-models-of-ai-tools/ (Anthropic, Amp, Cursor Prompts, Windsurf,
  Warp.dev, Devin AI) - 6 independent vendor tool-schema sources, read directly.
- D:/Aura/docs/audit/live-conversations-2026-08-04/ (FINDINGS.md, TOOL-SIMPLIFICATION.md,
  CONTEXT-MANAGEMENT.md) - the motivating audit for this entire milestone, F-1 through F-10,
  C-1 through C-8, read directly by all four researchers.
- D:/Aura/.planning/codebase/ (ARCHITECTURE.md, STRUCTURE.md, CONCERNS.md) and
  .planning/PROJECT.md - read directly for milestone scope, constraints, and the confirmed
  deletion of internal/eval/.
- OpenRouter Usage Accounting cookbook
  (https://openrouter.ai/docs/cookbook/administration/usage-accounting) - fetched live
  2026-08-05, confirms native-tokenizer counts and streaming delivery semantics.
- Anthropic Tool search tool documentation
  (https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-search-tool) - fetched,
  the 30-50-tool degradation threshold, cross-checked against an existing Aura memory note
  independently citing the same number.

### Secondary (MEDIUM confidence)
- TOOL-SIMPLIFICATION.md's exact param-count targets - one engineer's proposal, validated in
  parts (A1, A2/A3, B2, C1, C2, C3, C5, C4) and challenged in one part (B1, the fs_glob/fs_grep
  merge) by FEATURES.md's cross-vendor comparison.

### Tertiary (LOW confidence, used only to bracket a range, not as a primary claim)
- Zylos - Tool-Augmented LLM Agents: Production Architecture Patterns
  (https://zylos.ai/research/2026-04-16-tool-augmented-llm-agents-production-architecture/)
  and MachineLearningMastery - The Complete Guide to Tool Selection in AI Agents
  (https://machinelearningmastery.com/the-complete-guide-to-tool-selection-in-ai-agents/) -
  no published controlled methodology; used only to note a "15-20 tools in active rotation"
  lower-band degradation report, well below Anthropic's own 30-50 hard wall.

---
Research completed: 2026-08-05
Ready for roadmap: yes, with Phase 49 (spike) explicitly flagged as a non-standard,
output-determines-next-phase step, and the eval-harness gap flagged as a cross-cutting
prerequisite the roadmapper must schedule, not assume away.
