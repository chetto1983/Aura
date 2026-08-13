# Phase 45: Harness correctness - Context

**Gathered:** 2026-08-13
**Status:** Ready for planning

<domain>
## Phase Boundary

Aura's harness never reports a tool result it did not produce **this call**, and a memory
correction closes **exactly the fact it names**.

Twelve requirements: HARN-01, HARN-02, HARN-03, HARN-04, HARN-06, HARN-07, HARN-08, HARN-09,
MEM-04, MEM-05, ACC-01, ACC-02. No new tools, no new packages, no new model-facing surface.

Files in the blast radius: `internal/agent/idempotency_operation.go`, `internal/agent/llm_agent.go`,
`internal/agent/llm_agent_completion.go`, `internal/gateway/reserve.go`, `internal/gateway/guard.go`,
`internal/arcadedb/memory.go`, `cmd/arcadedb-mcp/tool_memory.go`, the static system-prompt text,
plus the PRD/ROADMAP amendment commits that must land first.

**Not this phase:** the tool-surface work (47/48), MCP trust and the `bridgePolicy` generalization
(46), memory tiers and HARN-05 atomic batches (49), the nine compensating `learned_lesson` facts
and the `always-deliver-files` skill (54, SURF-05/ACC-03).

</domain>

<decisions>
## Implementation Decisions

The user's standing directive for this phase, given at area selection: **research hermes first,
adopt its shape, do not invent** (`D:/tmp/hermes-agent` is on disk; PROJECT.md carries this as a
named milestone constraint, CLAUDE.md as *inventory before invention* / *stop before bespoke*).
Every decision below records what hermes does, including where hermes does nothing.

### Replay discrimination (HARN-01, HARN-02)

- **D-01:** The child operation key gains **`RoundOrdinal uint32` only** — `requestID` is
  deliberately excluded. `deriveToolOperationContext` (`internal/agent/idempotency_operation.go:32-46`)
  adds one field to its `FingerprintTyped` struct and bumps `Version` to `tool-child-v2`.
  A deliberate cross-round re-issue derives a different key and executes; a scheduler reclaim
  replays the same parent key with ordinals restarting at 1, so at-most-once holds.
  `ic.RequestID` is "minted once at root" per turn (`internal/agent/agent.go:67`), so folding it
  in would make every reclaim miss its key and re-apply every mutating side effect — the exact
  double-apply the `reserve.go:233-246` comment documents.
  — **Reversibility:** costly — every child key's fingerprint changes, so reverting is a second
  key-version change carrying the same scheduler-drain window as the first.

- **D-02:** **The round ordinal already exists and already discriminates.** `internal/agent/model_round.go`
  defines `modelRound{requestID uuid.UUID, ordinal uint32}`; `llm_agent.go:353` advances it once per
  non-retry LLM call and `llm_agent.go:346` **reuses the same round verbatim on a transport retry**
  (`modelRound = retryRound`). That is precisely the retry-vs-reissue line hermes draws at the round
  boundary. This closes ROADMAP §45's instruction to "confirm the round-ordinal shape against Aura's
  own dispatch loop before building" — confirmed present, no new counter is built.

- **D-03:** The ordinal reaches `execTool` by **re-pointing `ic.Ctx` through the existing
  `withModelRound` immediately before `a.dispatch`** (`llm_agent.go:547`), not by threading a
  parameter through four signatures. Necessary because `llm_agent.go:355` puts the round on
  `spanCtx` while `llm_agent_dispatch.go:109` dispatches on `ic.Ctx` — the tool path never sees it
  today. `InvocationContext.WithContext` already returns a copy (D-24), so the receiver is untouched.

- **D-04:** **Fail closed when the ordinal is absent.** `modelRoundFromContext` returning `ok=false`
  in `deriveToolOperationContext` is an error, never a fallback to ordinal 0 — a silent fallback
  would restore exactly today's collapsing behaviour. `dispatch → executeBatch → runTool → execTool`
  is the single path to `execTool`, so absence means a caller outside the loop, which should be loud.

