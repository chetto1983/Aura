# Phase 45: Harness correctness - Research

**Researched:** 2026-08-13
**Domain:** Go agent-harness idempotency/replay correctness (mutating-tool-call dedup key
shape, replay-result labelling) and ArcadeDB bitemporal memory fact-correction precision.
**Confidence:** HIGH — every load-bearing claim below is anchored to a file:line read during
this research session, not to CONTEXT.md's prose or to training-data assumptions about how the
code "probably" works.

## Summary

This phase has already been decided at the discussion level: `.planning/phases/
45-harness-correctness/45-CONTEXT.md` (25 numbered decisions, D-01 through D-25) is the
output of a `/gsd-discuss-phase` session that itself did the grounding work — it read hermes
(`D:/tmp/hermes-agent`) and the exact Aura files this research was asked to re-verify. This
RESEARCH.md's job is therefore narrower than usual: **independently confirm** the six mandatory
grounding claims against the code as it stands right now (2026-08-13, HEAD includes commit
`09f91a865`), flag anything CONTEXT.md got wrong or left unstated, and package the result in the
shape the planner consumes.

**All six grounding claims verified TRUE against the running codebase.** The most consequential:
(1) the per-turn round ordinal CONTEXT.md's D-02 relies on already exists
(`internal/agent/model_round.go`) and is already reused verbatim on a transport retry
(`llm_agent.go:346`) — no new counter is being built, only a context-plumbing gap closed; (2)
`ReplayReissueExecutes` does **not** exist in `internal/agent/tools/spec.go` today —
`ReplayToolResult` is the sole `ReplayPolicy` constant — so ROADMAP.md's Phase 45 rationale
paragraph ("every later phase needs the new `ReplayPolicy` value... from day one") is stale and
must be amended before code, exactly as D-08 (BLOCKING) requires; (3) `aura.tool_invocations`'
actual schema (migration `0011_tool_invocations.up.sql`) has no room for a third `event_kind`
without a CHECK-constraint and unique-index change, confirming D-11's deferral is forced by the
schema, not a preference; (4) `internal/arcadedb/memory.go` is 495 LOC today (confirmed by
direct line count) — headroom exists under the 600-LOC cap but three decisions (D-15/D-16/D-18)
land in it in the same phase, so refactor-on-touch vigilance is warranted; (5) the
`arcadedb_integration` build tag is real, carried by 11 files, and **is** exercised — by
`scripts/agent_memory_eval.py` (`make agent-memory-eval`) inside CI job `arcadedb-integration-
test` (`.github/workflows/ci.yml:713`) — which makes CLAUDE.md's "not CI, not the Makefile, not
even `go vet`" claim about this tier stale on all four counts, a fix-on-touch item this phase
already owns per D-24/D-25's Claude's Discretion note.

**Primary recommendation:** Implement exactly what CONTEXT.md's decisions specify — this is not
a "choose a direction" research task, it is a "verify the chosen direction is buildable exactly
as described" task, and it is. Treat CONTEXT.md as the design; treat this document as its
independently-checked evidence base plus the two things CONTEXT.md does not cover in planner-
ready form: the Validation Architecture (test-tier mapping) and the Security Domain (ASVS
mapping), both required by `.planning/config.json`.

## User Constraints

<user_constraints>
### Locked Decisions (verbatim from 45-CONTEXT.md, condensed to their headline clauses —
read `.planning/phases/45-harness-correctness/45-CONTEXT.md` §Decisions for full text and
reversibility notes; the planner MUST treat every D-NN below as binding, not advisory)

- **D-01:** Child operation key gains `RoundOrdinal uint32` only; `requestID` stays excluded.
  `deriveToolOperationContext`'s `FingerprintTyped` struct gets one field; `Version` may bump to
  `tool-child-v2`.
- **D-02:** The round ordinal already exists (`internal/agent/model_round.go`); nothing new is
  built here, confirmed by this research (see Finding 1).
- **D-03:** Ordinal reaches `execTool` via `ic.Ctx = ic.WithContext(withModelRound(ic.Ctx,
  modelRound))` (or equivalent) immediately before `a.dispatch`, at `llm_agent.go:547` — NOT a
  threaded parameter.
- **D-04:** Fail CLOSED (return an error) when `modelRoundFromContext` returns `ok=false` inside
  `deriveToolOperationContext` — never silently fall back to ordinal 0.
- **D-05:** `Branch` is NOT folded into the key. The concurrent-swarm-worker key-collision hazard
  this leaves open is recorded, unchanged, deferred to Phase 51/SWARM-07.
- **D-06 (deploy):** Drain the scheduler before deploying — pre-deploy `tool-child-v1` keys
  become unreachable after the fingerprint shape changes.
- **D-07:** `ReplayReissueExecutes` is NOT built. `ReplayToolResult` remains the only
  `ReplayPolicy` value; `reserve.go:43`'s exact-match guard is unchanged.
- **D-08 (BLOCKING, before any code):** Two PRD/ROADMAP amendment commits land first — ROADMAP
  §45's "introduces the ReplayPolicy vocabulary" claim and §46's dependency on that vocabulary
  are both superseded and must be corrected, citing `model_round.go` and
  `applyMCPOperationMetadata` (`internal/agent/mcptools/bridge.go:230-240`) as evidence.
- **D-09:** `gateway.ValidateClassifiable` (`internal/gateway/guard.go`) gains a second boot-time
  panic assertion: a registered `Mutating` tool with empty `OperationScope`,
  `OperationNormalizer`, or `ReplayPolicy` panics at boot.
- **D-10:** A `replayedMarker` const mirroring `resultExpiredMarker` (`reserve.go:24-28`) is
  appended to the returned preview in both `replayResult` (Layer A) and `decodeOperationReplay`
  (Layer B), plus OTel span attributes `aura.tool.replayed` (bool) and `aura.tool.replay_layer`
  (`reservation` | `operation`). No migration.
- **D-11:** No `replay` `event_kind` row in `aura.tool_invocations` — the schema forbids it
  cheaply (confirmed, Finding 5); Success Criterion 3 is satisfied by the span attribute alone.
- **D-12:** Both same-message-duplicate repairs live in the agent loop at hermes' two sites:
  uniquify tool-call ids immediately after `consume` returns; dedupe identical `(name,
  arguments)` immediately before the `:546` history append.
- **D-13:** Adopt `<id>_d<n>` collision repair (first occurrence keeps its id, `n` starts at 2)
  and the deterministic blank-id fallback `call_<sha256("name:args:index")[:12]>`. Never a UUID.
- **D-14 (no code):** `Budget.BeforeToolCall`'s result-change veto and the operation-registry
  replay are independent mechanisms; cross-round identical calls executing is intended and does
  not need reconciling with the loop-guard.
- **D-15:** `fact_key` (already produced by `factIdentity`, `memory_provenance.go:206-216`) is
  the explicit fact identifier. Surface it in `arcadedb.FactHit` and `MemorySearchHit`; add
  `supersedes_fact_key` to `MemoryUpsertFactInput`; pivot the UPDATE to `WHERE fact_key =
  :target AND expired_at IS NULL` when a key is given. No migration.
- **D-16:** Legacy `supersedes: true` adopts hermes' ambiguity contract: 0 matches → refuse with
  candidates; exactly 1 → close it; >1 distinct → refuse with previews, naming
  `supersedes_fact_key` as the disambiguation path.
- **D-17:** The refusal is a **successful call** carrying a refusal payload —
  `MemoryUpsertFactOutput` gains `refused bool`, `reason string`, `candidates
  []MemorySearchHit`, `superseded: 0` — never an `mcp.ToolCallError`.
- **D-18 (MEM-05):** Validation guard only in `Fact.validate`: object must name an entity, not
  prose (rune bound tighter than `EntityRunes`, no newlines, no sentence-terminal punctuation).
  No migration, no backfill, existing junk vertices untouched.
- **D-19 (MEM-04):** Canonicalize the operator's subject host-side in
  `memoryUpsertFactHandler` (`cmd/arcadedb-mcp/tool_memory.go:59-103`) — the single place every
  fact is built, tenant already resolved. No schema change, no general alias mechanism.
- **D-20 (HARN-06):** Drop `!a.sideEffected` from `gateCompletion`'s guard
  (`llm_agent_completion.go:59`) so every voluntary termination is critic-judged; raise the veto
  budget from 1 to 2 with a second nudge demanding the turn name what didn't run and why.
- **D-21 (HARN-07):** Static system-prompt rule only (`messages[0]`, byte-stable): reply in the
  operator's language; no critic involvement, no config knob.
- **D-22:** Every requirement verified by a real conversation with the running Aura, read from
  OTel, `aura.tool_invocations`, `aura.conversation_turns`, and the ArcadeDB graph. A green suite
  is never sufficient evidence.
- **D-23:** Success Criterion 4 is proven by correcting the specific F-1-caused misdiagnosis fact
  in live long-term memory — not a synthetic fixture.
- **D-24 (tests):** New memory tests join the existing `arcadedb_live` leg
  (`scripts/agent_memory_eval.py:52-55`, `make agent-memory-eval`, CI job
  `arcadedb-integration-test`). `AURA_COVERAGE_TAGS` stays `db_integration`-only — this phase does
  not re-base the coverage floor. Pure logic (prose check, candidate selection,
  `canonicalSubject`, id uniquifier, ordinal key derivation) additionally needs daemon-free unit
  tests so the 85% floor sees the new code regardless.
- **D-25 (adversarial tests):** Ship paired assertions, not just the happy path — same
  `tool_call_id` retry replays; later-round identical-args re-issue re-executes; simulated
  scheduler reclaim executes exactly once; re-run the `reserve.go:233-246` fabricated-success
  regression.

### Claude's Discretion (verbatim from 45-CONTEXT.md)

