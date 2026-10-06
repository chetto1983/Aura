# Turn recall: one reading of the message picks the reasoning effort and preloads tools

Agreed with the operator on 2026-10-06. Today the reasoning effort is picked from a fixed seed bank. Tools
are found only when the model spends a round on `tool_search`. After this change, Aura reads each user
message once against two banks:

- its seed bank, in process;
- its own past turns, in the identity's ArcadeDB.

That reading picks the effort and loads the tools similar turns used. Aura learns from the turns it
serves and is never retrained. `tool_search` gains a dense leg beside BM25, if the measurement below
confirms it.

## Decisions

Stated by the operator:

- **One mechanism for the effort and for tools**: "they are very similar and reusable in one
  function". The same neighbour memory answers both.
- **A neighbour memory in ArcadeDB that learns from turns, with no training.** Fine-tuning was
  measured and dropped: it helped seen tools and hurt every unseen MCP family.
- **For tools, preloading and ranking.** Past turns preload the tools they used. `tool_search` adds
  a dense leg to BM25, but only if it measures better.
- **The fusion runs in process.** Tools are process state (registry and mounted MCP), not data.
- **Tool calls come from what ArcadeDB already records**: the reasoning graph's `ReasoningToolCall`.
  There is no new store.
- **Efforts come from the model.** The model's published set (`cfg.SupportedReasoningEfforts`, the
  same source as the cockpit's selector) is the only effort vocabulary. Every effort passes through
  `cfg.ClampReasoningEffort`, and this feature has no mapping table of its own.
- **Provider-agnostic, configured from the database.** Every LLM call uses the turn's own client on
  the route resolved from `aura.settings`. Nothing reads `.env`.
- **The teacher is the existing router prompt, asked synchronously and only on uncertain turns.**

Assumed during design:

- **Constants** (see Constants): margin 0.075, recall radius 0.10, 3 preloaded tools. They are code
  constants measured on 2026-10-06, like `tierNeighbours`, and are not environment knobs.
- **Two embeddings per turn.** The seed bank stays on raw text. Recall uses the document template
  that `ConversationTurn` vectors are stored in, embedded by the memory client so that the space
  stamp is the client's own.
- **The reasoning graph also records the tool calls of turns that exposed no reasoning.** Today it
  drops them.
- **The effort a turn ran with is persisted** in Postgres and projected onto `ConversationTurn`.

## What was measured

All of this was measured on 2026-10-06 against the lab VM. It used the VM's EmbeddingGemma sidecar
and its ArcadeDB 26.10.1. The live gate is 45 held-out prompts plus 13 distinct prompts of the VM's
real traffic (prd.md §6).

| | Gate (58) | VM traffic (15) |
|---|---|---|
| Centroid seed bank (before `4f8b3a0df`) | 49 | 8 |
| 3 nearest per tier, raw text (shipped) | 54 | 14 |
| The same on the document template | 56, one hard turn `none` | 15 |
| Teacher alone (router prompt, `gemma4:31b-cloud` from `aura.settings`) | 55, p50 515 ms | n/a |
| Seeds, plus the teacher when margin < 0.075 | 58, teacher on 18 of 58 | n/a |

- **The two methods fail on different prompts.** No prompt is wrong for both. The seed bank's four
  misses all have margins under 0.06, and the teacher gets each of them right. The teacher's three
  misses are scheduler tests, which the seeds get right with their widest margins (0.13-0.15).
- **The memory needs a distance bound.** A memory of judged turns, read within a distance bound and
  learning in order, recovered the VM's repeats without moving the gate. Unbounded, it learned the
  VM perfectly and dropped the gate to 39, with three hard turns sent to `none`.
- **The document template compresses similarity.** In that space, unrelated short prompts reach
  cosine 0.88-0.90, while repeats of the same request sit at 0.91-1.00. It also sends one hard turn
  to `none`. So the seed bank stays raw, and recall keeps a tight bound.
- **The graph already links turns to their tools.** It goes from a user `ConversationTurn` through
  `NEXT_TURN` to the answer, then `INITIATED_BY` to the trace, then `HAS_STEP` and `INVOKED` to
  `tool_name`. One query does the neighbour search and that traversal (shape below) in 7 ms.
- **The graph misses some tool turns.** It reaches 9 of the 16 turns that invoked a tool.
  `ReasoningTraceBuilder.CommitSourceTurn` keeps a trace only if the turn exposed reasoning, and keeps
  a step only if it has a summary. `validateReasoningText` rejects an empty summary. So a turn that
  ran at effort `none` and scheduled a reminder leaves no tool call in ArcadeDB.