- **D-05:** **`Branch` is NOT folded into the key.** Two concurrent swarm workers share `RequestID`
  (`WithSubAgent` only re-points `Agent`) and each runs its own `modelRoundOrdinal` from 1, so
  identical (tool, args) calls at their own round 1 derive one key and one worker's write is
  silently replaced by a replay. This is **pre-existing and unchanged** by this phase. Recorded as a
  known hazard for Phase 51 / SWARM-07, which owns concurrent-worker durable writes.

- **D-06 (deploy):** Adding a field changes the fingerprint whether or not the `Version` string
  changes, so pre-deploy `tool-child-v1` keys become unreachable. The only exposed case is a
  scheduler run that crashes before the deploy and is reclaimed after it, which would execute once
  more. **Drain the scheduler before deploying this phase.** Approval-resume is unaffected (the
  withheld attempt never reaches Layer B) and Layer A's `ReservationKey` is untouched.

### Replay policy vocabulary (supersedes the ROADMAP)

- **D-07:** **`ReplayReissueExecutes` is NOT built. `ReplayToolResult` stays the only value.**
  With `RoundOrdinal` in the key, a cross-round re-issue already executes for *every* mutating tool
  with no per-tool declaration; a second value could only mean "never replay even on a genuine
  reclaim", which is HARN-02's opposite and is what hermes accepts by having no registry at all.
  Consequence: `reserve.go:43`'s exact-match guard (`spec.ReplayPolicy != tools.ReplayToolResult`)
  stays **unchanged**, so no new deny surface is introduced on the path every mutating call takes.
  — **Reversibility:** reversible — adding a constant and relaxing one comparison later is cheap.

- **D-08 (BLOCKING, before any code):** Two amendment commits land first, per CLAUDE.md's
  PRD-amendment-before-code rule and the PRD-first principle that a measurement beats the document:
  1. **ROADMAP §45** — "It introduces the `ReplayPolicy` vocabulary every later phase's tool specs
     need to declare correctly from day one" is obsolete; the vocabulary is not introduced.
  2. **ROADMAP §46** — "needs Phase 45's vocabulary — `applyMCPOperationMetadata` can't assign
     anything but the uniform default to a bridged tool without it" narrows: Phase 46 still depends
     on Phase 45, but for the risk-override and hide-list work only. The uniform default is correct.
  Both cite the evidence: `model_round.go` already discriminates, and `applyMCPOperationMetadata`
  (`internal/agent/mcptools/bridge.go:230-240`) already fills all three fields unconditionally.

- **D-09:** `gateway.ValidateClassifiable` (`internal/gateway/guard.go`) gains a second assertion:
  a registered `Mutating` tool with an empty `OperationScope`, `OperationNormalizer` or
  `ReplayPolicy` **panics at boot**, same idiom and same posture as the existing multiplexed-classifier
  check. This is the protection the second policy value was standing in for, and it is orthogonal to
  how many values exist. No bridged tool can trip it today; Phase 46's `bridgePolicy` overrides are
  what could make it reachable from a fail-soft mount, and that constraint is Phase 46's to carry.

### Replay marker (HARN-03)

- **D-10:** A `replayedMarker` const mirroring `resultExpiredMarker` (`reserve.go:24-28`) is appended
  to the returned preview in **both** replay paths — `replayResult` (Layer A, reservation duplicate,
  same `tool_call_id`) and `decodeOperationReplay` (Layer B, operation registry). Plus OTel span
  attributes `aura.tool.replayed` (bool) and `aura.tool.replay_layer` (`reservation` | `operation`)
  so the two are distinguishable in diagnosis. **No migration.**

