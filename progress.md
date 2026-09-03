# Progress

Last updated: 2026-09-03

This file is the durable handoff for the current work. Update it after each
meaningful implementation, verification, commit, or live-stack step so the work
survives context compaction.

The previous occupant of this file was the Phase 51 LLM-provider handoff. That
phase closed and its record lives in git history at `bdb3f6975^`.

## Objective

Bring the Aura memory MCP surface (`cmd/arcadedb-mcp`, `internal/arcadedb`) to
industrial quality: every tool exercised end to end against the running stack, in
every mode, with each defect reproduced before it is fixed and re-verified through
the real MCP surface afterwards — not only through Go tests.

## Non-negotiable constraints

- A defect is only a defect once it is reproduced on the live stack. A Go test
  against a fake server is not evidence; this whole task exists because
  `MergeEntities` had only that.
- Read the official manual before probing. It is cloned read-only at
  `D:/tmp/arcadedb-docs` (`ArcadeData/arcadedb-docs`, sources under
  `src/main/asciidoc/`). Three behaviours below were established by trial and were
  already written there.
- A probe measures ONE phrasing of a call, never the absence of a capability.
- No secret values in tests, CI, docs, or this file. The new live tests read no
  env of their own; they use the existing `arcadedb_integration` harness, so the
  CI tier needs no new variables.
- Never modify a test to make it pass unless the test itself encoded the defect —
  where that happened it is called out by name below.

## What was measured

Every tool in the surface was exercised against the live stack: `graph_schema`,
`memory_entities`, `memory_digest`, `memory_search`, `memory_facts_about`,
`memory_recall` (semantic, recent, open, scroll, reasoning), `memory_upsert_fact`,
`memory_batch` (upsert_fact, supersede_fact, merge_entities, forget),
`memory_merge_entities`, `memory_forget`, `memory_reembed`.

Eleven defects were found. Each was reproduced first, fixed, and re-verified
through the MCP tools against a rebuilt container.

| # | Defect | Reproduction |
|---|--------|--------------|
| 1 | `memory_merge_entities` failed for every merge that could matter. `SET n = properties(f)` copies `sources`, declared `LIST OF MAP`, and openCypher rejects a map-valued property assignment. Every fact written through the MCP carries provenance, and an entity with no facts has nothing to merge. | `http 400: TypeError: InvalidPropertyType` |
| 2 | `memory_batch` could not state an entity's POLE class. The tool input lacked the fields, and beneath it the batch's own state carried only the kind, so the store resolved every class with `poleClassFor("", kind)`. The bulk path — the one an import agent uses — was the only one unable to classify what it coined. | `Inspector` + explicit `Person` → `Other` |
| 3 | `poleByKind` omitted the reference model's own refinements. The file's doc comment named `Vehicle`, `Phone` and `PostCode` as canonical while the map held none of them. | fresh `Vehicle` and `City` → `Other`, with `Object`/`Location` empty |
| 4 | `memory_recall` `scroll` refused a cursor handed back on its own, though the tool documents the cursor as opaque. The caller had to repeat the conversation, anchor and direction the cursor already carried. | `recall cursor conversation mismatch` |
| 5 | Conversation paging never terminated. The page statements are inclusive and the next cursor pointed AT the last turn returned, so every page repeated its predecessor's boundary turn and, at the end of a conversation, the cursor was a fixed point. | turn 10 → turn 10, byte-identical `next_cursor` |
| 6 | Reaching the end of a conversation was reported as `conversation_anchor_not_found`, sending the caller to hunt a bug in an anchor that was fine. | — |
| 7 | `memory_reembed` with `all` could not pass its first batch. It selected `WHERE statement IS NOT NULL LIMIT batch`, a set re-embedding does not shrink, so it returned the same rows forever — on the one operation whose entire purpose is that no vector is left in the old geometry. | two calls on a 55-fact memory: `30`, `30` |
| 8 | `writeVectors` panicked when the embedder returned fewer vectors than texts, indexing positionally into the answer. A sidecar responding short crashed the process. | `index out of range [0] with length 0` |
| 9 | `memory_search` returned facts without `fact_key` on the hybrid path while the lexical path returned it. Whether a hit could be corrected with `supersedes_fact_key` depended on whether the embedder happened to be up. | — |
| 10 | A REFUSED supersede still coined its endpoints, against the function's own contract ("the new fact itself was not created"). The orphan landed in `memory_entities`, which exists to be read BEFORE a name is coined. | `RefusedProbe-Fantasma`, 0 facts |
| 11 | The same already-closed fact written twice was stored twice. A historical fact carries no `fact_key` (the unique index reserves it for the currently-valid version), so the provenance attach, which looked a fact up by key, never matched. Replaying a historical import multiplied rows and split each fact's provenance across the copies. | two edges, `as_of` returning the fact twice |