- Exact wording of `replayedMarker`, the second completion-critic nudge, and the system-prompt
  language rule.
- The precise `looksLikeProse` predicate for MEM-05 and the exact canonical form for the
  operator's subject.
- Whether `Version` moves to `tool-child-v2` as documentation (the fingerprint changes either
  way since a field was added to the hashed struct).
- Fix-on-touch: `internal/toolinvocations/redact.go:23`'s stale `2048` comment (real default is
  `30000`, `config_knobs.go:98`) — the working tree already has an in-progress redact rewrite
  touching that file. **And** CLAUDE.md's claim that `arcadedb_integration` is run by nothing is
  stale on all four counts (confirmed by this research, Finding 5) — correct it in this phase's
  commit.

### Deferred Ideas (OUT OF SCOPE, verbatim from 45-CONTEXT.md)

- `Branch` in the child operation key (concurrent-swarm-worker collision) → Phase 51, SWARM-07.
- MEM-05 data-model change (object as edge property + backfill, orphan-vertex deletion) →
  Phase 49.
- General `Entity` alias mechanism (any entity with two names, not just the operator) →
  Phase 49.
- A `replay` `event_kind` row in `aura.tool_invocations` → unscheduled, needs a schema change.
- A sweep for existing junk `Entity` vertices and already-split operator entities → Phase 49.
- `ReplayReissueExecutes` — unbuilt, not deleted from the design space; reopening also means
  amending HARN-02.
- Folding `arcadedb_integration` into `AURA_COVERAGE_TAGS` → unscheduled (would re-base the
  90.3% baseline and require a live ArcadeDB + embed sidecar + MCP in every coverage run).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| HARN-01 | Re-issued mutating call in same turn with identical args executes again, fresh result | Finding 1+2: `RoundOrdinal` discriminates rounds; D-01/D-02/D-03 implement it on the existing, verified-present `modelRound` mechanism |
| HARN-02 | A genuinely retried dispatch (CLI/scheduler restart, approval resume) still executes at most once | Finding 2: ordinal restarts at 1 per turn on a scheduler reclaim (fresh `ic.RequestID`/fresh round sequence), so the parent-scoped key still collapses; D-06 deploy note (drain scheduler) closes the migration-window gap |
| HARN-03 | A legitimate replay's result is labelled so the model can tell it apart | Finding 3: `resultExpiredMarker` pattern exists and is exactly mirrorable (`reserve.go:24-28`); D-10 |
| HARN-04 | A memory correction closes exactly the fact it names, siblings untouched | Finding 4: `fact_key`/`factIdentity`/`closeSupersededStatement` already support exact-match closure; D-15/D-16/D-17 |
| HARN-06 | A turn does not end on a stated-but-unexecuted intention | Finding: `gateCompletion`'s `!a.sideEffected` guard confirmed present (`llm_agent_completion.go:59`); D-20 removes it |
| HARN-07 | Reply is in the operator's language; no raw deliberation leaks | `messages[0]` is byte-stable per `agent.go`/prompt-builder pattern; D-21 is a static-text addition only |
| HARN-08 | Duplicate ids in one assistant message repaired deterministically (`<id>_d<n>`, never UUID) | D-12/D-13; hermes reference cited in CONTEXT.md, not independently re-verified here per scope (hermes files are outside this repo) |
| HARN-09 | Identical (name,args) calls discriminated by round, not provider id | Finding 1+2: the round-ordinal mechanism is exactly this discriminator |
| MEM-04 | One person is one entity, not split across name and UUID | Finding 4b: `memoryUpsertFactHandler` (`tool_memory.go:59-103`) confirmed as the single host-side construction point; D-19 |
| MEM-05 | Multi-valued fact does not create a junk entity node per value | Finding 4: `UpsertFact` (`memory.go:213`) confirmed to mint an `Entity` for both endpoints unconditionally; D-18 adds a validation guard |
| ACC-01 | Every requirement verified by a real conversation, never a green suite alone | D-22, `.planning/REQUIREMENTS.md` preamble; this document's Validation Architecture section maps tiers accordingly |
| ACC-02 | Evidence read from OTel + `aura.tool_invocations` + `aura.conversation_turns`, no new eval harness | Finding 5+6: `aura.tool_invocations`' real schema confirmed (migration `0011`), sqlc queries confirmed (`internal/db/queries/tool_invocations.sql`) — SQL assertions for Success Criteria 1/2 are directly writable against real columns |

Note: HARN-05 (atomic batches) is explicitly **not** in this phase's requirement list (it belongs
to Phase 49) and is excluded above; the phase description's requirement list (HARN-01/02/03/04/
06/07/08/09, MEM-04/05, ACC-01/02) is reproduced in full.
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Round-ordinal discrimination (HARN-01/02/09) | Agent loop (`internal/agent`) | Gateway (`internal/gateway`) | The ordinal is minted and held in the agent's turn loop; the gateway only consumes it via the operation-context key it receives |
| Idempotency key composition (HARN-01/02) | Agent (`idempotency_operation.go`) | Gateway (`reserve.go`, `beginOperation`) | Key derivation is agent-owned (knows the tool spec + round); the gateway is a pure consumer that begins/replays against the shared registry |
| Replay marker + OTel attributes (HARN-03) | Gateway (`internal/gateway/reserve.go`) | Observability (`toolinvocations`, OTel span) | The marker is produced where the replay decision is made; span attributes are set alongside, not in a separate layer |
| Same-message duplicate-id repair (HARN-08/09) | Agent loop (`internal/agent`, post-`consume`) | — | hermes places both repairs in the conversation loop, never in the provider/wire adapter — Aura mirrors that (D-12) |
| Fact identity + supersede precision (HARN-04) | Data/Storage (`internal/arcadedb`) | MCP tool surface (`cmd/arcadedb-mcp`) | The bitemporal supersede logic and `fact_key` machinery live in the ArcadeDB client; the MCP tool is the model-facing contract that surfaces/accepts the identifier |
| Entity canonicalization (MEM-04) | MCP tool surface (`cmd/arcadedb-mcp/tool_memory.go`) | — | Single construction point for every fact write (bridge, CLI, host-driven), confirmed by grep — no alternate path bypasses it |
| Prose-guard validation (MEM-05) | Data/Storage (`internal/arcadedb`, `Fact.validate`) | — | Validation is a property of the fact model itself, enforced before any write reaches the graph |
| Turn honesty / completion critic (HARN-06) | Agent loop (`llm_agent_completion.go`) | — | The critic gate is a loop-local decision point; no other tier participates |
| Reply-language rule (HARN-07) | Prompt/system-message tier (`messages[0]`) | — | Static text injected once at message-build time, byte-stable, no runtime component |
| Evidence/verification methodology (ACC-01/02) | Observability (OTel, Postgres `aura.tool_invocations`/`aura.conversation_turns`, ArcadeDB graph) | Agent (produces the spans/rows) | This phase does not add a new evidence layer — it makes the existing three surfaces (OTel, Postgres ledger, graph) sufficient and correctly populated |

## Grounding Findings (mandatory grounding work, file:line evidence)

### Finding 1 — The dispatch loop and the round/iteration counter

**Claim to verify:** is there an existing per-turn round/iteration counter, is it in scope at
the child-operation-key build point, is it stable across a replayed dispatch?

**Evidence:**
- `internal/agent/model_round.go:9-19` — `modelRound{requestID uuid.UUID, ordinal uint32}`,
  `modelRoundOrdinal.next(requestID)` increments a `uint32` and returns a new `modelRound`.
  Context-carrying via `withModelRound`/`modelRoundFromContext` (unexported `context.Value` key,
  `modelRoundContextKey{}`).
- `internal/agent/llm_agent.go:249` — `var modelRoundOrdinal modelRoundOrdinal` declared once
  per `Run` call (i.e., once per turn/loop invocation), so it is loop-scoped, not tool-call-scoped.
- `internal/agent/llm_agent.go:342-355` — on a fresh (non-retry) LLM call:
  `modelRound = modelRoundOrdinal.next(ic.RequestID)` (line 353), then `spanCtx =
  withModelRound(spanCtx, modelRound)` (line 355). On a `transportRetry` (line 344-347),
  `modelRound = retryRound` is reused **verbatim** — the exact retry-vs-reissue line hermes draws
  at the round boundary.
- `internal/agent/llm_agent_dispatch.go:14,109` — `dispatch(ic InvocationContext, ...)` calls
  `a.executeBatch(ic.Ctx, ...)` at line 109 — **not** `spanCtx`. `spanCtx` (which carries the
  round via `withModelRound`) is a local variable inside `Run`'s loop body
  (`llm_agent.go:309,355`) that is never assigned onto `ic.Ctx` before `a.dispatch` is called
  (`llm_agent.go:547`).
- `internal/agent/llm_agent_parallel.go:30` → `internal/agent/llm_agent_tool.go:109` →
  `internal/agent/llm_agent_retry.go:72` (`execTool(ctx context.Context, ...)`) — the full call
  chain `dispatch → executeBatch → runToolRecovering → runTool → execTool` is confirmed to thread
  a single `ctx` parameter throughout, and that `ctx` originates from `ic.Ctx`, never from
  `spanCtx`.

**Conclusion:** CONTEXT.md's D-02 ("the round ordinal already exists and already
discriminates") and D-03 ("the tool path never sees it today... `llm_agent_dispatch.go:109`
dispatches on `ic.Ctx`") are both **confirmed exactly as stated**. The mechanism exists, is
stable across a transport retry (reused verbatim), and is NOT currently visible to
`execTool`/`deriveToolOperationContext`. D-03's proposed fix — re-point `ic.Ctx` through
`withModelRound` immediately before `a.dispatch` at `llm_agent.go:547`, using the existing
`InvocationContext.WithContext` (confirmed to exist and to return a copy, `agent.go:76-80`) — is
a one-line, minimal-blast-radius change consistent with the confirmed plumbing gap.