- **Why feeds matter.** The learning plane deleted on 2026-08-02 had 11 of 12 tables empty. The
  composer's explicit effort was set in 0 of 13 VM conversations. Both feeds in this design exist on
  every turn that needs them:
  - the teacher answers every uncertain turn;
  - tool calls are recorded for every turn that ran a tool.
- **The dense leg for `tool_search` is promising but its fusion is untested.** BM25 replaced the
  former embedding ranker on 2026-07-31 (96% top-1 at 17 µs, against 50% at 330-443 ms). Raw
  EmbeddingGemma beats BM25 on top-1 (blind English 24 vs 18 of 30, held-out 18 vs 12 of 24), but
  the fusion of the two is unmeasured; see the ship gate.

## Shape

```
user message
 ├─ greeting allowlist ──────────────► effort none, nothing else runs
 ├─ embed raw text ──► seed bank, 3 nearest per tier ──► tier, margin
 └─ RecallTurns(text) in the identity's ArcadeDB ──► past user turns within 0.10:
                                                     the effort each ran with + its source,
                                                     the tools each turn ran
effort:  composer > labelled neighbour > seeds (margin ≥ 0.075) > teacher > seeds
tools:   nearest neighbour's tools ∩ registered deferred tools, at most 3, loaded before round 1
after:   effort + source ──► aura.conversation_turns ──► projection ──► ConversationTurn
         tool calls ──► reasoning graph (now also when the turn exposed no reasoning)
tool_search free text: BM25 ⊕ dense leg by reciprocal rank fusion (only if the gate passes)
```

## The shared function: `RecallTurns` (`internal/arcadedb/turn_recall.go`, new)

`func (c *Client) RecallTurns(ctx context.Context, text string) ([]RecalledTurn, error)` returns
the identity's nearest past user turns within the radius, nearest first.

`RecalledTurn` holds:

- `Distance float64`;
- `Effort string` and `EffortSource string`, both empty if the turn has none;
- `Tools []string`, the names of its successful tool calls.

It embeds `text` with `taskDocumentPrefix` through the client's own embedder, and filters by that
embedder's space. This is the read side of what `denseQueryVector` does for memory search. If no
embedder or no space is available, it returns no rows and no error, the same as when memory is off.

The query was verified on the VM against 26.10.1:

```sql
SELECT distance, reasoning_effort, reasoning_effort_source,
       @rid.out('NEXT_TURN').in('INITIATED_BY').out('HAS_STEP').out('INVOKED')[status = 'succeeded'].tool_name AS tools
FROM (SELECT expand(`vector.neighbors`('ConversationTurn[embedding]', :vector, 5,
      { maxDistance: :radius,
        filter: (SELECT @rid FROM ConversationTurn WHERE role = 'user' AND embed_space = :space AND deleted_at IS NULL).@rid })))
```

Three details of this query are load-bearing:

- **The traversal must start from `@rid`.** The expanded neighbour rows are projections, so the same
  traversal written without `@rid` returns null.