## Root cause of #1

`MergeEntities` had **no live test at all**. Its two unit tests asserted the Cypher
this package emits against a fake server that answers `{"moved":1}` to any
statement — precisely what `memory_integration_test.go`'s own header warns about.
`merge_live_integration_test.go` now runs it for real.

## What the manual already said

Consulted after the fact, and it should have been first:

- `reference/cypher/cypher-clauses.adoc` — "A property value must be a scalar or a
  list of scalars. A map value … is rejected with a `TypeError` and no part of the
  clause is written." Defect #1 was one page away.
- `reference/managing-dates.adoc` — the datetime format is `yyyy-MM-dd HH:mm:ss`
  **by default** and is changed with `ALTER DATABASE DATETIMEFORMAT`; `date()`
  converts with an explicit format. This is why datetime **equality** needs
  `date()` while the **range** operators coerce an RFC3339 string (which is why
  `as_of` always worked).
- `reference/sql/sql-methods.adoc` — `replace` is a string **method**
  (`<value>.replace(a, b)`), not a function. A probe returned "Unknown function
  name 'replace'" and that was written into a code comment as "SQL has no
  replace()". The probe measured the wrong shape of call; the comment overreached
  into absence and has been corrected.

## Design decisions worth keeping

- **The merge copies; it does not re-point.** SQL *can* move an edge's endpoint in
  place (`UPDATE FACT SET @out = …`) and both vertices' adjacency follows —
  strictly better, copying nothing. It only works on an **unindexed** edge type:
  with a single index on `FACT` the engine fails with `IllegalStateException:
  Cannot read original buffer`, and `FACT` carries three. Probed before writing,
  so the regression never shipped. Each fact now moves as one `sqlscript` —
  create then delete inside one transaction — because two round trips would leave
  a duplicated fact behind whenever the second did not happen.
- **The merge drops `embedding` deliberately.** The statement is rewritten, so the
  old vector describes the old wording. `memory_embed_backfill` selects on
  `embedding IS NULL` and recomputes it, which is the mechanism this codebase
  already built for exactly this gap.
- **`ReEmbedAllFacts` clears then drains** for the same reason: "has no vector" is
  the only predicate that shrinks as the work is done.
- **Minting moved past every refusal.** Classifying an endpoint is a pure decision;
  creating the vertex is a write, and a write must not happen before the call
  knows it will keep it.
- **Omission is not a mismatch.** `scroll` now takes the cursor alone and still
  refuses a request that *contradicts* it. The identity check stays unconditional:
  identity is host-derived, so refusing another identity's cursor is a tenancy
  boundary rather than a restatement.

## Verification

Run from `D:/Repo/Aura`.

- `go build ./...`, `go vet ./...`, `go test ./...` — whole module green.
- Live tier, both packages green:
  ```bash
  ARCADEDB_URL=http://127.0.0.1:2480 ARCADEDB_PASSWORD=… \
  ARCADEDB_DATABASE=aura_memory_it AURA_ARCADEDB_TENANT_SECRET=… \
  go test -tags arcadedb_integration ./internal/arcadedb/ ./cmd/arcadedb-mcp/
  ```
  `ARCADEDB_DATABASE` must name a database carrying the memory schema;
  `aura_memory_it` was created for this. Without it
  `TestMemoryVectorDegradesWithoutAnEmbedder` fails on a pre-existing fixture
  expectation, not on anything changed here.
- Race, native, in WSL: `CGO_ENABLED=1 go test -race ./internal/arcadedb/ ./cmd/arcadedb-mcp/` — green.
- `golangci-lint run ./internal/arcadedb/... ./cmd/arcadedb-mcp/...` — 0 issues.
- Container rebuilt (`docker compose build arcadedb-mcp && docker compose up -d
  arcadedb-mcp`) after each fix, and every fix re-verified through the MCP tools.
- Every new test was watched FAIL against the unfixed code before being kept —
  e.g. `re-embedded 3 of 7 facts`, `minted as "Other", want Person`,
  `the same closed fact was stored 2 times`.

## Dogfooding

