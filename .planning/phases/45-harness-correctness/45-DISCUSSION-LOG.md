# Phase 45: Harness correctness - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-08-13
**Phase:** 45-harness-correctness
**Areas discussed:** Replay key discriminator, Ordinal plumbing, Swarm key, Policy value, Replay marker, Same-message duplicates, Id machinery scope, Fact target, MEM-05 junk nodes, MEM-04 entity identity, Turn honesty, Reply rules, Boot guard, Refusal shape, Coverage tier, Misdiagnosis correction

**Standing directive from the user, given at area selection:** *"reserch hermes no bespoke"* —
research the on-disk reference implementation and adopt its shape rather than inventing. Applied to
every area below; where hermes has no counterpart, that absence is recorded explicitly.

---

## Area selection

| Option | Description | Selected |
|--------|-------------|----------|
| Replay key discriminator | What is folded into the child operation key | ✓ |
| Which tools re-execute | Blast radius of `ReplayReissueExecutes` | ✓ |
| Same-message duplicates | Where id repair and in-message dedupe land | ✓ |
| Naming the fact to close | HARN-04 + MEM-04/05 | ✓ |

**User's choice:** all four, plus the free-text directive *"reserch hermes no bespoke"*.
**Notes:** A fifth area (HARN-06/07 turn honesty + ACC-01/02 evidence) was offered in the preamble
and initially left to be captured without discussion; the user later opened it. Four further areas
were surfaced afterwards and all four were opened.

---

## Replay key discriminator

| Option | Description | Selected |
|--------|-------------|----------|
| Ordinal only | `modelRound.ordinal` in the child key, `requestID` excluded; survives a scheduler reclaim | ✓ |
| Ordinal + requestID | Strictly per-turn; `ic.RequestID` is fresh per turn, so a reclaim re-executes every side effect — breaks HARN-02 | |
| Per-(tool,args) occurrence | A new in-process counter following the `budget_dedup.go` fingerprint precedent | |
| Hermes shape: no replay | Retire tool-result replay entirely; at-most-once at the parent claim only, as hermes does | |

**User's choice:** Ordinal only.
**Notes:** Grounded on two reads made before the question: `model_round.go` already mints the
ordinal and `llm_agent.go:346` already reuses it verbatim on a transport retry, which closes
ROADMAP §45's "confirm the round-ordinal shape against Aura's own dispatch loop before building".
The hermes option was put on the table honestly because the directive demanded it, with its real
cost stated — hermes has no per-tool-call registry at all (`tool_guardrails.py:20-38` is a
read-only loop-guard list; `cron/scheduler.py` holds at-most-once at the job claim), so adopting it
would require rewriting HARN-02 rather than satisfying it.

---

## Ordinal plumbing

| Option | Description | Selected |
|--------|-------------|----------|
| On `ic.Ctx` before dispatch | One line re-pointing `ic.Ctx` through the existing `withModelRound`; accessor already exists | ✓ |
| Explicit parameter | Thread `modelRound` through four signatures and ~30 test call sites | |

**User's choice:** On `ic.Ctx` before dispatch.
**Notes:** The question existed because of a read: `llm_agent.go:355` puts the round on `spanCtx`
while `llm_agent_dispatch.go:109` dispatches on `ic.Ctx`, so the tool path never sees it today.
Fail-closed on a missing ordinal was taken as an implementer decision rather than asked.

---

## Swarm key

| Option | Description | Selected |
|--------|-------------|----------|
| Ordinal only — stay in scope | Concurrent-worker key collision stays as today; recorded for Phase 51 / SWARM-07 | ✓ |
| Ordinal + Branch | One extra field; closes a silent-write-loss hole, but touches a Phase 51 requirement | |

**User's choice:** Ordinal only.
**Notes:** Scope control over opportunistic fixing. The hazard is real — `WithSubAgent` keeps the
same `RequestID` and each child runs its own ordinal from 1 — but it is pre-existing and unchanged.

---

## Policy value

| Option | Description | Selected |
|--------|-------------|----------|
| Drop it — one value + boot guard | The ordinal subsumes it; extend `ValidateClassifiable`; amend ROADMAP §45/§46 | ✓ |
| Ship it — narrow opt-in list | `ReplayReissueExecutes` on shell_exec/fs_write/fs_edit/memory_forget/etc; relax `reserve.go:43` | |
| Ship it — default for all mutating | Maximal hermes alignment; HARN-02 then rests on Layer A + the parent claim only | |

**User's choice:** Drop it — one value + boot guard.
**Notes:** The roadmap's headline deliverable for this phase, dropped on evidence. With the ordinal
in the key a cross-round re-issue already executes for every mutating tool; a second value could
only mean "never replay even on a genuine reclaim", which is HARN-02's opposite. Requires two
amendment commits before any code (ROADMAP §45's vocabulary claim, §46's dependency), which is the
PRD-first principle running as intended rather than a workaround.