**What this does NOT prove:** that `RoundOrdinal` behaves correctly under every concurrency
shape (see Finding on `Branch`/swarm below) or that no other caller reaches `execTool` outside
the `dispatch` path — D-04's fail-closed posture is the correct mitigation for that residual
unknown, not a claim that no such caller exists.

### Finding 2 — The operation key, `ReplayPolicy`, and the STATE.md/ROADMAP.md contradiction

**Claim to verify:** exact current child-operation-key composition; `ReplayPolicy` type and its
existing values; every call site; where a round ordinal threads in; does
`ReplayReissueExecutes` already exist (resolve STATE.md vs. ROADMAP.md).

**Evidence:**
- `internal/agent/idempotency_operation.go:32-46` — `deriveToolOperationContext` builds
  `FingerprintTyped` over exactly six fields: `Version` (`"tool-child-v1"`), `ParentScope`,
  `ParentKey`, `ParentFingerprint`, `ToolScope`, `ToolFingerprint`. **No `RoundOrdinal` field
  exists today.** No `tool_call_id`, no `RequestID`, no timestamp participate — confirmed by
  reading the struct literal in full.
- `internal/agent/tools/spec.go:72-83` — `type ReplayPolicy string`; the `const` block defines
  exactly one value: `ReplayToolResult ReplayPolicy = "tool_result"`. No
  `ReplayReissueExecutes` identifier exists anywhere in the tree —
  `grep -rn "ReplayReissueExecutes" internal/` returns zero matches (only
  `ReplayToolResult` appears, at every declared mutating-tool spec: `patch.go:80`,
  `shell_bg_owner.go:252`, `shell_exec.go:116`, `skill_manage.go:58`, `swarm_spawn.go:90`,
  `task.go:146`, `write_file.go:59`, plus two test files).
- `internal/gateway/reserve.go:43` — `beginOperation`'s exact-match guard:
  `spec.ReplayPolicy != tools.ReplayToolResult` → deny. This is the only place `ReplayPolicy` is
  compared against anything, and it compares against the sole existing constant.
- **STATE.md vs. ROADMAP.md, resolved:** `git show --stat 09f91a865` (commit "Settle Phase 45
  against hermes, and drop the ReplayPolicy value the roadmap promised", authored 2026-08-13, the
  same session that produced 45-CONTEXT.md) is the commit that reconciles this. **STATE.md is
  correct as of HEAD** — `ReplayReissueExecutes` was never built, and the commit message states
  explicitly it has "no remaining job" once `RoundOrdinal` is in the key. **ROADMAP.md's Phase 45
  paragraph is the stale one** — it still reads "Every later phase needs the new `ReplayPolicy`
  value (`ReplayReissueExecutes`) from day one," confirmed still present verbatim by reading
  `.planning/ROADMAP.md` §"Phase 45" directly in this session. D-08's BLOCKING amendment
  requirement (amend ROADMAP §45 and §46 before any code) is therefore not a hypothetical
  process step — it is fixing a **currently false statement that is still on disk** as of this
  research.
- `internal/idempotency/types.go:19` — `MaxOperationKeyBytes = 200`; the key format is
  `"child:" + hex(sha256(...))` = 6 + 64 = 70 bytes, confirmed well within the CHECK constraint
  headroom (`internal/db/migrations/0043_idempotency_operations.up.sql`:
  `operation_key ... octet_length BETWEEN 1 AND 200`) — adding `RoundOrdinal` to the hashed
  struct changes the fingerprint's 64-hex-char output content but not its length, so no key-length
  concern exists.

**Conclusion:** D-01 and D-07 are fully buildable as specified. The only required code changes to
`idempotency_operation.go` are: add `RoundOrdinal uint32` to the `FingerprintTyped` anonymous
struct at line ~32-39, read it via `modelRoundFromContext(ctx)` (fail closed per D-04 if
`ok=false`), and optionally bump `Version` to `"tool-child-v2"`.

**What this does NOT prove:** whether any currently-passing test asserts on the literal
`"tool-child-v1"` string or the exact field set of `FingerprintTyped` (a fingerprint-shape golden
test would need updating) — this is a planner/executor concern for the test-update task, not
verified in this research pass. `grep -rn "tool-child-v1" internal/` was not run to completion in
this session; the planner should re-run it as a pre-flight check for the implementation task.

### Finding 3 — The marker seam (`resultExpiredMarker` → `replayedMarker`)

**Claim to verify:** exactly how `resultExpiredMarker` is produced, where it enters the tool
result surfaced to the model, whether an OTel span attribute is set alongside it.

**Evidence:**
- `internal/gateway/reserve.go:24-28` — `const resultExpiredMarker = "\n\n[result expired: full
  output no longer retained]"`. It is a package-level string constant, appended by string
  concatenation.
- `internal/gateway/reserve.go:285-305` — `replayResult(end *toolinvocations.Event)
  tools.ToolResult` is the single production site for Layer A (reservation-ledger) replays: it
  reads `end.ResultPreview`, checks `os.Stat(fullPath)` (line 294), and on stat failure does
  `preview += resultExpiredMarker; fullPath = ""` (lines 295-296) before returning
  `tools.ToolResult{Preview: preview, ...}`. This return value is what `execTool`
  (`llm_agent_retry.go:141-143`, `if verdict.Replay != nil { return *verdict.Replay, nil }`)
  hands directly back as the tool's result — i.e., it reaches the model as the literal tool-role
  message content with **no intermediate transformation**.
- `internal/gateway/reserve.go:95-113` — `decodeOperationReplay(replay *idempotency.ReplayResult)
  (tools.ToolResult, error)` is the Layer B (operation-registry) analog: it does NOT currently
  append any marker on any path (neither the `len(replay.Body)!=0` branch nor the
  preview/sidecar-only branch).