The findings themselves were written into the live memory through the MCP, which
is the most realistic end-to-end exercise available: eleven facts about the tools,
the incidents, the ArcadeDB behaviours and the documentation location, all
classified through the POLE path that was just fixed (`Tool`→Object,
`Incident`→Event, `Milestone`→Event, `Officer`→Person, `Precinct`→Location).
Retrieval was then checked against that real content: a natural-language question
returned the right facts at ranks 1–2, each carrying its `fact_key`.

All synthetic probe entities were removed with `memory_forget`; the project
knowledge was kept.

## Files

Changed: `internal/arcadedb/{merge,memory,memory_pole,memory_batch,
memory_batch_state,memory_batch_store,memory_recall_browse,memory_vector,
memory_backfill,memory_provenance,write_retry}.go` and their tests;
`cmd/arcadedb-mcp/tool_memory_batch.go` and tests;
`docs/arcadedb-mcp-live-tools.json` (regenerated golden).

New: `internal/arcadedb/memory_fact_selector.go`,
`internal/arcadedb/merge_live_integration_test.go`,
`internal/arcadedb/memory_reembed_live_integration_test.go`,
`internal/arcadedb/memory_historical_live_integration_test.go`.

Tests rewritten because they encoded the defect, each justified in place:
`TestMemoryRecallLive_BrowseCursor` asserted the duplicate boundary turn;
`TestMergeEntitiesMovesBothDirectionsAndDropsSelfLinks` and
`TestUpsertFactCreatesEntitiesThenTheEdge` pinned statement positions rather than
invariants; the vector and supersede tool tests pinned a request order that the
deliberate reordering changed.

## Not claimed

- Pre-migration entities still report `pole: "Entity"`. They predate the classed
  schema; nothing here reclassifies them.
- The merge rewrites the **literal** entity name inside a statement. Merging two
  entities whose names do not appear verbatim in their prose leaves those
  statements describing the pre-merge world. Pre-existing, and correcting it would
  mean rewriting an author's sentences.
- `MENTIONS` being empty at boot is not a defect: the sweep is scheduled every 30
  minutes and had not yet ticked. Forced, it linked 6 edges.

## The skill and the prompt that drive these tools

`internal/skills/embed/memory-aura/SKILL.md` is what Aura reads before using this
surface, so a tool fix that leaves the skill describing the old behaviour is half a
fix. Every claim in it was re-checked against the live tools; two were wrong:

- It listed **`entity` as a `mode`** of `memory_recall`. It is a PARAMETER; the enum is
  `semantic, recent, open, scroll, reasoning`, and `mode: entity` is rejected before the
  call runs. Following the skill produced a failing call.
- It said the cursor needs its `direction` repeated. It does not any more, and it never
  said that paging terminates when `next_cursor` stops coming.

Claims that were checked and held, so they stayed: `source: {}` is complete; exact
replay attaches rather than duplicating; the prose guard rejects a sentence-shaped
object (`fact object reads as prose`) but passes a tidy phrase like
`11ms single query latency on RTX A2000`; a `memory_forget` matching nothing returns a
plain `0` rather than an error — though the same forget inside a `memory_batch` ABORTS
the batch, which the skill now says.

Added, because the tools have it and the skill did not: the POLE class model
(six classes, `subject_pole`/`object_pole`, `kind` as the refinement, the class decided
once at first write), `memory_reembed` (absent entirely), the batch's idempotency
semantics, and a map of all eleven tools.

Rewritten deepest: **conversations and reasoning traces**, which are the powerful half
and were four lines. A trace opened by `trace_id` returns its ordered `steps` and, on
the steps that made them, the `tool_calls` — `tool_name`, `status`, `duration_ms`, an
`observation`, `artifact_refs`, and an `argument_digest` which is a hash, so the
arguments themselves were never stored and cannot be reported. Traces carry `expires_at`
and are gone after thirty days, where facts are not. A trace also carries
`conversation_id` and `turn_seq`, and a turn carries the matching `source_ref`, so the
two halves join: *why* from the trace, *what was said* from `open`/`scroll` on that
conversation, *what was concluded* from the facts. That pivot is now the skill's centre.
`turn_seq` is not contiguous (a real conversation runs 1, 2, 3, 6, 7, 10), which the
skill now warns about.