---

## Replay marker

| Option | Description | Selected |
|--------|-------------|----------|
| Preview text + OTel span attribute | Marker in both replay paths, `aura.tool.replayed` / `replay_layer`; no migration | ✓ |
| Preview text only | ~6 lines, no machine-readable trace anywhere | |
| Text + OTel + a `replay` ledger row | Fullest audit; needs a CHECK relaxation, a shape arm, and a discriminator in the unique index | |

**User's choice:** Preview text + OTel span attribute.
**Notes:** Asked after establishing that a replayed call writes **no row** today — Layer B returns
before `reserve` is called, and Layer A's `rows==0` is the insert that did not happen — so the table
ACC-02 names as the evidence source is currently blind to replays.

---

## Same-message duplicates

| Option | Description | Selected |
|--------|-------------|----------|
| Agent loop, hermes' two sites | Uniquify right after `consume`; dedupe immediately before the `:546` history append | ✓ |
| `internal/llm` wire boundary | Ids repaired in the provider client; splits the pair across two packages | |

**User's choice:** Agent loop, hermes' two sites.
**Notes:** hermes puts both in `conversation_loop.py`, not a provider adapter. Dropping before the
assistant message is built is what keeps the provider invariant (*"each tool_call must have a
matching tool result and vice versa"*) intact with no synthesized result.

---

## Id machinery scope

| Option | Description | Selected |
|--------|-------------|----------|
| Collision repair + blank-id fallback | `<id>_d<n>` plus `call_<sha256("name:args:index")[:12]>`; closes a latent Layer-A collapse | ✓ |
| Collision repair only | Exactly HARN-08's words; leaves the blank-id case latent | |

**User's choice:** Collision repair + blank-id fallback.
**Notes:** The blank-id case was found by reading the schema, not the requirement:
`UNIQUE (conversation_id, request_id, tool_call_id, event_kind)` means two blank-id calls in one turn
collapse onto one reservation. hermes' composite `call_x|fc_y` splitting was deliberately not
adopted — Codex-specific, dead code here.

---

## Fact target

| Option | Description | Selected |
|--------|-------------|----------|
| Expose `fact_key` + refuse ambiguity | Surface the identifier that already exists; legacy `supersedes:true` refuses on ≠1 and returns candidates | ✓ |
| `supersedes_object` + refuse ambiguity | Name the old value instead of a handle; model must reproduce a sentence exactly | |
| Predicate-cardinality registry | Declare single- vs multi-valued predicates; a new list to maintain, and blind to fuzzy subject resolution | |

**User's choice:** Expose `fact_key` + refuse ambiguity.
**Notes:** `factIdentity` already produces a deterministic content-derived key, already stored on
every edge, already NULLed on close so it can only name a still-valid fact — and
`closeSupersededStatement` already carries `AND fact_key <> :fact_key`. The refusal contract is
hermes' `memory_tool.py` shape verbatim. Pitfall 6's preferred direction, satisfied with no new
machinery.

---

## MEM-05 junk nodes

| Option | Description | Selected |
|--------|-------------|----------|
| Validation guard only | Object must name an entity, not prose; no migration, no backfill; existing junk untouched | ✓ |
| Data-model change + backfill | Object as an edge property; rewrites every projection, backfills every identity database | |
| Defer MEM-05 to Phase 49 | Ship nothing here; requires amending ROADMAP §45's bundling rationale | |

**User's choice:** Validation guard only.
**Notes:** Forward-looking prevention, not remediation. Existing junk vertices are recorded as
deferred cleanup.

---

## MEM-04 entity identity

| Option | Description | Selected |
|--------|-------------|----------|
| Canonicalize the operator's subject host-side | In `memoryUpsertFactHandler`, covering bridge + CLI + host paths; no schema change | ✓ |
| General Entity alias mechanism | Alias property or `ALIAS_OF` edge with resolution on every path; solves more than MEM-04 asks | |

**User's choice:** Canonicalize the operator's subject host-side.
**Notes:** Placed in the MCP handler rather than the bridge specifically because the bridge would
miss the CLI and host-driven paths. Existing split entities need a merge sweep, deferred.

---

## Turn honesty (HARN-06)

| Option | Description | Selected |
|--------|-------------|----------|
| Both: widen the trigger and add the honest-failure nudge | Drop `!a.sideEffected`; raise the veto budget to 2 with a second nudge demanding honesty | ✓ |
| Widen the trigger only | One condition changed; the second branch left to the prompt | |
| Honest-failure nudge only | Cheapest; leaves the no-side-effect case uncovered, which is HARN-06's own shape | |

**User's choice:** Both.
**Notes:** The critic already existed with the right prompt (*"a script that was written but never
executed ... is NOT done"*); the gap was its trigger. hermes has no equivalent check at all.

---

## Reply rules (HARN-07)

| Option | Description | Selected |
|--------|-------------|----------|
| System prompt only, mirror the user | Static `messages[0]` rule; hermes parity; verified live, not gated | ✓ |
| Prompt rule + the critic grades it | Enforced, but widens a deliberately narrow gate | |
| Configured operator language | Deterministic per deployment; wrong the moment the operator switches language; no precedent in either reference | |

**User's choice:** System prompt only, mirror the user.
**Notes:** hermes' only language rule anywhere is one prompt line in its context compressor. No
detection or enforcement exists in either codebase.

---

## Boot guard

| Option | Description | Selected |
|--------|-------------|----------|
| Panic, same as today | Keep `ValidateClassifiable`'s existing posture; unreachable from the bridge today | ✓ |
| Refuse to register that one tool | Fail-soft like MCP mounts; splits the guard into two behaviours | |

**User's choice:** Panic, same as today.
**Notes:** `applyMCPOperationMetadata` fills all three fields unconditionally, so the collision with
fail-soft MCP mounting is latent rather than live; Phase 46's `bridgePolicy` overrides are what
could make it reachable, and that constraint travels with Phase 46.

---

## Refusal shape

| Option | Description | Selected |
|--------|-------------|----------|
| Successful call, refusal payload | hermes' shape; `refused`/`reason`/`candidates`, normal `end` row | ✓ |
| Typed `ToolCallError` with `Effect: none` | Uses Aura's existing `DeterministicNoEffect` → `RejectOperation` path; durable rejection record | |

**User's choice:** Successful call, refusal payload.
**Notes:** The rejected option was a genuine contender — Aura already has the typed path and
`decodeToolCallError` already special-cases rejections. It lost because a clean effect-free refusal
would be recorded as a failed mutation, and the candidate list would have to ride an error string.

---

## Coverage tier

| Option | Description | Selected |
|--------|-------------|----------|
| Add tests to the existing leg, leave the floor alone | New tests join `arcadedb_live`; `AURA_COVERAGE_TAGS` unchanged; pure logic also unit-tested daemon-free | ✓ |
| Fold `arcadedb_integration` into the floor | Closes the runs-but-feeds-no-coverage split; needs live ArcadeDB + embed + MCP in every coverage run | |

**User's choice:** Add tests to the existing leg, leave the floor alone.
**Notes:** This question was asked on a false premise first. CLAUDE.md states the
`arcadedb_integration` tier is run by *"not CI, not the Makefile, not the coverage scripts, not even
`go vet`"*; `scripts/agent_memory_eval.py:52-55` runs it with `-race` and a coverage profile, driven
by `make agent-memory-eval` from CI job `arcadedb-integration-test`. The claim was corrected before
the options were presented, and correcting CLAUDE.md is recorded as fix-on-touch.

---

## Misdiagnosis correction

| Option | Description | Selected |
|--------|-------------|----------|
| Phase 45, as the fix's own live proof | Correcting the orphan-nodes fact exercises the whole chain; scope is that one fact | ✓ |
| Phase 54 with the rest | One sweep alongside SURF-05/ACC-03; the false fact keeps influencing recall for eight phases | |

**User's choice:** Phase 45, as the fix's own live proof.
**Notes:** Explicitly bounded to the single fact F-1 caused. The nine compensating `learned_lesson`
facts and the `always-deliver-files` skill stay with Phase 54.

---

## Claude's Discretion

The user delegated nothing explicitly; these were taken as implementer decisions and recorded in
CONTEXT.md rather than asked:

- Fail-closed behaviour when `modelRoundFromContext` returns `ok=false` (consistent with the
  codebase's fail-closed-on-wiring posture).
- Draining the scheduler before deploying the key-shape change, recorded as a deploy note.
- Exact wording of `replayedMarker`, the second critic nudge, and the system-prompt language rule.
- The precise `looksLikeProse` predicate and the canonical form for the operator's subject.
- Whether the key `Version` string moves to `tool-child-v2` (the fingerprint changes either way).
- Treating the two fix-on-touch items (`internal/toolinvocations/redact.go:23`, and CLAUDE.md's
  stale `arcadedb_integration` claim) as in-commit corrections per CLAUDE.md's fix-on-touch rule.

## Deferred Ideas

- `Branch` in the child operation key → Phase 51 / SWARM-07.
- MEM-05 data-model change (object as edge property) + backfill → Phase 49.
- General `Entity` alias mechanism → Phase 49.
- A `replay` `event_kind` row in `aura.tool_invocations` → unscheduled.
- A sweep for existing junk vertices and already-split operator entities → Phase 49.
- `ReplayReissueExecutes` — unbuilt, not deleted; reopening also means amending HARN-02.
- Folding `arcadedb_integration` into `AURA_COVERAGE_TAGS` → unscheduled.