- **No OTel span attribute is set today** on either replay path — `grep`-level scan of
  `reserve.go` shows no `span.SetAttributes` call anywhere in the file; the marker today is
  string-only, and only on the Layer A path. This confirms CONTEXT.md's premise stated in the
  Discussion Log ("a replayed call writes no row today... the table ACC-02 names as the evidence
  source is currently blind to replays") is accurate for **both** the row-level blindness (no
  `aura.tool_invocations` row on a Layer B replay, since it returns before `reserve()`/an INSERT
  ever runs — Layer A's replay is `rows==0`, i.e., the INSERT that did not happen) and the
  span-level blindness (no attribute exists anywhere yet).

**Conclusion:** D-10 is directly implementable by (a) defining `replayedMarker` next to
`resultExpiredMarker` at `reserve.go:24-28`, (b) appending it in `replayResult` (Layer A) and
adding equivalent appending logic to `decodeOperationReplay` (Layer B, which currently has none —
this is new code, not a mirror of existing code, despite the "mirrors `resultExpiredMarker`"
framing being correct for the marker string itself), and (c) adding
`span.SetAttributes(attribute.Bool("aura.tool.replayed", true), attribute.String(
"aura.tool.replay_layer", "reservation"|"operation"))` at both call sites — this is a genuinely
new addition, not a mirror, since no span-attribute precedent exists in this file today.

**What this does NOT prove:** which OTel span is in scope at the exact point `replayResult`/
`decodeOperationReplay` return (both are pure functions with no `context.Context` parameter
today) — threading a span reference (or returning a marker flag for the caller to attribute) is
an implementation detail the planner must resolve; this research surfaces the gap, not the exact
signature change.

### Finding 4 — The memory upsert (supersede/validity-window logic, entity resolution, LOC)

**Claim to verify:** how a fact is matched for supersede today; what happens with several facts
sharing subject+predicate; whether an explicit fact identifier is already available; the
entity-resolution path; current LOC vs. the 600 cap.

**Evidence:**
- `internal/arcadedb/memory.go` is **495 lines** (confirmed via `wc -l`, 2026-08-13). Headroom to
  600 exists (105 lines), but D-15+D-16+D-18 all land in this file in the same phase — worth
  tracking as the file grows.
- `internal/arcadedb/memory.go:175-178` — `closeSupersededStatement`:
  `UPDATE FACT SET valid_to = :valid_to, expired_at = :expired_at, fact_key = NULL WHERE
  predicate = :predicate AND expired_at IS NULL AND (valid_to IS NULL OR valid_to > :valid_to)
  AND fact_key <> :fact_key AND outV().name = :subject_name`. Confirmed: matches on **subject +
  predicate only**, object deliberately excluded (comment at lines 170-174 explains why: "the
  object is deliberately NOT in the WHERE clause — the object is the thing that changed"). This
  is F-2's exact root cause, confirmed present and unchanged as of this session.
- `internal/arcadedb/memory.go:231-243` — `UpsertFact`'s `if fact.Supersedes` branch invokes
  `closeSupersededStatement` with `fact_key: factKey` (the **new** fact's own key, since the old
  fact hasn't been created yet at this point) bound to the `fact_key <> :fact_key` clause — this
  clause's actual job is preventing a fact from superseding itself on a re-run, not target
  disambiguation. Confirms D-15's framing that `fact_key <> :fact_key` "already carries" the
  exclusion, but the exclusion is not currently usable as a **positive** filter (there is no
  `WHERE fact_key = :target` variant today) — that positive-match UPDATE variant is new code
  D-15 must add, not a mirror of existing code.
- `internal/arcadedb/memory_provenance.go:206-216` — `factIdentity(fact Fact) string`: a
  length-prefixed SHA-256 over `(Subject, Predicate, Object, Statement)` in that order, hex-
  encoded. Confirmed deterministic and content-derived (no randomness, no timestamp).
- `internal/arcadedb/memory_provenance.go:218-223` — `activeFactKey(key string, validTo, now
  time.Time) any`: returns `nil` (i.e., SQL `NULL`) when `!validTo.IsZero() &&
  !validTo.After(now)`, else returns `key`. Confirms "NULLed when a fact is closed" — but note
  this specific function only NULLs at **creation time** for an already-expired fact; the
  **close** path's NULLing is a separate write, `closeSupersededStatement`'s own `fact_key =
  NULL` SET clause (line 175-176) — both paths independently null the key, confirmed consistent.
- `internal/arcadedb/memory.go:273-284` — `FactHit` struct has **no `FactKey` field today**.
  `cmd/arcadedb-mcp/tool_memory.go:114-124` — `MemorySearchHit` likewise has **no field for it**.
  D-15's "surface `fact_key`" is therefore a genuine new-field addition on both the internal
  `arcadedb.FactHit` type and the MCP-facing `MemorySearchHit` type, plus the corresponding SQL
  `SELECT` clauses (`searchFactsStatement` at line 290-294 and `factsAboutStatement` at line
  352-355 both currently omit `fact_key` from their projected columns — this must be added).
- `internal/arcadedb/memory.go:213` (entity minting) — `UpsertFact`'s loop over
  `[]struct{ name, kind string }{{fact.Subject, ...}, {fact.Object, ...}}` (lines 213-222) runs
  `upsertEntityStatement`/`upsertTypedEntityStatement` (`UPDATE Entity ... UPSERT`) for **both**
  endpoints **unconditionally**, confirming MEM-05's premise exactly: any `Object` string,
  including a full sentence (as in the `learned_lesson` predicate's actual usage), becomes an
  `Entity` vertex under the `UNIQUE` index on `Entity.name`
  (`memorySchemaStatements()`, line 42: `CREATE INDEX ... ON Entity (name) UNIQUE`). No
  conditional check on the shape of `Object` exists anywhere in this path today.
- `internal/arcadedb/memory.go:110-158` — `Fact.validate(limits MemoryLimits)` currently checks
  non-empty, a `ValidTo > ValidFrom` ordering constraint, and per-field rune-count limits
  (`EntityRunes`, `PredicateRunes`, `StatementRunes`, etc. via `validateRuneLimit`). **No
  prose-detection rule exists** — confirming D-18 requires genuinely new validation logic
  (`looksLikeProse` or equivalent), not a tightening of an existing bound.
- `cmd/arcadedb-mcp/tool_memory.go:59-103` — `memoryUpsertFactHandler` confirmed as the single
  handler function that constructs every `arcadedb.Fact{}` from model/CLI/host input before
  calling `client.UpsertFact`. `internal/agent/mcptools/bridge_memory.go:45-58`
  (`withMemoryUserIdentifier`) confirmed as the existing host-side-injection precedent D-19
  explicitly mirrors — it resolves `identityctx.IdentityID(ctx)` and injects
  `args["user_identifier"]` before the call reaches the bridge; MEM-04's canonicalization is a
  parallel host-side rewrite but placed in the MCP handler itself (not the bridge) specifically
  because the bridge is bypassed by CLI/host-driven calls, per D-19's stated rationale — the
  research confirms `memoryUpsertFactHandler` is genuinely the only common chokepoint (grep for
  `arcadedb.Fact{` in the tree shows this is the sole construction site outside test files).

**Conclusion:** All of D-15/D-16/D-17/D-18/D-19 describe genuinely buildable, correctly-scoped
changes against the code as it exists. None of them requires a migration (no new ArcadeDB
property — `fact_key` is already a schema property per `memorySchemaStatements()` line 51).

**What this does NOT prove:** the exact shape of the `looksLikeProse` predicate (left to Claude's
Discretion per CONTEXT.md, correctly) or whether any existing junk `Entity` vertices from
sentence-object facts already violate a `UNIQUE` constraint in a way that would make a future
migration awkward — that sweep is explicitly deferred to Phase 49 and out of this phase's
verification scope.

### Finding 5 — `aura.tool_invocations` schema and migration-number floor

**Claim to verify:** the table's actual columns; whether a migration is needed this phase; the
current migration-number floor.

**Evidence:**
- `internal/db/migrations/0011_tool_invocations.up.sql` (full read) — columns: `id` (uuid pk),
  `conversation_id` (uuid, FK), `request_id` (uuid), `tool_call_id` (text), `tool_name` (text),
  `event_kind` (text, `CHECK (event_kind IN ('start','end'))`), `seq` (integer), `ts`
  (timestamptz), `started_at`/`ended_at`/`duration_ms`, `args_raw`/`args_bytes`, `status` (text,
  `CHECK (status IS NULL OR status IN ('ok','error'))`), `error`, `result_preview`,
  `preview_bytes`, `result_bytes`, `result_truncated`, `result_sidecar_path`, `exit_code`,
  `meta` (jsonb). A `tool_invocations_event_shape` CHECK enforces the start/end column shape
  invariant. A **`UNIQUE (conversation_id, request_id, tool_call_id, event_kind)`** index
  (`tool_invocations_once_per_phase_idx`) is the constraint D-11 cites as forcing the deferral of
  a third `event_kind` value — confirmed: adding `'replay'` to the `CHECK` and to this unique
  index's discriminator space would be a real schema change, not a query-only addition, and the
  table is protected by an append-only trigger
  (`aura.reject_tool_invocations_mutation()`, confirmed present in the same file) that would make
  any later correction to a wrongly-shaped row impossible to fix by UPDATE.
- `internal/db/queries/tool_invocations.sql` — `InsertToolInvocation` (`ON CONFLICT (...)
  DO NOTHING`), `ListToolInvocationsByConversation`, `GetToolInvocationEnd`,
  `ListInFlightToolInvocationsBefore`. These four sqlc queries are the complete current surface;
  Success Criteria 1 and 2 ("two distinct executions" / "exactly one real execution") are
  directly assertable via `GetToolInvocationEnd` or a `COUNT(*) ... WHERE event_kind='end'`
  query scoped by `(conversation_id, request_id, tool_call_id)`, with **no schema change needed**
  — confirmed D-11's "no migration" claim is correct.
- **Migration floor:** `ls internal/db/migrations/ | tail -1` → `0094_verification_evidence.up.sql`
  (confirmed 2026-08-13). Per CLAUDE.md's imperative rule, if this phase's implementation
  ultimately needs a migration (it currently does not, per every D-NN decision's explicit "no
  migration" note), the number is **whatever `ls internal/db/migrations/ | tail -1` returns at
  landing time**, not `0095` computed now — this research deliberately does not hardcode a number
  for that reason.
- `internal/db/migrations/0043_idempotency_operations.up.sql` (full read) — this is the Layer B
  registry table, `aura.idempotency_operations`, confirmed distinct from `aura.tool_invocations`
  (comment at top of file: "aura.tool_invocations remains the append-only execution/audit ledger
  and is deliberately not altered here"). Its `operation_key` CHECK
  (`octet_length BETWEEN 1 AND 200`) is the constraint Finding 2 checked the new key length
  against.

**Conclusion:** No migration is required for this phase's decided scope. Success Criteria 1 and 2
are verifiable by direct SQL against existing columns.

**What this does NOT prove:** whether `aura.conversation_turns` (named in ACC-02 alongside
`aura.tool_invocations`) has any columns relevant to this phase — that table was not read in this
session since no decision in CONTEXT.md touches it; the planner should treat it as
read-only evidence infrastructure, not a target of this phase's changes.

### Finding 6 — Test tiers covering the touched packages

**Claim to verify:** which build tags cover the touched packages; does `arcadedb_integration`
block verifying Success Criterion 4; is CLAUDE.md's "nothing runs it" claim accurate.

**Evidence:**
- `internal/agent/*.go`, `internal/gateway/*.go`: predominantly **untagged** (default `go test`
  tier). Exceptions found: `internal/agent/verification_read_deadline_integration_test.go`
  (`//go:build db_integration`), `internal/agent/live_finalize_test.go` (`//go:build
  live_finalize`), `internal/agent/reasoning_tier_live_test.go` (`//go:build reasoning_live`) —
  none of these three tags is `arcadedb_integration`, and none is in this phase's touched-file
  list from CONTEXT.md's blast radius (`idempotency_operation.go`, `llm_agent.go`,
  `llm_agent_completion.go`, `reserve.go`, `guard.go`). The new unit tests D-24/D-25 require
  (ordinal key derivation, id uniquifier, `looksLikeProse`, `canonicalSubject`, candidate
  selection) land in the **default/untagged tier**, confirmed by the absence of any build tag on
  the files they'd extend.
- `internal/arcadedb/*.go`, `cmd/arcadedb-mcp/*.go`: **11 files** carry `//go:build
  arcadedb_integration` (`locomo_analyzer_test.go`, `locomo_dense_test.go`,
  `locomo_facts_test.go`, `locomo_native_test.go`, `locomo_test.go`,
  `memory_integration_test.go`, `memory_vector_live_test.go`, `testclient_test.go`,
  `cmd/arcadedb-mcp/memory_live_integration_test.go`,
  `cmd/aura/memory_latency_live_test.go`,
  `cmd/aura/serve_deprovision_memory_integration_test.go`) — this count is close to but not
  identical to CLAUDE.md's stated "10 test files carry it" (11 found, one of which,
  `testclient_test.go`, is shared live-client test scaffolding rather than a standalone test
  file — the discrepancy is not material to the claim being checked).
- **CLAUDE.md's "nothing runs it" claim is FALSE as of this session, confirmed by direct read:**
  `.github/workflows/ci.yml:713` defines job `arcadedb-integration-test`; line 811 of that same
  file runs `make agent-memory-eval`. `scripts/agent_memory_eval.py`'s `default_manifest()`
  function (lines ~44-58) defines a `arcadedb_live` suite row:
  `["go", "test", "-race", "-json", "-tags=arcadedb_integration", "-count=1",
  "-coverprofile={coverage_profile}", ..., "./internal/arcadedb/"]` — this **does** run with
  `-race` and **does** produce a coverage profile. So: it runs in CI (job exists and is
  scheduled), it runs via the Makefile (`make agent-memory-eval` is the CI step), and it is
  `go vet`-compiled implicitly by `go test` itself. CLAUDE.md's claim ("not CI, not the
  Makefile, not the coverage scripts, not even `go vet`") is stale on all four counts, exactly as
  D-24's Claude's Discretion note states — this is now a confirmed fix-on-touch correction this
  phase owns.
- `scripts/coverage_gate.sh:29` — `TAGS="${AURA_COVERAGE_TAGS:-db_integration}"` — confirmed the
  85% floor's default tag set is `db_integration` only; `arcadedb_integration` coverage is
  produced (by `agent_memory_eval.py`'s `-coverprofile`) but **not folded into the floor gate** —
  this is the "runs but feeds no coverage" split D-24 references, confirmed real and distinct
  from "does not run at all."

**Conclusion:** Success Criterion 4 (the ArcadeDB fact-correction scenario) **is** verifiable at
the `arcadedb_integration` tier via the existing live leg, and that leg genuinely executes in CI
today — it is not blocked. New arcadedb-side tests for D-15/D-16/D-17/D-18/D-19 belong in
`internal/arcadedb/` files carrying `//go:build arcadedb_integration` (for live-graph assertions)
paired with untagged unit tests for the pure logic, per D-24's explicit split.

**What this does NOT prove:** that `make agent-memory-eval`'s pass threshold (confirmed
`PASS_THRESHOLD = 96.5` out of `TOTAL_POINTS = 100.0` in `agent_memory_eval.py`) is currently
green on `master` — this research read the script's structure, not its last CI run result; the
planner/executor should check the most recent `arcadedb-integration-test` CI run before assuming
a clean baseline.

## Standard Stack

This phase adds **no new external dependency** — every change is to existing internal packages
(`internal/agent`, `internal/gateway`, `internal/arcadedb`, `cmd/arcadedb-mcp`,
`internal/idempotency`). No `go.mod` addition is implicated by any of the 25 decisions in
CONTEXT.md. `go.opentelemetry.io/otel/attribute` (already a dependency, used elsewhere for span
attributes — confirm at implementation time via `grep -rn "attribute\." internal/ | head`) is the
only "new to this file" import, and it is already vendored project-wide.

**Installation:** none required.

## Package Legitimacy Audit

**Not applicable.** This phase installs no external packages (confirmed: every D-NN decision in
CONTEXT.md operates on existing internal Go packages; no `go get`/`npm install` is implied by any
decision). The Package Legitimacy Gate protocol is skipped per its own applicability condition.

## Architecture Patterns

### System Architecture Diagram — the two dedup layers and where this phase's changes land

```
                         Assistant message with tool_calls
                                      │
                                      ▼
                    ┌─────────────────────────────────────┐
                    │  llm_agent.go Run() loop             │
                    │  - modelRoundOrdinal.next(RequestID) │  ← Finding 1: already exists
                    │  - spanCtx = withModelRound(...)     │
                    └───────────────┬───────────────────────┘
                                    │ ic.Ctx (round NOT yet attached — Finding 1 gap)
                                    ▼
                    ┌─────────────────────────────────────┐
                    │  D-12: uniquify tool_call ids         │  ← NEW (HARN-08)
                    │  (right after consume() returns)      │
                    └───────────────┬───────────────────────┘
                                    │
                                    ▼
                    ┌─────────────────────────────────────┐
                    │  D-12: dedupe identical (name,args)   │  ← NEW (HARN-09)
                    │  (immediately before :546 history     │
                    │   append)                              │
                    └───────────────┬───────────────────────┘
                                    │
                                    ▼  D-03: ic.Ctx = ic.WithContext(withModelRound(ic.Ctx, modelRound))
                    ┌─────────────────────────────────────┐
                    │  dispatch → executeBatch → runTool    │
                    │  → execTool(ctx, ...)                 │
                    └───────────────┬───────────────────────┘
                                    │
                          mutating? │
                                    ▼
                    ┌─────────────────────────────────────┐
                    │ deriveToolOperationContext            │
                    │  FingerprintTyped{                    │
                    │    Version, ParentScope, ParentKey,   │
                    │    ParentFingerprint, ToolScope,      │
                    │    ToolFingerprint,                   │
                    │    RoundOrdinal ← D-01 NEW FIELD       │
                    │  }                                     │
                    └───────────────┬───────────────────────┘
                                    │ child operation key = "child:" + hex(sha256(...))
                                    ▼
                    ┌─────────────────────────────────────┐
                    │ gateway.Decide → beginOperation       │
                    │  (Layer B: idempotency.Begin)         │
                    │  Acquired → execute                    │
                    │  Replay → decodeOperationReplay        │
                    │    + D-10 replayedMarker + OTel attr   │  ← NEW (HARN-03)
                    │  InProgress/Indeterminate/Conflict/    │
                    │  Rejected → deny (unchanged)            │
                    └───────────────┬───────────────────────┘
                                    │ Allow, no Layer-B replay
                                    ▼
                    ┌─────────────────────────────────────┐
                    │ gateway.reserve (Layer A: ledger)     │
                    │  rows==1 → Allow, Execute proceeds    │
                    │  rows==0 → replayResult()             │
                    │    + D-10 replayedMarker + OTel attr   │  ← NEW (already has marker
                    │                                          idiom, extend w/ span attr)
                    └───────────────┬───────────────────────┘
                                    │
                                    ▼
                              tool.Execute(ctx, args)
```

### Recommended Project Structure

No new files are required by CONTEXT.md's decisions. All changes are additions/edits to existing
files:
```
internal/agent/
├── idempotency_operation.go   # D-01: +RoundOrdinal field, D-04: fail-closed
├── llm_agent.go               # D-03: ic.Ctx re-point before dispatch (line ~547)
├── llm_agent_completion.go    # D-20: drop !a.sideEffected, raise veto budget to 2
├── (id uniquify/dedupe site)  # D-12/D-13: new functions near consume()/history append
internal/gateway/
├── reserve.go                 # D-10: replayedMarker const + OTel attrs, both replay paths
├── guard.go                   # D-09: second boot-time panic assertion
internal/arcadedb/
├── memory.go                  # D-15 (fact_key WHERE-pivot), D-16 (ambiguity), D-18 (validate)
├── memory_provenance.go       # (factIdentity/activeFactKey unchanged — already sufficient)
cmd/arcadedb-mcp/
├── tool_memory.go             # D-15 (I/O shapes), D-17 (refusal payload), D-19 (canonicalize)
```

### Pattern 1: Fail-closed context extraction with an explicit sentinel error

**What:** `deriveToolOperationContext` already uses this shape for `errUnsupportedParentOperation`
(`idempotency_operation.go:12,26`) — an unexported sentinel error returned when a precondition the
function requires is not met, rather than silently degrading. D-04 extends this same pattern to
the missing-round-ordinal case.
**When to use:** Any time a function derives a security/correctness-relevant value from context
and an absent value must not silently substitute a default.
**Example (existing code, confirmed present):**
```go
// Source: internal/agent/idempotency_operation.go:12,22-27
var errUnsupportedParentOperation = errors.New("unsupported parent operation scope")
// ...
switch parent.Key.Scope {
case idempotency.ScopeHTTPMutation, idempotency.ScopeCLICommand,
    idempotency.ScopeSchedulerRun, idempotency.ScopeApproval:
default:
    return nil, errUnsupportedParentOperation
}
```
D-04's addition follows the identical shape: `round, ok := modelRoundFromContext(ctx); if !ok {
return nil, errMissingModelRound }` (exact naming at implementer's discretion).

### Pattern 2: Marker-in-preview + companion metadata, not a new ledger row

**What:** `resultExpiredMarker` (Finding 3) demonstrates the established idiom for surfacing a
"this result is not what it looks like" signal to the model: append a bracketed string to the
`Preview` field rather than changing the `ToolResult` struct's shape or adding a ledger row.
**When to use:** Any time a tool result needs a machine-and-model-readable annotation without a
schema/migration change.
**Example:**
```go
// Source: internal/gateway/reserve.go:24-28, 293-296
const resultExpiredMarker = "\n\n[result expired: full output no longer retained]"
// ...
if fullPath != "" {
    if _, statErr := os.Stat(fullPath); statErr != nil {
        preview += resultExpiredMarker
        fullPath = ""
    }
}
```

### Pattern 3: Ambiguity resolved by returning candidates, not by erroring

**What:** D-16/D-17's refusal contract (0 matches → refuse+candidates; >1 → refuse+candidates;
exactly 1 → proceed) is a "successful call, refusal payload" shape — confirmed to have **no
existing precedent inside Aura's own code** (this is new to the codebase; the precedent cited in
CONTEXT.md is hermes' `tools/memory_tool.py`, an external reference file, not something already
built here). The nearest existing Aura idiom this must NOT be confused with is
`mcp.ToolCallError` + `DeterministicNoEffect()` (`internal/mcp/tool_error.go:50-65`), which D-17
explicitly rejects because it would route through `RejectOperation`/`MarkOperationIndeterminate`
and record a failed mutation for what is a clean, effect-free refusal.
**When to use:** Any tool call where "the model's request is well-formed but the target is
ambiguous" should not be conflated with "the call failed."

### Anti-Patterns to Avoid

- **Threading `spanCtx` (which carries the round) as a second context parameter alongside
  `ic.Ctx`:** rejected by D-03 in favor of re-pointing `ic.Ctx` itself via the existing
  `WithContext` copy-semantics method — introducing a second context parameter would require
  touching every signature between `Run` and `execTool` (~4 function signatures, ~30 test call
  sites per the Discussion Log's own estimate), which is exactly the higher-blast-radius
  alternative the user explicitly rejected.
- **Folding `tool_call_id` into the child operation key:** rejected by D-01/Pitfall 5 — providers
  (DeepSeek named specifically) reject duplicate ids outright, and models/providers reuse one id
  across a batch, so keying on it either breaks strict providers or fails to discriminate the
  cases it's meant to discriminate. This was the original open question the whole phase exists to
  close; do not reopen it.
- **A pure count-threshold refusal for HARN-04** (block/confirm above N matches): rejected by
  Pitfall 6 and D-16 — it fixes the *symptom* (blast radius of a bad supersede) not the *cause*
  (ambiguous target identification), and it is blind to the subject-mismatch variant of the same
  silent-data-loss failure.
- **Routing the memory-correction refusal through `mcp.ToolCallError`:** rejected by D-17 — see
  Pattern 3 above; this would durably record a failed mutation for a no-op refusal.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Round/turn discrimination for replay | A new per-tool-call counter or a fresh `tool_call_id`-keyed registry | The existing `modelRound`/`modelRoundOrdinal` mechanism (`internal/agent/model_round.go`) | Confirmed already built, already correct at the retry/reissue boundary (Finding 1) — building a parallel mechanism would be exactly the "invent instead of inventory" failure CLAUDE.md forbids |
| Deterministic tool-call id generation | A UUID-based fallback | `call_<sha256("name:args:index")[:12]>` (D-13, hermes' shape) | Random ids break prompt-cache prefix stability — a hard invariant in the reference implementation, and Aura already relies on cache-prefix stability elsewhere (`messages[0]` byte-stability, confirmed in multiple comments across `llm_agent.go`) |
| Ambiguous-fact-target resolution | A new predicate-cardinality registry (declare which predicates are single- vs multi-valued) | The existing `factIdentity`/`fact_key` content-derived identifier (Finding 4) | `fact_key` already uniquely and deterministically names a still-valid fact; a cardinality registry would be a second, redundant, error-prone source of truth that still doesn't solve fuzzy-subject-match ambiguity (Pitfall 6's stated gap) |
| Replayed-call auditability | A new `event_kind` value + schema migration on `aura.tool_invocations` | OTel span attributes (`aura.tool.replayed`, `aura.tool.replay_layer`) | The schema's `UNIQUE (conversation_id, request_id, tool_call_id, event_kind)` index and append-only trigger make a schema change costly and irreversible-by-UPDATE; the OTel span already satisfies Success Criterion 3's literal wording ("transcript / OTel span attributes") |

**Key insight:** every "don't hand-roll" item above is a case where the codebase (or the
reference implementation the user directed research toward) already has the exact mechanism
needed, discovered by reading the code rather than assumed from the requirement text. This mirrors
CLAUDE.md's own "Inventory before invention" rule directly — this phase is a case study in it.

## Common Pitfalls

### Pitfall 1: Loosening the idempotency key without preserving at-most-once (Pitfall 5, PITFALLS.md)

**What goes wrong:** Adding a discriminator to the child operation key can flip the failure mode
from "stale replay served" (F-1, the audit finding) to "real double execution of a destructive
action" if the discriminator is unstable across a genuine retry.
**Why it happens:** A `ToolCall{Name, Arguments}` looks identical at the type level whether it's
a harness-level retry of an in-flight dispatch or a model-level deliberate re-issue — the only
thing that tells them apart is *when* they arrive relative to the round boundary.
**How to avoid:** This is exactly why D-01 chose `RoundOrdinal` (stable across a transport retry,
confirmed at `llm_agent.go:346`, and reset to 1 on a scheduler reclaim since a new turn mints a
fresh round sequence) over `tool_call_id` (provider-reused, confirmed unstable per hermes'
documented DeepSeek behavior) or a bare occurrence counter (would not reset on reclaim the same
way). Ship D-25's adversarial test pair (same-id retry replays; later-round reissue re-executes;
scheduler reclaim executes exactly once) — a suite green only on "duplicate is deduplicated" is,
per PITFALLS.md verbatim, "precisely how the original bug shipped."
**Warning signs:** Two `start` reservation rows for what turns out to be the same real-world
destructive action within a short window, post-deploy.

### Pitfall 2: A cardinality-only guardrail on `supersedes` still closes the wrong fact

**What goes wrong:** "Refuse if `Supersedes:true` would close more than 1 fact" stops F-2's
exact shape (blanket closure of a multi-valued predicate) but does nothing for a
single-valued predicate where the model's chosen subject string doesn't exactly match the
stored entity's canonical name — 0 matches silently leaves the stale fact un-superseded, or a
fuzzy 1-match silently closes the wrong entity's fact.
**Why it happens:** Count is the wrong axis; the actual problem is target ambiguity, not blast
radius.
**How to avoid:** D-15/D-16's `fact_key`-based exact-match path is the fix — it names the target
directly rather than inferring it from subject+predicate matching. Confirmed buildable with no
migration (Finding 4).
**Warning signs:** A `superseded: N` count returned as an ordinary success field with no
opportunity for the caller to have seen the candidate set first (this is literally what F-2
looked like — `"superseded": 8` came back as ordinary success).

### Pitfall 3: The deploy-time key-shape change unaccounted-for reclaim window

**What goes wrong:** Adding `RoundOrdinal` to the fingerprint changes every child key's hash
output, whether or not `Version` also changes the literal string — so any `tool-child-v1`-keyed
operation still `in_progress` at deploy time (a scheduler run that crashed pre-deploy) becomes
unreachable by its old key post-deploy, and a reclaim of that run would derive a *new* key and
execute once more.
**Why it happens:** The fingerprint is a pure hash of a struct's fields; changing the struct
shape necessarily changes every hash output, deploy-boundary or not.
**How to avoid:** D-06's deploy note — drain the scheduler before deploying this phase's change.
Approval-resume is unaffected (the withheld attempt never reaches Layer B) and Layer A's
`ReservationKey` (`{ConversationID, RequestID, ToolCallID}`) is untouched by this key-shape
change, confirmed by reading `reserve.go`'s `reserve()`/`reservationStart()` functions, which use
`ReservationKey` independently of `idempotency.OperationKey`.
**Warning signs:** A scheduler-restart double-execution shortly after this phase's deploy,
specifically for a run that was in-flight at deploy time.

### Pitfall 4: The refusal payload silently becoming a durable rejection

**What goes wrong:** If the memory-correction refusal (D-16/D-17) is implemented via
`mcp.ToolCallError` + `DeterministicNoEffect()` instead of a normal successful response with a
`refused` field, it routes through `execTool`'s `RejectOperation` path
(`llm_agent_retry.go:158-163`, confirmed present) and durably records a failed mutation in the
idempotency registry for what is, semantically, a no-op read-then-refuse.
**Why it happens:** `DeterministicNoEffect()` is the *correct* mechanism for a tool that truly
attempted and failed a mutation with no side effect — it is easy to reach for it here by analogy,
but the ambiguity refusal never attempts a mutation at all.
**How to avoid:** Follow D-17 exactly — `MemoryUpsertFactOutput{refused: true, reason, candidates,
superseded: 0}` returned as a normal, successful `*mcp.CallToolResult`.
**Warning signs:** A retried correction (with `supersedes_fact_key` now supplied) is denied by the
idempotency layer because the prior refusal was recorded as a terminal rejection rather than
left retriable.

## Code Examples

### Verified pattern: fail-closed context derivation (extend, don't replace)
```go
// Source: internal/agent/idempotency_operation.go:17-27 (existing code, confirmed present)
func deriveToolOperationContext(ctx context.Context, spec tools.Spec, args json.RawMessage) (context.Context, error) {
	parent, ok := idempotency.OperationFromContext(ctx)
	if !ok || parent.Key.Scope == spec.OperationScope {
		return ctx, nil
	}
	switch parent.Key.Scope {
	case idempotency.ScopeHTTPMutation, idempotency.ScopeCLICommand,
		idempotency.ScopeSchedulerRun, idempotency.ScopeApproval:
	default:
		return nil, errUnsupportedParentOperation
	}
	// D-04 extension point: derive round here, fail closed on !ok, before
	// FingerprintTyped is built.
	toolFingerprint, err := tools.OperationFingerprint(spec, args)
	// ...
}
```

### Verified pattern: replay-path marker append (extend to both layers)
```go
// Source: internal/gateway/reserve.go:285-305 (existing code, confirmed present — Layer A)
func replayResult(end *toolinvocations.Event) tools.ToolResult {
	if end == nil {
		return tools.ToolResult{Preview: "[reservation held: the tool did NOT run and no result was recorded]"}
	}
	preview := end.ResultPreview
	fullPath := end.ResultSidecarPath
	if fullPath != "" {
		if _, statErr := os.Stat(fullPath); statErr != nil {
			preview += resultExpiredMarker
			fullPath = ""
		}
	}
	// D-10 extension point: preview += replayedMarker unconditionally here (this
	// function is only ever called on the rows==0 duplicate path), plus a span
	// attribute set by the caller (reserve() itself has no span/ctx today — the
	// caller in execTool does).
	return tools.ToolResult{Preview: preview, FullPath: fullPath, Bytes: end.ResultBytes, Truncated: end.ResultTruncated}
}
```

### Verified pattern: exact-match fact closure (new WHERE-clause variant to add)
```sql
-- Source: internal/arcadedb/memory.go:175-178 (existing statement, confirmed present)
-- Current (subject+predicate match, D-16's "legacy supersedes:true" path):
UPDATE FACT SET valid_to = :valid_to, expired_at = :expired_at, fact_key = NULL
WHERE predicate = :predicate AND expired_at IS NULL
  AND (valid_to IS NULL OR valid_to > :valid_to)
  AND fact_key <> :fact_key AND outV().name = :subject_name

-- D-15's new positive-match variant (fact_key given explicitly):
-- UPDATE FACT SET valid_to = :valid_to, expired_at = :expired_at, fact_key = NULL
-- WHERE fact_key = :target_fact_key AND expired_at IS NULL
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| Child operation key hashes only parent+tool+args (no round) | Adds `RoundOrdinal` (D-01) | This phase | Cross-round reissues execute instead of replaying (HARN-01/09); same-round dispatch retries still collapse (HARN-02) |
| `supersedes:true` closes every subject+predicate match unconditionally | `fact_key`-targeted close when supplied; ambiguity refusal for the legacy path (D-15/D-16) | This phase | HARN-04's exact-fact guarantee becomes enforceable rather than aspirational |
| `ReplayPolicy` framed by ROADMAP.md as a two-value vocabulary to be introduced | Confirmed (Finding 2) to remain single-valued (`ReplayToolResult` only); ROADMAP.md text is stale and must be amended (D-08) | This phase (amendment lands before code) | Downstream Phase 46 dependency narrows from "needs the vocabulary" to "needs only the risk-override/hide-list work" |

**Deprecated/outdated:**
- ROADMAP.md's Phase 45 rationale paragraph's claim that this phase "introduces the `ReplayPolicy`
  vocabulary every later phase's tool specs need to declare correctly from day one" — confirmed
  false against the code as it will ship (Finding 2); superseded by D-08's amendment.
- CLAUDE.md's claim that the `arcadedb_integration` tier "is not even `go vet`-compiled" and run
  by nothing — confirmed false (Finding 6); superseded by this phase's fix-on-touch commit.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Hermes' exact behavior (`agent/message_sanitization.py:536-566`, `conversation_loop.py:5950-5990,6155-6215`, `tools/memory_tool.py:462-491,615-642`) as described in CONTEXT.md/ROADMAP.md — this research did **not** independently re-read `D:/tmp/hermes-agent` (out of scope: it is not part of this repository, and CONTEXT.md's discussion session already did this reading with file:line citations) | User Constraints (D-12/D-13/D-16/D-17), Don't Hand-Roll | If a hermes citation is subtly mis-transcribed in CONTEXT.md, the specific mechanic (e.g., the exact collision-suffix algorithm) could be wrong in a way this research would not catch; the planner should treat hermes citations as CITED-tier (from the discuss-phase session), not independently re-verified here |
| A2 | `go.opentelemetry.io/otel/attribute` (or the project's existing OTel wrapper) is the correct import for D-10's span attributes | Standard Stack | If Aura wraps OTel behind an internal package instead of calling the SDK directly, the exact import path differs — a two-minute `grep -rn "SetAttributes\|attribute\." internal/` at implementation time resolves this; not independently confirmed in this session |
| A3 | No currently-passing test asserts on the literal `"tool-child-v1"` version string or the exact 6-field shape of `FingerprintTyped` | Finding 2 | If such a test exists, it needs updating alongside D-01's field addition; this research did not run the specific grep to confirm either way (noted explicitly in Finding 2's "what this does not prove"). **RESOLVED 2026-08-13** — the grep was run at planning time: zero matches on the `"tool-child-v1"` literal, and every `FingerprintTyped` test occurrence builds a PARENT struct. See Open Question 2's resolution below. |

## Open Questions (RESOLVED — both closed at planning time, 2026-08-13)

> Both questions below were open when this research was written and are now closed. The resolution
> is recorded inline under each, with the plan and task that carries it. Neither remains a blocker
> for execution; do not re-open either without a new measurement.

1. **Which span is in scope when `replayResult`/`decodeOperationReplay` need to set an OTel
   attribute?** — **RESOLVED by plan 45-03, Task 2.**
   - What we know: neither function takes a `context.Context` or span reference today (Finding
     3); the caller (`execTool`) does have `ctx`.
   - What's unclear: whether the attribute should be set by the caller (after receiving the
     `tools.ToolResult` back) using a boolean/string returned alongside it, or whether the two
     replay functions should be given a span parameter directly.
   - Recommendation: the planner should decide this as an implementation-task detail; either
     shape satisfies D-10's requirement, and CONTEXT.md deliberately left "exact wording" to
     Claude's Discretion, which by extension covers this plumbing choice too.
   - **RESOLUTION (plan 45-03, Task 2):** neither replay function is given a span parameter. The
     layer is DERIVED where the information already exists — in `execTool`, at the point it already
     reads `verdict.OperationDecision` and `verdict.Replay`: `OperationDecision ==
     idempotency.DecisionReplay` means `"operation"`, otherwise a non-nil `verdict.Replay` means
     `"reservation"`. No field is added to `gateway.Verdict`. The choice between the two remaining
     plumbing shapes — stamping inside `execTool` on the span its `ctx` already carries, versus
     returning the two values for `runTool` to stamp before `endToolSpan` — is delegated to the
     implementer as the smaller signature change, with the attribute-name literals confined to a
     single stamping helper beside `endToolSpan` in `tracing.go` and the derivation unit-tested in
     `internal/agent/llm_agent_replay_layer_test.go`. What is NOT delegated: the attribute names are
     fixed at `aura.tool.replayed` (bool) and `aura.tool.replay_layer` (string), because ACC-02's
     evidence reading queries them.

2. **Does any existing test assert on `FingerprintTyped`'s exact field set or the
   `"tool-child-v1"` literal?** — **RESOLVED by plan 45-01's `<assumption_delta_decision>`; re-run
   as a task step in plan 45-02, Task 1.**
   - What we know: the struct and constant are confirmed present (Finding 2).
   - What's unclear: whether a golden/snapshot test pins the current shape.
   - Recommendation: run `grep -rn "tool-child-v1\|FingerprintTyped" internal/ --include=*_test.go`
     as a pre-flight step before starting the D-01 implementation task.
   - **RESOLUTION (measured 2026-08-13, recorded verbatim in 45-01-PLAN.md
     `<assumption_delta_decision>`):** the grep was run. **Zero matches on the `"tool-child-v1"`
     literal anywhere in the tree.** `FingerprintTyped` appears in 8 test files
     (`llm_agent_retry_gateway_test.go:371`, `agui/idempotency_http_test.go:215`,
     `cron/handlers/agentjob_test.go:156`, `gateway/idempotency_test.go:49`,
     `idempotency/fingerprint_test.go`, `idempotency/maintenance_integration_test.go:190`,
     `idempotency/store_integration_test.go:403`, `idempotency/store_test.go:395`,
     `runner/runner_resume_idempotency_test.go:31`) but **every occurrence builds a PARENT
     fingerprint struct, never the child `FingerprintTyped` literal in
     `deriveToolOperationContext`.** No golden or snapshot test pins the child struct's field set,
     so D-01's field addition and the `tool-child-v1` → `tool-child-v2` version bump are unblocked.
     This closes assumption **A3** in the table above.
   - **Why it is still a task step and not merely a recorded answer:** RESEARCH.md carries a 14-day
     validity window and this branch is active, so plan 45-02 Task 1 re-runs the identical grep
     before the key-shape edit. If the re-run disagrees with the measurement above, the pinning test
     is updated in the same commit rather than worked around.

## Environment Availability

Skipped — this phase has no external service/tool dependencies beyond what the project already
requires to run its existing test suite (Go toolchain, Postgres, ArcadeDB for the
`arcadedb_integration` tier — all already verified present and exercised per Finding 6's evidence
that the CI job currently runs).

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go standard `testing` package + `go test -race` |
| Config file | none — build-tag-gated tiers (`db_integration`, `arcadedb_integration`), no separate test-runner config |
| Quick run command | `go test ./internal/agent/... ./internal/gateway/...` (untagged tier; covers D-01/D-03/D-04/D-09/D-12/D-13/D-20) |
| Full suite command | `make agent-memory-eval` (arcadedb-tagged live tier, covers D-15/D-16/D-17/D-18/D-19) plus `go test -race ./internal/agent/... ./internal/gateway/... ./internal/idempotency/... ./cmd/arcadedb-mcp/...` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| HARN-01 | Same-turn reissue with changed world executes twice | unit + live | `go test -run TestDeriveToolOperationContext ./internal/agent/` (new) | ❌ Wave 0 — new golden/unit test for the `RoundOrdinal`-in-key derivation |
| HARN-02 | Genuine retry/reclaim executes exactly once | unit (fake clock/context) + live scenario | `go test -run TestReserve.*Reclaim ./internal/gateway/` (new, mirrors the `reserve.go:233-246` regression D-25 calls out) | ❌ Wave 0 — adversarial pair per D-25 |
| HARN-03 | Replay carries a visible marker | unit | `go test -run TestReplayedMarker ./internal/gateway/` (new) | ❌ Wave 0 |
| HARN-04 | Correction closes exactly the named fact | `arcadedb_integration` live | `go test -race -tags=arcadedb_integration -run TestUpsertFact.*FactKey ./internal/arcadedb/` (new) | ❌ Wave 0 — plus D-23's live conversation against real Aura |
| HARN-06 | No stated-but-unexecuted intention | unit (critic gate trigger) | `go test -run TestGateCompletion ./internal/agent/` (existing file likely `llm_agent_completion_test.go` — extend) | ⚠️ extend existing |
| HARN-07 | Reply matches operator's language | **manual-only** (live conversation, ACC-01) | N/A — HARN-07 is explicitly not critic-gated per D-21 | N/A — verified live only, per D-21's explicit rejection of critic enforcement |
| HARN-08 | Duplicate ids repaired deterministically | unit | `go test -run TestUniquifyToolCallIDs ./internal/agent/` (new) | ❌ Wave 0 |
| HARN-09 | Same-round dup dropped, cross-round dup executes | unit | `go test -run TestDedupeSameMessage ./internal/agent/` (new) | ❌ Wave 0 |
| MEM-04 | Operator subject canonicalized | unit | `go test -run TestCanonicalSubject ./cmd/arcadedb-mcp/` (new) | ❌ Wave 0 |
| MEM-05 | Prose object rejected | unit | `go test -run TestFactValidateRejectsProse ./internal/arcadedb/` (new) | ❌ Wave 0 |
| ACC-01 | Every requirement verified live, not by suite alone | **manual-only** | N/A — this is a methodology requirement, not a code path | N/A |
| ACC-02 | Evidence from OTel/`tool_invocations`/`conversation_turns`/graph | live scenario + SQL assertion | Direct `psql` query against `aura.tool_invocations` post-scenario (per Finding 5, no new query needed for a `COUNT(*)` check; `GetToolInvocationEnd` sqlc query already exists for a single-row check) | ✅ existing sqlc queries sufficient |

### Sampling Rate
- **Per task commit:** `go test ./internal/agent/... ./internal/gateway/...` (untagged tier,
  seconds-scale) for every D-01/D-03/D-04/D-09/D-12/D-13/D-20 commit.
- **Per wave merge:** `go test -race -tags=arcadedb_integration ./internal/arcadedb/
  ./cmd/arcadedb-mcp/` for D-15/D-16/D-17/D-18/D-19 waves, plus the full untagged suite.
- **Phase gate:** `make agent-memory-eval` full run (live tier) green, **then** the D-23 live
  conversation correcting the real F-1-caused misdiagnosis fact, scored per CLAUDE.md's Definition
  of Done (>9.8 on the real scenario) — a green suite alone never closes this phase (ACC-01).

### Wave 0 Gaps
- [ ] `internal/agent/idempotency_operation_test.go` (new or extend existing) — covers HARN-01/02,
      D-01/D-04's `RoundOrdinal`-in-key derivation and fail-closed behavior.
- [ ] `internal/gateway/reserve_test.go` (extend existing — confirmed file exists per
      `internal/gateway/idempotency_test.go`, `injection_suite_test.go` seen in this session) —
      covers HARN-03's marker/attribute addition and D-25's adversarial retry/reissue/reclaim pair.
- [ ] `internal/agent/` — new test file for D-12/D-13's id-uniquify and same-message-dedupe
      functions (exact file name is an implementation-task decision; likely
      `llm_agent_dedup_ids_test.go` or similar, following the codebase's existing per-concern
      test-file-per-source-file convention observed in `budget_dedup.go`/its test sibling).
- [ ] `internal/arcadedb/memory_test.go` (extend existing) — covers D-15/D-16/D-18's
      `fact_key`-targeted close, ambiguity refusal, and prose-guard validation; needs both an
      untagged unit-test pass (pure validation logic) and an `arcadedb_integration`-tagged live
      pass (multi-sibling-fact scenario).
- [ ] `cmd/arcadedb-mcp/tool_memory_test.go` (extend existing) — covers D-17's refusal payload
      shape and D-19's canonicalization.
- [ ] Framework install: none — `go test`/`go test -race` already fully wired.

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | Phase does not touch auth; `identityctx.IdentityID` is read, not established, in D-19 |
| V3 Session Management | no | No session-token handling in scope |
| V4 Access Control | yes | D-04's fail-closed posture on missing operation context is an access-control-adjacent control (a caller that reaches `execTool` outside the verified dispatch path is denied loudly, not silently downgraded); D-09's boot-time panic on incomplete `ReplayPolicy`/`OperationScope`/`OperationNormalizer` metadata is the existing `gateway.ValidateClassifiable` fail-loud pattern, extended |
| V5 Input Validation | yes | D-18's `Fact.validate` prose-guard is new input validation on the `Object` field; `Fact.validate`'s existing rune-limit checks (`validateRuneLimit`, confirmed present at `memory.go:402-407`) are the established pattern to extend, not replace |
| V6 Cryptography | no direct change | `factIdentity`'s SHA-256 usage (confirmed, `memory_provenance.go:206-216`) is unchanged by this phase — it is a content-addressing hash, not a security boundary, and no new cryptographic primitive is introduced |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Replay of a stale mutating-tool result presented as fresh (F-1, the phase's own headline defect) | Tampering / Repudiation (the model cannot tell what actually happened) | D-01's `RoundOrdinal` discrimination + D-10's explicit marker, both confirmed buildable against existing mechanisms (Findings 1-3) |
| Silent over-broad data modification via an ambiguous `supersedes` match (F-2) | Tampering (unintended data loss framed as a successful, unremarked write) | D-15/D-16's exact-fact-key targeting + ambiguity refusal, confirmed buildable with no migration (Finding 4) |
| Duplicate `tool_call_id` causing a provider-side hard rejection or a synthesized/mismatched tool-result pairing | Denial of Service (request rejected) / Tampering (a synthesized result attributed to the wrong call) | D-12/D-13's deterministic collision repair, applied before validation/dispatch/history-build per hermes' documented ordering |
| An operator identity split across two `Entity` nodes (name string vs. UUID) enabling a fact write to land against the "wrong" node, invisible to a subsequent read scoped to the other node | Tampering / Information Disclosure (a fact intended for the operator's profile becomes unreachable from the canonical lookup path) | D-19's host-side canonicalization at the single `memoryUpsertFactHandler` chokepoint (confirmed to be the sole construction site, Finding 4) |

## Sources

### Primary (HIGH confidence — direct file:line reads performed in this research session)
- `D:/Repo/Aura/internal/agent/model_round.go` (full file read)
- `D:/Repo/Aura/internal/agent/idempotency_operation.go` (full file read)
- `D:/Repo/Aura/internal/gateway/reserve.go` (full file read)
- `D:/Repo/Aura/internal/agent/llm_agent.go` (lines 240-370, 490-579)
- `D:/Repo/Aura/internal/agent/llm_agent_dispatch.go` (full file read)
- `D:/Repo/Aura/internal/agent/llm_agent_retry.go` (lines 1-175)
- `D:/Repo/Aura/internal/agent/llm_agent_completion.go` (full file read)
- `D:/Repo/Aura/internal/gateway/guard.go` (full file read)
- `D:/Repo/Aura/internal/arcadedb/memory.go` (full file read)
- `D:/Repo/Aura/internal/arcadedb/memory_provenance.go` (full file read)
- `D:/Repo/Aura/cmd/arcadedb-mcp/tool_memory.go` (full file read)
- `D:/Repo/Aura/internal/agent/mcptools/bridge_memory.go` (full file read)
- `D:/Repo/Aura/internal/agent/tools/spec.go` (lines 55-100)
- `D:/Repo/Aura/internal/agent/agent.go` (lines 55-92)
- `D:/Repo/Aura/internal/db/migrations/0011_tool_invocations.up.sql` (full file read)
- `D:/Repo/Aura/internal/db/migrations/0043_idempotency_operations.up.sql` (full file read)
- `D:/Repo/Aura/internal/db/queries/tool_invocations.sql` (full file read)
- `D:/Repo/Aura/internal/idempotency/context.go`, `types.go` (partial read, key constants/types)
- `D:/Repo/Aura/scripts/agent_memory_eval.py` (lines 1-80)
- `D:/Repo/Aura/.github/workflows/ci.yml` (grep-confirmed lines 713, 811)
- `D:/Repo/Aura/scripts/coverage_gate.sh` (grep-confirmed line 29)
- `git show --stat 09f91a865` — the commit resolving the STATE.md/ROADMAP.md contradiction
- `.planning/ROADMAP.md` §"Phase 45" (direct read, confirms stale paragraph still present)
- `.planning/REQUIREMENTS.md` (full read)
- `.planning/STATE.md` (full read)
- `.planning/config.json` (full read — confirms `nyquist_validation: true`,
  `security_enforcement: true`, `security_asvs_level: 1`)

### Secondary (MEDIUM confidence — synthesized from the discuss-phase session's own citations,
not independently re-read in this session)
- `.planning/phases/45-harness-correctness/45-CONTEXT.md` and `45-DISCUSSION-LOG.md` — the
  decisions and their stated evidence; treated as the design input, cross-checked against the
  primary sources above wherever a specific file:line claim was made.
- `.planning/research/PITFALLS.md` §Pitfall 5, §Pitfall 6 (read directly in this session,
  confirming the framing CONTEXT.md summarizes).

### Tertiary (LOW confidence — not independently verified in this session)
- Hermes reference-implementation behavior (`D:/tmp/hermes-agent/...`) as cited throughout
  CONTEXT.md — this research did not re-open those files; see Assumptions Log A1.

## Metadata

**Confidence breakdown:**
- Standard stack: N/A (no new dependencies) — HIGH by default (nothing to get wrong)
- Architecture: HIGH — every structural claim (dispatch chain, key composition, marker seam,
  memory upsert logic, table schema) verified by direct file read in this session
- Pitfalls: HIGH — Pitfall 1/2/3/4 all trace to specific confirmed code (the `reserve.go:233-246`
  fabricated-success comment, the `closeSupersededStatement` subject+predicate match, the
  fingerprint-shape deploy hazard, the `RejectOperation` path) rather than generic advice

**Research date:** 2026-08-13
**Valid until:** 14 days (this is an active-development branch touching the exact files this
research reads; a subsequent commit could shift line numbers or add the `RoundOrdinal` field
already, making some "confirmed absent" findings stale — re-verify Finding 2's "no
`RoundOrdinal` field exists today" claim specifically before planning if more than a few days
have passed)