**The skill was available and did not fire.** Read back from the live database after the
rebuild (conversation `01a06705`, 11:27-11:30): asked "what do you know about me", Aura
answered from the bare tools; asked "why don't you use the memory skill", it conceded the
skill "was installed and present in my capability list from the start of the session" and
that it "chose the shorter way instead of the correct one". Two turns earlier, asked how
its reasoning trace works, it wrote an essay about thinking and acting — and asserted "it
is not a tool I call, it is the way I think", which is false here.

That settles a design question rather than opening one. The first draft of this work added
a third mention of the skill to the always-present system prompt; it was reverted on the
argument that skills are advertised to the model by name and description, so the skill is
reachable on its own. The measurement says the advertisement was not ENOUGH, not that the
layering was wrong — and the fix belongs where the trigger lives. So the skill's
description now names the situations it must fire in (someone asks what you know or
remember, someone reveals something durable, a stored fact is wrong, a question is about an
earlier conversation or an earlier line of reasoning) and says plainly that the tools will
answer without it and that this is the trap. The system prompt stays untouched.

The same read also proves it is a triggering gap and not a capability one: on 2026-09-01,
pushed with "I have never seen you use it", Aura DID open `mode: reasoning` and used it
well, showing the operator its own recorded struggle to guess the batch tool's keys. Same
question two days later, no push, and it described the mechanism instead. Both failure
modes are now named in the skill by what they look like from the outside.

**One layer owns the doctrine.** The per-turn `<memory_context>` pointer
(`cmd/aura/serve_memory_context.go`) used to restate `depth 2` semantics and advertise
the skill as covering "writing and correcting" — which undersells it, so an agent with a
question about a reasoning trace had no reason to load it. It now carries only what
cannot live in a lazily-loaded skill: the memory's shape, the three-way read routing
(whose failure needs no tool call — an agent that does not know `recent` exists answers
"I recall nothing"), and an accurate name for the skill. A test pins that it does not
restate `depth`, `cursor`, `supersede` or the class names.

The system prompt was left alone. Naming the skill there too was drafted and reverted:
skills are advertised to the model by name and description, so `memory-aura` is already
reachable from its own frontmatter, and a third mention would have been coordination
to maintain rather than doctrine to follow.

Both containers were rebuilt, and the rewritten skill is materialized inside the
running `aura` container at `/var/lib/aura/skills/memory-aura/SKILL.md`.

## The surface Aura actually holds

The deepest finding came last, and it explains the behaviour above better than any
discipline argument does. `memoryHiddenFromModel` did not DEFER eight of the eleven memory
tools — `bridgeToolsWithPolicy` skipped them, so they were absent from the model's world
and `tool_search` could not reach them either. The agent held exactly `memory_recall`,
`memory_upsert_fact` and `memory_batch`.

So the skill's central instruction — read `memory_entities` before any write — named a tool
that did not exist for Aura, and the per-turn pointer routed to `memory_facts_about` and
`memory_search`, both absent. Answering everything with `memory_recall` was not laziness;
it was the only surface it had. Its apology for "choosing the shorter way" was an apology
for a constraint.

On the operator's decision the hiding is gone. Deferral now chooses what rides in a
manifest; it never chooses what exists:

- **All 11 memory tools are bridged.**
- **Four ride in every turn**: `memory_recall`, `memory_upsert_fact`, `memory_batch`,
  `memory_entities` — the ones with no substitute. `memory_recall` already answers what
  `memory_facts_about` and `memory_search` answer (its `entity` parameter takes the graph
  path, its `query` the hybrid one), `memory_batch` subsumes forget and merge, and the
  runner injects the digest; nothing at all covered "which names already exist".
- **Seven are deferred**, one `tool_search` away, exactly like `web_search`.

Mechanically this needed per-tool deferral (`bridgePolicy.deferredTool`) and a slot
arithmetic that weighs the manifest core rather than the whole surface
(`bridgePolicy.manifestCount`), with `maxAlwaysLoadedMCPTools` moved 3 → 4 to admit the
fourth. The slot outcome is unchanged: calendar (1) and memory (4) hold the two slots.
Verified on the rebuilt container:

    mcp always-loaded slot granted  namespace=memory model_facing=4 slots_remaining=0
    mcp live mounted                server=memory tools=11

The skill and the pointer now say which four are present and that the rest need loading,
so neither names a tool the agent cannot reach.

## Next actions

1. Commit as six atomic changes: merge · POLE classing · recall paging · re-embed and
   `fact_key` projection · refusal and historical dedupe · skill and pointer.
2. Coverage gate and a mutation spot-check on the critical new file before closing.