- **D-11:** **No `replay` `event_kind` row.** `0011_tool_invocations.up.sql:11` constrains
  `event_kind IN ('start','end')`, `:32` shapes columns per kind, and `:45-46` is
  `UNIQUE (conversation_id, request_id, tool_call_id, event_kind)` — a Layer A replay shares the
  original's `tool_call_id`, so a third kind would need a discriminator column in that index, on an
  append-only table protected by an un-deletable trigger. Success Criterion 3 says "the transcript /
  OTel span attributes", which the span attribute satisfies; Success Criterion 1 stays countable as
  two `end` rows for two real executions. Deferred, not overlooked.

### Same-message duplicate calls (HARN-08, HARN-09)

- **D-12:** Both repairs live in the **agent loop**, at hermes' two sites (`conversation_loop.py`,
  not a provider adapter):
  1. **Uniquify ids first** — the moment `consume` returns `calls`, before the terminal/runnable
     partition, before argument validation, before dispatch, before the history append. hermes'
     comment: *"BEFORE any downstream consumer (validation error paths, dispatch, history build,
     Responses item-id derivation)"* (`conversation_loop.py:5967-5972`).
  2. **Dedupe `(name, arguments)` late** — immediately before the assistant message is appended at
     `llm_agent.go:546`, so the dropped call never enters history and therefore never reaches the
     wire. No orphan `tool_call`, no synthesized result. hermes states the invariant it protects:
     *"providers require each tool_call to have a matching tool result and vice versa"*
     (`conversation_loop.py:6176-6180`).

- **D-13:** Adopt **`<id>_d<n>` collision repair** (first occurrence keeps its id; n starts at 2 and
  increments until free) **and the deterministic blank-id fallback**
  `call_<sha256("name:args:index")[:12]>`. Never a UUID — hermes marks this a HARD INVARIANT because
  random ids break prompt-cache prefix stability. The blank-id fallback also closes a latent Aura
  bug: `ReservationKey{ConversationID, RequestID, ToolCallID}` feeds
  `UNIQUE (conversation_id, request_id, tool_call_id, event_kind)`, so two blank-id calls in one turn
  collapse onto one reservation and the second is never run.
  **Not adopted:** hermes' composite `call_x|fc_y` splitting — Codex Responses-specific, dead code
  against OpenRouter/DeepSeek.

- **D-14 (interaction, no code):** cross-round identical calls now execute, and `Budget.BeforeToolCall`
  still watches them. Its result-change **veto** (`internal/agent/budget_dedup.go`) treats a changed
  result as progress, so a genuine re-issue after a world change passes; it only fires on three
  identical results running, which is the runaway loop it exists for. No change needed — recorded so
  a planner does not read the two mechanisms as contradictory.

### Memory correction (HARN-04)

- **D-15:** **`fact_key` is the explicit fact identifier, and it already exists.** `factIdentity`
  (`internal/arcadedb/memory_provenance.go:206-216`) is a length-prefixed sha256 over
  `(Subject, Predicate, Object, Statement)`, stored on the FACT edge, and **NULLed when a fact is
  closed** (`activeFactKey`, and `closeSupersededStatement` sets `fact_key = NULL`) — so it names
  only still-valid facts, exactly the set a correction may close. `closeSupersededStatement`
  (`memory.go:175-178`) already carries `AND fact_key <> :fact_key`.
  Changes: surface `fact_key` in `arcadedb.FactHit` and `MemorySearchHit`; add `supersedes_fact_key`
  to `MemoryUpsertFactInput`; pivot the UPDATE to `WHERE fact_key = :target AND expired_at IS NULL`
  when a key is given. **No migration** — the property is on every edge already.
  — **Reversibility:** costly — `fact_key` becomes a published field of a model-facing tool output;
  removing it later breaks rehydrated history and any caller that learned to pass it.