- **The `filter` needs 26.10.1.** Before 26.10.1, a filter matching nothing was treated as no filter
  (ArcadeData/arcadedb#8959). That would rank another space's vectors as soon as an identity had none
  in the current one. `minSecureVersion` (`internal/arcadedb/admin.go`) is raised to 26.10.1 in the
  same change, so an older server is refused rather than allowed to return mixed-space neighbours.
  The compose pin already ships 26.10.1 alongside the image, so the updater moves both together.
  26.10.1 also closes a critical advisory (GHSA-h2j4-28h8-cj5v).
- **The bound is enforced by the engine**, through `maxDistance`.

The agent sees a port with its own types, because `internal/agent` does not import `arcadedb`. The
runner binds it per identity with `TenantClients.Existing`, which never provisions on a read.

## Effort decision (`internal/agent/llm_agent_turn_reading.go`, new)

This runs once per turn, where `adaptiveReasoningTier` runs today (`llm_agent.go`), before the first
request. The first matching row wins:

| Situation | Effort applied | Source persisted |
|---|---|---|
| The composer set a fixed effort | that effort (already from the model's set) | `user` |
| The greeting allowlist matches | `none` | `greeting` |
| A neighbour within the radius carries a `user` or `teacher` source | its effort | `memory` |
| Seed margin ≥ 0.075 | the seed tier's effort | `seeds` |
| Seed margin < 0.075 and the teacher answers | the teacher tier's effort | `teacher` |
| The teacher fails, times out or answers invalid JSON | the seed tier's effort | `seeds` |
| No classifier, or the raw embed failed | static `low`, as today | `fallback` |

How the table applies:

- **Every effort is clamped.** Each one goes through `cfg.ClampReasoningEffort`. A tier becomes an
  effort only through the existing `ReasoningTier.reasoning` mapping.
- **Among neighbours, `user` beats `teacher`.** Of the neighbours inside the radius, the nearest
  with source `user` wins, otherwise the nearest with source `teacher`.
- **A copied label is never a label.** Rows whose source is `memory`, `seeds`, `greeting` or
  `fallback` are never used as labels. A turn decided by recall copies a label and does not create
  one, so the memory cannot reinforce its own guesses.
- **The teacher is extracted, not rewritten.** It is the router branch of `adaptiveReasoningTier`
  moved into a function. The no-classifier path calls the same function, so nothing is duplicated.
- **The classifier reports its margin.** `ReasoningClassifier.Classify` returns the margin that
  `semindex.Classifier.RankNearest` already computes.
- **Tools still load when the effort is fixed.** With a fixed effort, recall still runs for the
  tools.

## Tool preload

- **What loads.** Take the tools of the nearest neighbour that has any. Keep only registered,
  `Deferred` tools, drop `tool_search`, deduplicate in returned order, and keep the first 3.
- **How they load.** They are added to `a.activated` before the first `buildRequest`. This is the
  same promotion that `MetaActivatedTools` triggers after a `tool_search` call.
- **Effect on the prompt cache.** The tools array changes at round 1 instead of after a `tool_search`
  round, which is no worse than today.
- **Stale tools.** A tool that was unmounted since is filtered out by the registry check.

## Persisting what was learned

**The effort.**
- **The migration.** It takes the next free number at landing (`ls internal/db/migrations/`). It
  adds `reasoning_effort text` and `reasoning_effort_source text` to `aura.conversation_turns`. The
  source is checked against the six values in the table.
- **The write.** A sqlc query sets both on the user row. The runner writes them from the agent's
  reading in the commit path, before the conversation projection is offered. The projector's
  once-a-minute replay covers a late write.
- **The projection.** `ListProjectionTurns`, `ConversationTurnProjection` and the `ConversationTurn`
  upsert carry both fields. The ArcadeDB schema gains the two STRING properties. Postgres stays
  authoritative, so a rebuilt projection keeps every label.

**Tool calls.**
- **Recording.** `ReasoningTraceBuilder` keeps a step that has tool calls and no summary. A turn
  that exposed no reasoning but ran tools now yields a trace.
- **Validation.** `validateReasoningText` accepts an empty summary only on a trace or step that
  carries tool calls.
- **The embedding pass.** The trace pass gives an empty-summary trace a space stamp and no vector,
  as it does for text a space refused. Otherwise the pass would retry the row forever.
- **Retention.** Successful traces keep 30 days, so tool memory reaches 30 days back.
- **Deletion.** Conversation and identity deletion already remove these rows and vertices, and they
  also remove the labels.

## `tool_search` dense leg (`internal/agent/tools/search_dense.go`, new)

- **The index.** It covers the deferred tools: `name + summary + retrieval keywords` with the
  document template, in a `semindex.Ranker`. `Ranker` is already in the repo and has no production
  caller.
- **The query.** The model's free text, embedded with the query template.
- **Building it.** It is built on the first free-text search and rebuilt when the deferred set's
  names and summaries change. Builds are single-flighted, as the classifier's bank is.
- **Fusion.** `semindex.FuseRRF`, new and pure, adds 1/(60 + rank) over the BM25 and dense rankings.
  Ties keep BM25's order. The exact-name layer and `select:` are unchanged.
- **Failure.** If embedding fails, the result is today's BM25 result.
- **The embedder.** It is the one the reasoning classifier uses, wired in `cmd/aura`.

**Ship gate**, measured in Go on the sets already in `search_gate_test.go` and
`search_keywords_measure_test.go`:

- the production gate stays at 26 of 26, and its floors hold;
- the sum of blind-English and held-out top-1 rises above BM25's.

If either condition fails, the leg is not merged.

## Errors

- **The raw embed fails:** static `low`, recall skipped. This is today's behaviour.
- **Recall errors or passes 500 ms:** the turn continues with no memory, and Aura logs one warning
  per turn.
- **The teacher fails:** the seed verdict is used and no label is written.
- **The effort write fails:** a warning; the turn is unaffected.
- **The trace write fails:** behaviour is unchanged (PRD §10's queue).

## Constants

| Name | Value | Measured |
|---|---|---|
| `tierNeighbours` | 3 | already shipped |
| `teacherMargin` | 0.075 | gate 58/58 with 18 calls; chosen on the scored set |
| `recallRadius` | 0.10 cosine distance, document template | repeats at 0.91-1.00; unrelated prompts up to 0.90 |
| `recallNeighbours` | 5 | one VM request was sent 6 times, so unlabelled copies can sit ahead of a labelled one |
| `preloadMax` | 3 | the most distinct deferred tools one VM turn ran (`task`, `memory__memory_recall`, `whatsapp__search_contacts`) |
| `recallTimeout` | 500 ms | the query is 7 ms on the VM |
| teacher timeout | `reasoningRouterTimeout` (at most 2 s) | existing |

## Observability

- **The log line.** "adaptive reasoning: tier applied" gains four fields: `source`, `margin`, the
  distance of the recalled neighbour, and the preloaded tools.
- **The persisted source.** It answers the question PRD §6 leaves open, how often the teacher fires:
  `select reasoning_effort_source, count(*) from aura.conversation_turns where role = 'user' group by 1`.

## Testing

**Unit tests (no daemon):**
- `semindex.FuseRRF`, as table tests.
- The classifier's margin.
- One test per row of the effort table:
  - the teacher is called only below the margin, and only when no labelled neighbour exists;
  - a `memory`, `seeds`, `greeting` or `fallback` neighbour is never used as a label;
  - preloaded tools appear in the first request's tools;
  - unregistered names, non-deferred names and `tool_search` are dropped;
  - a fixed effort persists `user`;
  - a greeting runs neither embed nor recall.
- The runner writes the effort before the projection is offered.
- `RecallTurns`, using `recordingClient`: statement shape, space filter, radius and row parsing.
- The projection upsert carries the effort.
- Validation accepts a step with tool calls only.
- `tool_search` with a failing embedder returns exactly today's BM25 result.

**Integration tests:**
- `arcadedb_integration`, on a disposable database:
  - two user turns, and a trace whose step has tool calls and no summary;
  - recall from a paraphrase returns the effort and the tools;
  - a turn in another space is excluded, even when the current space has no rows;
  - a far turn is excluded.
- `db_integration`: the migration up and down; the write and the `ListProjectionTurns` read.
- `reasoning_live`: the gate is unchanged at 54/58.

**End to end on the lab VM** (Definition of Done, score above 9.8), with fresh prompts that are in
no corpus:

1. Turn A is uncertain. The teacher fires, its source is persisted, and the turn runs its tools,
   recorded with or without exposed reasoning.
2. After the projection, which takes at most a minute, turn B paraphrases A. It is decided by
   `memory`, its tools are preloaded, and the ledger shows no `tool_search` call.
3. After deleting A's conversation, recall no longer returns it.

Report how many `tool_search` calls each tool turn makes before and after the change, and the
teacher's share of turns.

## Out of scope

- training or fine-tuning;
- letting `tool_search` abstain;
- changing the teacher prompt;
- ArcadeDB RRF for tool ranking;
- memory shared across identities;
- per-identity seed banks.

## Rejected alternatives

- **A new lesson vertex type.** It would duplicate the content, embedding and tool calls that
  ArcadeDB already holds.
- **Tool names copied onto `ConversationTurn` from Postgres.** That duplicates the reasoning graph.
  The graph's gap is a defect to fix.
- **The seed bank on the document template.** It sends one hard turn to `none`.
- **A memory without a distance bound.** The gate drops to 39.
- **A background teacher.** The first occurrence of every new kind of turn would always be wrong.
- **Composer-only labels.** The composer's explicit effort was used in 0 of 13 conversations.

## PRD

Amendments follow PRD-first: each is written after the measurement it records.

- **§6:** the effort decision, its constants, the document-template measurement, and the graph's
  9-of-16 coverage, extended with the end-to-end result.
- **§10:** traces carry the tool observations of turns that exposed no reasoning.
- **ArcadeDB scope:** `ConversationTurn` gains projected fields and nothing else. The 2026-07-31
  "memory only" decision still holds.