- **D-16:** Legacy `supersedes: true` keeps working but adopts **hermes' ambiguity contract**
  (`tools/memory_tool.py:462-491, 615-642`): resolve the candidate set first, then
  0 matches → refuse and return the candidates; exactly 1 → close it; more than 1 distinct → refuse,
  return previews, and name `supersedes_fact_key` as the way to disambiguate. Replaying F-2: the
  eight `learned_lesson` facts are **refused with eight previews returned**, `lives_in` is unchanged.
  This is Pitfall 6's "explicit fact identifier over count-threshold refusal", already shipped in a
  reference harness rather than designed here.
  — **Reversibility:** costly — a shipped tool contract's success semantics change; the
  `memory-aura` skill text and any rehydrated turn describing the old behaviour need updating with it.

- **D-17:** The refusal comes back as a **successful call carrying a refusal payload** —
  `MemoryUpsertFactOutput` gains `refused bool`, `reason string`, `candidates []MemorySearchHit`,
  with `superseded: 0`. hermes' shape verbatim (`{"success": false, "error": ..., "current_entries": [...]}`).
  Not an `mcp.ToolCallError`: an error would route through `execTool`'s
  `RejectOperation`/`MarkOperationIndeterminate` path (`llm_agent_retry.go:158-163`) and record a
  failed mutation in the ledger for what is a clean, effect-free refusal — and the candidate list,
  which is the useful content, would have to ride an error string. The corrected retry carries
  different arguments and therefore a different operation key.

### Entity identity (MEM-04, MEM-05)

- **D-18 (MEM-05):** **Validation guard only.** `UpsertFact` (`memory.go:213`) mints an `Entity`
  vertex for *both* endpoints unconditionally under a `UNIQUE` index on `Entity.name`, which is how a
  `learned_lesson` whose object is a sentence becomes a node. `Fact.validate` gains a rule that an
  object must name an entity, not prose — a tighter bound than the shared `EntityRunes` limit, plus
  no newlines and no sentence-terminal punctuation; the detail belongs in `statement`, which is the
  field that gets embedded and searched. **No migration, no backfill, no data-model change.**
  Existing junk vertices stay. Consistent with this being the phase the roadmap calls foundational
  and lowest-risk.
  — **Reversibility:** reversible — relaxing a validation rule is local; note it rejects writes that
  previously succeeded, so the model must handle the error.

- **D-19 (MEM-04):** **Canonicalize the operator's subject host-side**, in `memoryUpsertFactHandler`
  (`cmd/arcadedb-mcp/tool_memory.go:59-103`) — the single place every fact is built and where the
  tenant is already resolved from `user_identifier`, so it covers the bridge, the CLI and
  host-driven paths alike (the bridge alone would miss the latter two). A subject naming the
  operator — the identity UUID, or a configured display name — is rewritten to one canonical form
  before the write. Mirrors the existing `withMemoryUserIdentifier` injection
  (`internal/agent/mcptools/bridge_memory.go:45-58`). **No schema change.** Scope is the operator,
  which is what MEM-04 states. No general `Entity` alias mechanism — deferred.

### Turn honesty and reply discipline (HARN-06, HARN-07)

- **D-20 (HARN-06):** Both halves. (a) Drop `!a.sideEffected` from `gateCompletion`
  (`internal/agent/llm_agent_completion.go:59`) so the critic judges **every** voluntary
  termination — the failure HARN-06 describes is a turn that states an intention and dispatches
  nothing mutating, which never reaches the critic today. (b) Raise the veto budget to 2, the second
  nudge demanding the turn state plainly which action did not run and why, rather than claiming
  completion or ending silently. The critic's system prompt already carries the load-bearing rule
  (*"Judge ONLY by the tool results, never by the agent's claims. A script that was written but never
  executed ... is NOT done"*) and already handles read-only turns (*"If the user only asked a
  question and no artifact was requested, a well-supported answer IS done"*), so widening the
  trigger costs tokens, not false vetoes. `cfg.CompletionCriticModel` already exists to make those
  tokens cheap. Fail-open behaviour is unchanged.

- **D-21 (HARN-07):** **Static system-prompt rule only** — reply in the language the operator wrote
  in; planning, self-critique and tool reasoning never appear in a user-facing reply. It is
  `messages[0]` text, so byte-stable and cache-safe, with no config knob to drift. hermes parity:
  its only language rule anywhere is one prompt line in the context compressor
  (*"Write the summary in the same language the user was using"*, `agent/context_compressor.py:3663`)
  — no detection, no enforcement, in either codebase. **Not** added to the completion critic's remit:
  that gate is deliberately one narrow question, and a fuzzier critic is a noisier critic. Verified
  by ACC-01's live conversation, not by a gate.

### Evidence and acceptance (ACC-01, ACC-02)

- **D-22:** Every requirement is verified by a **real conversation with the running Aura**, scored on
  the answer she gave and the state she produced, read from OTel traces (including the new
  `aura.tool.replayed` / `aura.tool.replay_layer` attributes), `aura.tool_invocations`,
  `aura.conversation_turns`, and the ArcadeDB graph itself. **A green suite is not evidence and
  never closes a box.** No new eval harness; `internal/eval/` stays deleted. CLAUDE.md's Definition
  of Done applies: E2E validated at >9.8 on a real scenario.

- **D-23:** **Success Criterion 4 is proven by correcting the misdiagnosis F-1 actually caused** —
  the ArcadeDB-orphan-nodes theory Aura wrote into long-term memory as fact off a stale replay
  (Pitfall 5's closing note says it needs correcting regardless of which fix ships). Closing it
  exercises exactly this phase's machinery end to end: recall returns `fact_key`, the correction
  names one fact, the siblings survive. Scope is **that one fact** — the nine compensating
  `learned_lesson` facts and the `always-deliver-files` skill remain Phase 54's (SURF-05, ACC-03).

- **D-24 (tests):** New memory tests join the **existing** `arcadedb_live` leg, which already runs
  `go test -race -tags=arcadedb_integration -coverprofile=... ./internal/arcadedb/`
  (`scripts/agent_memory_eval.py:52-55`, driven by `make agent-memory-eval` from CI job
  `arcadedb-integration-test`, `.github/workflows/ci.yml:713`, with `CI: "true"` arming the
  no-skip-as-green guards). `AURA_COVERAGE_TAGS` stays `db_integration` — the 85% floor is not
  re-based by this phase. **The pure logic must also carry daemon-free unit tests** (the prose check,
  candidate selection, `canonicalSubject`, the id uniquifier, the ordinal key derivation) so the
  floor sees the new code regardless.

- **D-25 (adversarial tests, Pitfall 5 item 4):** Ship the paired assertions, not just the happy
  path: a retried dispatch with the **same** `tool_call_id` must replay; a deliberate re-issue in a
  **later round** with identical arguments must re-execute; a simulated scheduler reclaim must still
  execute exactly once. Re-run the reservation-fabrication regression the `reserve.go:233-246`
  comment documents. A suite green only on "duplicate is deduplicated" is how the original bug
  shipped.

### Claude's Discretion

- Exact wording of `replayedMarker`, the second completion-critic nudge, and the system-prompt
  language rule — shapes are fixed above, prose is the implementer's.
- The precise `looksLikeProse` predicate for MEM-05 (rune bound, newline and terminal-punctuation
  rules) and the exact canonical form chosen for the operator's subject.
- Whether `Version` moves to `tool-child-v2` as documentation (the fingerprint changes either way).
- Fix-on-touch: `internal/toolinvocations/redact.go:23` documents
  `AURA_CONTEXT_PREVIEW_CAP_BYTES=2048` where `config_knobs.go:98` says `30000` (REQUIREMENTS.md
  Fix-on-touch table) — the working tree already carries an in-progress redact rewrite touching
  that file. **And CLAUDE.md's claim that the `arcadedb_integration` tier is run by nothing is
  stale on all four counts** (see D-24) — correct it in this phase's commit.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase definition
- `.planning/ROADMAP.md` §"Phase 45: Harness correctness" — goal, the twelve requirements, five
  success criteria, and the build-order rationale. **Note D-08: two paragraphs of it are superseded
  by this discussion and must be amended before code.**
- `.planning/REQUIREMENTS.md` — HARN-01/02/03/04/06/07/08/09, MEM-04/05, ACC-01/02 verbatim, plus
  the Fix-on-touch table.
- `.planning/PROJECT.md` — core value, constraints, and the named constraint to read the reference
  implementations before designing anything in this milestone.
- `prd.md` — project truth-source; the amendment commits in D-08 land here and in ROADMAP.md.

### Research grounding
- `.planning/research/ARCHITECTURE.md` §1 (the replay defect, the two dedup layers, the four parent
  scopes, the approval-resume trace) and §4 (build order). **§1.4 recommends the `ReplayPolicy`
  route that D-07 supersedes** — read §1.1-1.3 as evidence, §1.4 as history.
- `.planning/research/PITFALLS.md` §"Pitfall 5" (loosening the key without reopening double
  execution; the four how-to-avoid items, of which item 4 is D-25) and §"Pitfall 6" (a cardinality
  guardrail still closes the wrong fact; prefer an explicit identifier).
- `.planning/codebase/ARCHITECTURE.md` — component map, the fresh-agent-per-turn and byte-stable
  `messages[0]` invariants, the always-active tool set.

### Reference implementations (on disk, read before designing)
- `D:/tmp/hermes-agent/agent/message_sanitization.py:500-598` — `deterministic_call_id`,
  `coalesce_tool_call_id`, `uniquify_tool_call_ids`, and the HARD INVARIANT never to use a UUID.
- `D:/tmp/hermes-agent/run_agent.py:4580-4640` — `_cap_delegate_task_calls`,
  `_deduplicate_tool_calls`, the forwarders.
- `D:/tmp/hermes-agent/agent/conversation_loop.py:5950-5990, 6155-6215` — the ordering that makes
  both work, and the provider invariant that every `tool_call` needs a matching result.
- `D:/tmp/hermes-agent/tools/memory_tool.py:462-491, 615-642` — the correction contract adopted in
  D-16: ambiguity refuses and returns the candidate set.
- `D:/tmp/hermes-agent/agent/tool_guardrails.py:20-38` — `IDEMPOTENT_TOOL_NAMES`, evidence that
  hermes' "idempotent" is a read-only loop-guard list, not a replay registry.
- `D:/tmp/hermes-agent/cron/scheduler.py:291, 2367-2417` — `claim_dispatch` /
  `heartbeat_run_claim`, hermes' at-most-once, held at the job rather than the tool call.
- `D:/tmp/hermes-agent/agent/context_compressor.py:3663` — hermes' only language rule anywhere.
- `D:/tmp/hermes-agent/agent/verification_stop.py:95-205` — the source of Aura's own
  `BuildVerifyOnStopNudge` port.

### Aura code the decisions bind to
- `internal/agent/model_round.go` — the ordinal that already exists (D-02).
- `internal/agent/llm_agent.go:249-260, 343-355, 541-547` — where it is minted, held across a
  transport retry, and where D-03's one line goes.
- `internal/agent/llm_agent_dispatch.go:109` — `executeBatch(ic.Ctx, ...)`, why D-03 is needed.
- `internal/agent/idempotency_operation.go:14-56` — the key derivation D-01 changes.
- `internal/gateway/reserve.go:24-28, 35-93, 233-250, 280-305` — the markers, `beginOperation`'s
  decision switch, the fabricated-success comment, `replayResult`.
- `internal/gateway/guard.go` — the boot guard D-09 extends.
- `internal/agent/llm_agent_completion.go:28-33, 58-72` — the critic prompt and trigger D-20 changes.
- `internal/agent/llm_agent_verification.go` — the adjacent verify-on-stop gate (code-edit scoped;
  not what D-20 touches).
- `internal/arcadedb/memory.go:160-271` — entity upsert, `closeSupersededStatement`, `UpsertFact`.
- `internal/arcadedb/memory_provenance.go:181-220` — `normalizeFact`, `factIdentity`, `activeFactKey`.
- `cmd/arcadedb-mcp/tool_memory.go` — the MCP input/output shapes D-15/D-16/D-17/D-19 change.
- `internal/db/migrations/0011_tool_invocations.up.sql:11, 32, 45-46` — why D-11 defers.
- `scripts/agent_memory_eval.py:52-55`, `.github/workflows/ci.yml:713`, `scripts/coverage_gate.sh:29`
  — the test tiers D-24 targets.

### Missing — flagged, not available
- `docs/audit/live-conversations-2026-08-04/{FINDINGS,TOOL-SIMPLIFICATION,CONTEXT-MANAGEMENT}.md` —
  cited throughout ROADMAP.md, REQUIREMENTS.md and the research as the primary evidence for F-1/F-2
  with file:line detail. **Not on disk and never in git**: `.gitignore:104` excludes
  `docs/audit/live-conversations-*`. Downstream agents cannot read it. What survives is the digest
  quoted inline in ROADMAP §45, REQUIREMENTS.md's preamble, and PITFALLS.md §5/§6 — treat those as
  the evidence of record for this phase, and do not cite a line number from a file no one can open.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `modelRound` / `withModelRound` / `modelRoundFromContext` (`internal/agent/model_round.go`) — the
  discriminator, already minted, already advanced per round, already held across a transport retry.
- `factIdentity` → `fact_key` (`memory_provenance.go:206-216`) — a content-derived, deterministic
  identifier already stored on every FACT edge and already NULLed on close.
- `resultExpiredMarker` (`reserve.go:24-28`) — the marker idiom D-10 copies.
- `gateway.ValidateClassifiable` (`guard.go`) — the boot-time fail-loud guard D-09 extends.
- `gateCompletion` + `completionCriticSystem` (`llm_agent_completion.go`) — HARN-06's mechanism,
  already carrying the right prompt, gated too narrowly.
- `withMemoryUserIdentifier` (`mcptools/bridge_memory.go:45-58`) — the host-side argument-derivation
  pattern D-19 mirrors.
- `mcp.ToolCallError` + `DeterministicNoEffect()` (`internal/mcp/tool_error.go:50-65`) — considered
  and deliberately not used for the refusal (D-17).
- `Budget.BeforeToolCall` result-change veto (`budget_dedup.go`) — the loop guard that must not be
  confused with the operation registry (D-14).

### Established Patterns
- **Two independent dedup layers.** Layer A = the reservation ledger keyed
  `{ConversationID, RequestID, ToolCallID}` (`reserve()`); Layer B = the idempotency operation
  registry keyed by parent + tool + args (`beginOperation`). **Only Layer B is F-1's cause and only
  Layer B changes.** Layer A is untouched by every decision here.
- **Only `DecisionReplay` may change behaviour.** `InProgress`, `Indeterminate`, `Conflict` and
  `Rejected` keep today's deny/wait semantics, or the fabricated-success class of bug returns from
  the other direction (research §1.3, `reserve.go:233-246`).
- **Fail closed on wiring, fail open on outages.** `Registry.Register` panics on a duplicate name,
  `Registry.Validate` fails closed with no non-deferred tool, `ValidateClassifiable` panics on a
  missing classifier — while the critic, the verify-on-stop gate and MCP mounts all fail open/soft.
  D-04 and D-09 sit on the wiring side; D-20 keeps the critic's fail-open.
- **`messages[0]` is byte-stable.** D-21's rule is static text and belongs there; nothing volatile
  may join it.
- **No non-test Go file over 600 LOC**, refactor on touch. `reserve.go` is 305, `guard.go` is small,
  `llm_agent_completion.go` is 288, `memory.go` is 495 — all have room, but `memory.go` is the one to
  watch as D-15/D-16/D-18 land together.

### Integration Points
- `llm_agent.go:547` — one line, the ordinal onto `ic.Ctx` (D-03).
- `llm_agent.go` after `consume`, and again at `:546` — the two hermes sites (D-12).
- `idempotency_operation.go:32-46` — one field in the fingerprint struct (D-01).
- `reserve.go` `replayResult` + `decodeOperationReplay` — the marker and span attributes (D-10).
- `guard.go` — one more loop body (D-09).
- `llm_agent_completion.go:59` — one condition, one counter bound, one nudge (D-20).
- `memory.go` `UpsertFact` / `closeSupersededStatement` / `Fact.validate` — D-15/D-16/D-18.
- `cmd/arcadedb-mcp/tool_memory.go` — input/output shapes and the canonicalization (D-15/17/19).

</code_context>

<specifics>
## Specific Ideas

- **"Research hermes, no bespoke."** The operator's directive at area selection, and the organising
  rule for the whole phase. Where hermes has a shipped shape, adopt it (D-12, D-13, D-16, D-17,
  D-21). Where hermes has nothing — no bitemporal model, no replay registry, no stated-but-unexecuted
  check — say so explicitly rather than inventing and calling it parity (D-07, D-18, D-19, D-20).
- Two roadmap-level claims were falsified by reading the code rather than the document, and both are
  recorded as amendments rather than quietly worked around (D-02, D-08). This is the PRD-first
  principle running the direction it is supposed to: the measurement wins and the document is
  corrected, with the date and the evidence.
- The phase's own bug produced a false fact in live memory; correcting that specific fact is the
  acceptance test (D-23), not a synthetic fixture.

</specifics>

<deferred>
## Deferred Ideas

- **`Branch` in the child operation key** — closes the concurrent-swarm-worker key collision where
  one worker's write is silently replaced by a replay. Pre-existing, not a regression. → Phase 51,
  SWARM-07.
- **MEM-05 data-model change** — object as a FACT edge property plus a literal sink, rewriting every
  `inV().name AS object` projection, with a backfill per identity database and deletion of orphaned
  `Entity` vertices. Actually removes the junk instead of preventing more. → Phase 49.
- **General `Entity` alias mechanism** — alias list or `ALIAS_OF` edge with resolution on every read
  and write, so any entity that acquires two names converges, not just the operator. → Phase 49.
- **A `replay` `event_kind` in `aura.tool_invocations`** — makes "was this replayed?" a SQL question;
  needs a CHECK relaxation, a shape arm, and a discriminator column in the unique index, on an
  append-only trigger-protected table. → whichever phase needs replay counted rather than observed.
- **A sweep for existing junk** — the `Entity` vertices already minted from prose objects, and the
  operator entities already split across a name and a UUID. Both fixes here are forward-looking
  only. → Phase 49, alongside the memory-tier work that touches the same rows.
- **`ReplayReissueExecutes`** — not deleted from the design space, just unbuilt. Trigger to reopen:
  a tool appears whose recorded result must never be returned even on a genuine scheduler reclaim,
  which would also mean amending HARN-02.
- **Folding `arcadedb_integration` into `AURA_COVERAGE_TAGS`** — would close the runs-but-feeds-no-
  coverage split generally, at the cost of a live ArcadeDB + embed sidecar + MCP in every coverage
  run, including `scripts/coverage_docker.sh` locally, and would re-base the 90.3% baseline for
  reasons unrelated to this phase.

</deferred>

---

*Phase: 45-harness-correctness*
*Context gathered: 2026-08-13*
