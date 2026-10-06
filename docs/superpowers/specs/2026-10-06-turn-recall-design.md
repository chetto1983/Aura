# Turn recall: one decision path picks the reasoning effort and preloads tools

Agreed with the operator on 2026-10-06. Today the reasoning effort is picked from a fixed seed bank. Tools
are found only when the model spends a round on `tool_search`. After this change, Aura reads each user
message through one decision path against two banks:

- its seed bank, in process;
- its own past turns, in the identity's ArcadeDB.

That reading picks the effort and loads the tools similar turns used. Aura learns from the turns it
serves and is never retrained. `tool_search` gains a dense leg beside BM25, if the measurement below
confirms it.

Revised on 2026-10-06 after a static review of the design, the current code and primary sources.
The measurements below are retained as reported; this revision did not rerun the VM experiments.
The corrected contracts are implementation requirements, not newly measured results. Production
readiness requires the independent evaluation and live checks in Testing.

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

Design requirements to validate:

- **Candidate constants** (see Constants): margin 0.075, recall radius 0.10, 3 preloaded tools.
  These are starting values from the reported experiments, not independently validated cutoffs.
- **Up to two embeddings for the turn decision after index warm-up.** The seed bank stays on raw
  text. Recall uses the document template that `ConversationTurn` vectors are stored in, embedded
  by the memory client so that the space stamp is the client's own. Later free-text tool searches
  and cold index builds add their own embedding work.
- **The reasoning graph also records the tool calls of turns that exposed no reasoning.** Today it
  drops them.
- **The effort a turn ran with is persisted** in Postgres and projected onto `ConversationTurn`.
- **One retrieval function, distinct eligibility rules.** Effort labels and successful tool-use
  examples use the same query vector but separate candidate pools, filtered before the top-k cap.
- **Context and provenance bound reuse.** A matching message alone never authorizes copying an
  effort. Context, route and policy compatibility are checked before retrieving effort labels.

## What was measured

The following results were reported on 2026-10-06 against the lab VM, using its EmbeddingGemma
sidecar and ArcadeDB 26.10.1. The 58-case development gate comprises 45 originally held-out prompts
plus 13 distinct prompts of the VM's real traffic (prd.md §6). Once used to choose seeds or
thresholds, these cases are development/regression data, not an independent final test.

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
  `tool_name`. The historical query below took 7 ms on that corpus. This excludes query embedding,
  client acquisition and the corrected candidate-pool filtering; it is not a turn-latency bound.
- **The graph misses some tool turns.** It reaches 9 of the 16 turns that invoked a tool.
  Code review also found an earlier loss: `ObserveToolInvocation` rejects an event when `runID`
  or the first step has not been initialized by `ObserveReasoning`. `CommitSourceTurn` then drops
  empty summaries, and validation rejects them. All three stages must support tool-only traces.
- **Why feeds matter.** The learning plane deleted on 2026-08-02 had 11 of 12 tables empty. The
  composer's explicit effort was set in 0 of 13 VM conversations. The design attempts the teacher
  when no compatible label exists and the classifier is uncertain or absent, and observes tool
  calls independently of exposed reasoning. Teacher/persistence failures and coverage must be
  measured; neither feed is assumed complete.
- **The dense leg for `tool_search` was measured offline, fusion included.** BM25 replaced the
  former embedding ranker on 2026-07-31 (96% top-1 at 17 µs, against 50% at 330-443 ms). The
  measurement below used raw EmbeddingGemma over BM25's own retrieval document (`searchDocument`),
  with the document template, and reciprocal rank fusion with k = 60:

  | Set | BM25 top-1 / r@5 | Dense | RRF |
  |---|---|---|---|
  | Production gate (26) | 26 / 26 | 23 / 26 | 26 / 26 |
  | Held-out (24) | 12 / 19 | 18 / 22 | 17 / 22 |
  | Blind English (30) | 18 / 23 | 24 / 30 | 21 / 30 |
  | Blind Italian (30) | 19 / 19 | 21 / 27 | 21 / 28 |

  Fusion keeps the production gate whole, which dense alone does not, and lifts every other set.
  These sets now serve as regression data. The ship gate repeats the measurement in Go and adds
  a frozen, independent test set.

What these measurements do not establish:

- `teacherMargin = 0.075` was selected on the same 58 cases that scored 58/58. This is a
  calibration result, not an estimate of production accuracy or of the production teacher rate.
- The teacher was measured on one route and model. A teacher prediction and a user's chosen
  effort are routing evidence, not proof of the minimum effort needed for a correct answer.
- The document-template similarities have little separation: unrelated prompts reach 0.90.
  Radius 0.10 alone cannot establish that context, intent or effort requirements match.
- Neither end-to-end latency, cold index builds, cache effects nor the revised retrieval pools
  have been measured here. An E2E demonstration is necessary but not sufficient.

## Shape

```
user message + prior context + immutable route snapshot
 ├─ standalone greeting ────────────► fixed composer effort or none; no embeddings/recall
 ├─ embed raw text ──► seed bank, 3 nearest per tier ──► tier, margin
 └─ RecallTurns(request) ──► one document query vector, three bounded eligible pools:
                            user labels / teacher labels / successful tool-use turns
effort:  composer > compatible label > seeds (margin ≥ 0.075) > teacher > seeds
tools:   nearest compatible tool turn's registered deferred tools, at most 3 before round 1
after:   decision + provenance ──► aura.conversation_turns ──► projection ──► ConversationTurn
         tool calls ──► reasoning graph (now also when the turn exposed no reasoning)
tool_search free text: BM25 ⊕ dense leg by reciprocal rank fusion (only if the gate passes)
```

## The shared function: `RecallTurns` (`internal/arcadedb/turn_recall.go`, new)

`func (c *Client) RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error)`
returns the identity's nearest eligible past user turns within the radius.

`TurnRecallRequest` carries `Text`, `ContextKey`, `RouteKey`, `PolicyVersion`, the current
`SourceRef` to exclude self-matches, and the names of currently registered deferred tools eligible
for preload. `TurnRecall` has separate `UserLabels`, `TeacherLabels` and `ToolTurns` slices,
nearest first within each slice. `IncludeLabels` is false for a fixed composer effort, which
requests only the tool pool.

`RecalledTurn` holds:

- `Distance float64`;
- `SourceRef string`, identifying the authoritative user turn;
- `Effort string`, `RequestedEffort string` and `EffortSource string`, empty if absent;
- `ContextKey`, `RouteKey`, `PolicyVersion` and `OriginRef`, for compatibility and audit;
- `Tools []string`, the names of its successful tool calls.

It embeds `request.Text` once with `taskDocumentPrefix` through the client's own embedder and
uses that vector for all pools. It checks the space before and after embedding, validates the
vector and filters by that space, following `denseQueryVector`'s space-change checks. If no
embedder is configured, it returns no rows and no error, the same as when memory is off. A failed
embedding is an error: the agent logs it once and reads the turn without memory.

This historical probe was reported as verified on the VM against 26.10.1. It demonstrates the
traversal, but is NOT the production retrieval contract: its unfiltered top-5 can hide labels
behind copies and does not enforce context or route compatibility.

```sql
SELECT distance, effort, effort_source,
       @rid.out('NEXT_TURN').in('INITIATED_BY').out('HAS_STEP').out('INVOKED')[status = 'succeeded'].tool_name AS tools
FROM (SELECT expand(`vector.neighbors`('ConversationTurn[embedding]', :vector, 5,
      { maxDistance: :radius,
        filter: (SELECT @rid FROM ConversationTurn WHERE role = 'user' AND embed_space = :space AND deleted_at IS NULL).@rid })))
```

Production retrieval must apply these predicates BEFORE each pool's top-k selection:

| Pool | Additional eligibility beyond user role, matching space and not deleted |
|---|---|
| User labels | Matching context, route and policy; source `user`; complete requested/applied effort provenance |
| Teacher labels | Same compatibility checks; source `teacher`; complete requested/applied effort provenance |
| Tool turns | Matching context; at least one successful call to a currently eligible deferred tool |

Exclude the current `SourceRef` from every pool. `memory`, `seeds`, `greeting` and `fallback`
never enter either label pool. Five closer unlabelled copies therefore cannot evict a teacher
label. Unmounted or always-loaded tools cannot fill the tool pool with unusable candidates.
Use ArcadeDB's native filter and bounded vector search for each pool, sharing the vector and
deadline. One SQL statement is not required. The exact revised SQL and its latency must be
verified on 26.10.1 before recording them as measured; the historical 7 ms is not carried forward.

The projection uses `effort`, `effort_source` and the provenance fields listed under Persisting
what was learned.
It stores routing metadata only, with no chain-of-thought in the conversation projection.

The engine and client constraints are:

- **The traversal must start from `@rid`.** The expanded neighbour rows are projections, so the same
  traversal written without `@rid` returns null.
- **The `filter` needs 26.10.1.** Before 26.10.1, a filter matching nothing was treated as no filter
  (ArcadeData/arcadedb#8959). That would rank another space's vectors as soon as an identity had none
  in the current one. `minSecureVersion` (`internal/arcadedb/admin.go`) is raised to 26.10.1 in the
  same change, so an older server is refused rather than allowed to return mixed-space neighbours.
  The compose pin already ships 26.10.1 alongside the image, so the updater moves both together.
  26.10.1 also closes a critical advisory (GHSA-h2j4-28h8-cj5v).
- **The version floor is wired for the first time.** `VerifySecureVersion` has no production
  caller today, so the floor CLAUDE.md describes is not enforced anywhere (found 2026-10-06 while
  writing this plan). `TenantClients.For` must verify the server once, before a tenant client is
  first returned, and cache the success. The check sits there rather than at boot, so a slow ArcadeDB
  start is a retried read and not a failed boot.
- **The bound is enforced by the engine**, through `maxDistance`.

The agent sees a port with its own types, because `internal/agent` does not import `arcadedb`. The
runner binds it per identity with `TenantClients.Existing`, which never provisions on a read.

## Compatibility and label provenance

- **Context key.** Compute a deterministic digest of the task-bearing context before the current
  user message: ordered prior messages including tool calls/results, attachment and artifact
  references with content versions, and effective governing instructions. Exclude the current
  message text (so paraphrases can match) and transport-only request/trace IDs. Do not discard
  prior content using a heuristic that declares a short message self-contained. Missing context
  or an unversioned referenced input makes that turn ineligible for recall and reusable labels.
  A known empty history has a valid digest; missing/unavailable history does not.
- **Conservative first release.** Context keys must match exactly for labels and tool examples.
  New conversations with equal prior context can share examples; continuations with different
  histories cannot. This deliberately reduces recall coverage. Context-sensitive paraphrases
  with different histories require a separately measured policy before broadening reuse.
- **Route key.** Derive a credential-free digest from the turn's immutable runtime snapshot:
  provider/endpoint identity, model identifier and exposed revision, supported efforts and
  mandatory-reasoning flag. A label from a different route is not eligible. Mutable model aliases
  are not proof of a stable backend; a known backend change invalidates the policy version and
  requires revalidation. Tool examples do not require a matching LLM route.
- **Policy version.** Version the seeds, tier mapping, teacher prompt, context-key format and
  selection rules together. Incompatible or missing versions cannot provide effort labels.
- **Requested versus applied.** Preserve the requested effort before `ClampReasoningEffort`
  alongside the effort actually sent. Reuse a compatible label's requested effort, then apply the
  current clamp. Never infer the original decision from a previously clamped value.
- **Origin.** A `user` or `teacher` label points to its own source turn. A `memory` decision records
  that original source for audit but remains ineligible as a new label. Failed teacher calls and
  unsuccessful/repudiated attempts produce no reusable teacher label. Neither label source
  certifies downstream answer quality; the evaluation checks that separately.

## Effort decision (`internal/agent/llm_agent_turn_reading.go`, new)

This runs once per turn, where `adaptiveReasoningTier` runs today (`llm_agent.go`), before the first
request. The first matching row wins:

| Situation | Effort applied | Source persisted |
|---|---|---|
| The composer set a fixed effort | that effort (already from the model's set) | `user` |
| A standalone greeting matches the allowlist | `none` | `greeting` |
| A compatible neighbour has a complete `user` or `teacher` label | its requested effort, clamped now | `memory` |
| Seed margin ≥ 0.075 | the seed tier's effort | `seeds` |
| Seed margin < 0.075 and the teacher answers | the teacher tier's effort | `teacher` |
| The teacher fails, times out or answers invalid JSON | the seed tier's effort | `seeds` |
| No classifier: the teacher answers | the teacher tier's effort | `teacher` |
| No classifier: the teacher fails | static `low`, as today | `fallback` |
| The raw embed failed | static `low`, as today | `fallback` |

How the table applies:

- **Every effort is clamped.** Each one goes through `cfg.ClampReasoningEffort`. A tier becomes an
  effort only through the existing tier mapping, renamed `ReasoningTier.Effort`.
- **The greeting fast path preserves the composer override.** It moves out of `Classify` into
  `IsTrivialGreeting` and applies only with no prior conversational context, attachments or pending
  action. An acknowledgement such as "ok" after an action request is not a standalone greeting.
  A standalone greeting runs neither embedding nor recall; an explicit effort still wins.
- **Among eligible labels, `user` beats `teacher`.** Take the nearest from the user pool, otherwise
  the nearest from the teacher pool. Missing provenance is a miss, never a wildcard match.
- **A copied label is never a label.** Rows whose source is `memory`, `seeds`, `greeting` or
  `fallback` are never used as labels. A turn decided by recall copies a label and does not create
  one, so the memory cannot reinforce its own guesses.
- **The teacher is extracted, not rewritten.** It is the router branch of `adaptiveReasoningTier`
  moved into a function. The no-classifier path calls the same function, so nothing is duplicated.
- **The classifier reports its margin.** `ReasoningClassifier.Classify` returns the margin that
  `semindex.Classifier.RankNearest` already computes.
- **Tools still load when the effort is fixed.** With a fixed effort, recall still runs for the
  tools on non-greeting turns; raw seed embedding and the teacher are unnecessary. A raw-embedding
  failure applies only to the adaptive path and cannot discard an explicit composer choice.

## Tool preload

- **What loads.** Walk the compatible tool pool nearest first. Recheck the live registry, keep
  registered `Deferred` tools, drop `tool_search`, deduplicate and sort by name for deterministic
  ordering. Use the first candidate with eligible tools and keep at most 3. A preload makes a
  schema available; it never executes a remembered call or reuses past arguments/authorization.
- **How they load.** They are added to `a.activated` before the first `buildRequest`. This is the
  same promotion that `MetaActivatedTools` triggers after a `tool_search` call.
- **Effect on the prompt cache.** Preload changes the round-1 tools array and can invalidate a
  cached prefix even when the model would not have searched. Native deferred-tool references and
  Aura's registry promotion have different cache behavior. Preserve deterministic ordering and
  measure cache reads/writes, added schema tokens and total cost by provider; no non-regression
  claim follows from moving the change to round 1.
- **Stale tools.** A tool that was unmounted since is filtered out by the registry check.

## Persisting what was learned

**The effort.**

- **The migration.** It takes the next free number at landing (`ls internal/db/migrations/`). Add
  the nullable fields below to `aura.conversation_turns`. Check non-null sources against the six
  table values. Existing rows are not backfilled with invented labels or compatibility keys.

  | Postgres field (text) | `ConversationTurn` property | Meaning |
  |---|---|---|
  | `recall_context_key` | `recall_context_key` | Prior-context digest, also used for tool recall |
  | `reasoning_effort` | `effort` | Applied effort on the first accepted main request |
  | `reasoning_effort_requested` | `effort_requested` | Decision before the clamp |
  | `reasoning_effort_source` | `effort_source` | `user`, `teacher`, `memory`, `seeds`, `greeting`, `fallback` |
  | `reasoning_effort_route_key` | `effort_route_key` | Route/capability digest |
  | `reasoning_effort_policy_version` | `effort_policy_version` | Compatible decision policy |
  | `reasoning_effort_origin_ref` | `effort_origin_ref` | Original user/teacher label's source, otherwise null |

- **The write.** Carry the authoritative user-turn identity from dispatch and update that exact
  row, scoped by identity and conversation, in the commit path before offering projection. Never
  resolve the target by "newest user" at write time. Write decision fields atomically; persist
  context for tool-use examples even if the request has no effort field. Absence of an effort
  field is distinct from the explicit value `none`, which must be recorded.
  A late successful write is recovered by periodic replay; replay cannot recover a failed
  Postgres write. The once-a-minute schedule is not a one-minute completion guarantee.
- **Branch re-runs write nothing.** A re-run answers an older user turn than the newest, so it
  records no label rather than label the wrong row.
- **The projection.** `ListProjectionTurns`, `ConversationTurnProjection` and the upsert carry
  every field above, including null clearing. Routing properties are STRING, contain no reasoning
  text, and must update even when content/hash/vector is unchanged. Postgres stays authoritative;
  a rebuild preserves provenance, and an old row without it cannot become a reusable label.

**Tool calls.**

- **Recording begins at observation.** `ObserveToolInvocation` validates the runtime event and,
  on the first valid terminal tool event, initializes `runID`, `createdAt` and a first step even
  if `ObserveReasoning` never ran. Once initialized, foreign run IDs are rejected. Preserve
  call-ID deduplication, limits and status mapping. Later reasoning can join the same trace.
- **Commit and reset.** `CommitSourceTurn` keeps a step with tool calls and an empty summary and
  commits a tool-only trace against the accepted authoritative assistant turn. Reset discards
  tools and decision provenance from repudiated attempts, so retries cannot leak old evidence.
- **Validation.** Permit an empty step summary only if that step has valid tool calls, and an
  empty trace summary only if it contains such evidence. Make this exception in structural
  trace/step validation; keep `validateReasoningText` strict for IDs, names, status and references.
  Empty traces and empty steps with no calls remain invalid. Do not synthesize reasoning text.
- **The embedding pass needs no change.** It already sets aside a row with no text: the row gets a
  space stamp and no vector, as text a space refused does (`memory_embed_pass.go`).
- **Retention.** Successful traces keep 30 days, so tool memory reaches 30 days back.
- **Deletion.** Conversation and identity deletion already remove these rows and vertices, and they
  also remove the labels.

## `tool_search` dense leg (`internal/agent/tools/search_dense.go`, new)

- **The index.** It covers the same corpus as BM25, the whole registry except `tool_search`. Each
  document is `searchDocument(spec)`, BM25's own retrieval document, as measured, embedded with the
  document template into a `semindex.Ranker`. `Ranker` is already in the repo and has no
  production caller.
- **The query.** The model's free text, embedded with the query template.
- **Building and invalidating.** Snapshot the whole registry except `tool_search`, ordered by
  name, initially on the first free-text search unless cold-build measurements require prewarming.
  The index key hashes each name and its exact `searchDocument(spec)` bytes, embedding
  space ID and document/query template versions. This covers summaries, retrieval keywords and
  schema argument names, including always-loaded tools. Registry invalidation also refreshes
  current spec/deferral metadata even if retrieval text is unchanged.
  Builds are single-flighted per key. Publish only if the registry generation and embedding
  space still match; discard stale builds. Fuse rankings from the same snapshot and recheck
  registered tools before returning results. A changed embedding space invalidates the bank.
- **Fusion.** `semindex.FuseRRF`, new and pure, adds 1/(60 + rank) over the BM25 and dense rankings,
  with one-based ranks and zero contribution when absent from a leg. For this small registry,
  fuse complete rankings before applying `max_results`. Ties keep BM25's order, then tool name
  for candidates absent from BM25. The exact-name layer and `select:` are unchanged.
- **Failure and latency.** If embedding fails, returns invalid vectors, changes space or exceeds
  the dense-search budget, return today's BM25 result. Bound both cold-build waiting and query
  embedding; a hung build cannot block lexical searches. Measure cold build duration: if it
  cannot fit the budget, validate prewarming via the existing startup/mount lifecycle before
  enabling the dense leg, rather than retrying an impossible cold build on every request.
- **The embedder.** It is the one the reasoning classifier uses, wired in `cmd/aura`.
- **Capability gaps.** The dense leg ranks every tool, so a request no tool serves gets the
  nearest tools instead of today's capability-gap orientation. The ship gate counts how many
  negatives lose their orientation, and the operator decides with that number before merge.

**Ship gate.** Repeat the offline numbers in Go, on the sets already in `search_gate_test.go` and
`search_keywords_measure_test.go`:

- the production gate stays at 26 of 26, and its floors hold;
- the sum of blind-English and held-out top-1 rises above BM25's. Offline it was 38 against 30.
- the independent test and end-to-end cost/latency gates under Testing pass, including Italian
  queries and requests with no supported capability.

If any condition fails, the leg is not merged. Counting capability-gap negatives is mandatory;
the already-required operator decision is made on the concrete measured report before merge.

## Errors

- **The raw embed fails:** static `low`, recall skipped. This is today's behaviour.
- **Recall errors or passes its deadline:** the turn continues with no memory, with one warning
  per turn. The budget includes client acquisition, space checks, embedding and all pool queries.
- **Missing/incompatible context or provenance:** a normal miss, with a reason in telemetry;
  no reusable label is fabricated. Seeds/teacher still decide the adaptive effort.
- **The teacher fails:** persist the seed/fallback decision, but no reusable teacher label;
  count the teacher attempt and its failure separately from the final decision source.
- **The effort write fails:** a warning; the turn is unaffected.
- **The trace write fails:** behaviour is unchanged (PRD §10's queue).

## Constants

| Name | Value | Evidence / validation status |
|---|---|---|
| `tierNeighbours` | 3 | already shipped |
| `teacherMargin` | 0.075 | Candidate calibrated on the scored set; needs independent validation |
| `recallRadius` | 0.10 cosine distance, document template | Candidate; similarities alone do not establish valid reuse |
| `recallNeighbours` | 5 per eligible pool | Bounded candidate count; labels filtered before top-k; validate duplicates and ANN recall |
| `preloadMax` | 3 | Observed maximum in the small VM sample; validate schema-token cost and unused preloads |
| `recallTimeout` | 500 ms | Proposed total recall budget; historical SQL-only 7 ms does not validate it |
| `denseSearchTimeout` | 500 ms | Proposed dense build/query wait budget; cold-build feasibility unmeasured |
| teacher timeout | `reasoningRouterTimeout` (at most 2 s) | existing |

## Observability

- **The log line.** The agent logs one line per turn, "adaptive reasoning: turn read", with the
  source, requested/applied effort, margin, selected label origin/distance, tool-turn
  origin/distance, compatibility-miss reason and preloaded tools. These may be different turns;
  one undifferentiated "nearest distance" is insufficient. Record route/policy versions and
  teacher outcome without logging raw context or credentials. "Tier applied" becomes "effort applied".
- **The persisted source counts final decisions**, not teacher calls:
  `select reasoning_effort_source, count(*) from aura.conversation_turns where role = 'user' group by 1`.
- **Teacher usage.** Count decision turns, turns that attempted the teacher, and outcomes
  `success`, `timeout`, `invalid`, `error`, `cancelled`. Include attempts on turns later aborted
  or failing persistence. Provider retries are a separate request counter. Teacher share is
  turns-with-attempt / decision-turns, with fixed/greeting/adaptive cohorts reported separately;
  a timeout followed by `seeds` still counts as a teacher attempt.
- **Latency and cost.** Record seed embedding, complete recall, teacher, cold index build and
  dense query durations; end-to-end time to first response/tool and completion; token usage,
  provider-reported cache reads/writes and cost when known. Count preloaded tools actually used,
  schema tokens added, and `tool_search` calls per tool turn. Unknown cache/cost data stays unknown.

## Testing

**Unit tests (no daemon):**

- `semindex.FuseRRF`: rank numbering, missing candidates and deterministic ties; classifier margin.
- One test per row of the effort table:
  - with a classifier, the teacher runs only below the margin and without an eligible label;
    the no-classifier path uses the same teacher and records its outcome;
  - a `memory`, `seeds`, `greeting` or `fallback` neighbour is never used as a label;
  - preloaded tools appear in the first request's tools;
  - unregistered names, non-deferred names and `tool_search` are dropped;
  - a fixed effort persists `user`;
  - standalone greetings skip embeddings/recall, fixed effort still wins, contextual "ok" does not;
  - incompatible/missing context, route or policy rejects labels; requested and applied values
    survive clamping, serialization and round trips without being confused.
- Runner writes the exact dispatched user row before projection; newer user rows and branch
  reruns are untouched. Explicit `none` persists; a missing effort still permits a context key.
- `RecallTurns` with `recordingClient`: eligibility in each pre-top-k filter, space/radius,
  self-exclusion, provenance parsing and one shared embedding; space changes fail closed.
- Projection updates decision metadata even with unchanged content and preserves/clears nulls.
- A real event sequence with only tool start/end and final answer produces a trace. Repeated
  call IDs, foreign run IDs and reset/retry do not duplicate or retain rejected calls. Structural
  validation accepts tool-only evidence while rejecting empty traces and invalid IDs/statuses.
- Dense invalidation covers always-loaded tools, summary/argument/keyword changes, removals,
  embedding space and templates. A mount change during a build prevents stale publication.
- Embedding error, invalid vector, timeout and unavailable/stale bank return the current BM25
  result; concurrent cold searches have bounded waits and cannot block lexical fallback.
- Teacher success and each failure outcome count attempts independently of decision source.

**Integration tests:**

- `arcadedb_integration`, on a disposable database:
  - two user turns, and a trace whose step has tool calls and no summary;
  - recall from a paraphrase returns the effort and the tools;
  - a turn in another space is excluded, even when the current space has no rows;
  - a far turn, current turn and incompatible context/route/policy are excluded;
  - more than five closer unlabelled copies cannot hide an eligible teacher label;
    teacher rows cannot crowd the user-label pool, and stale tools cannot crowd the tool pool;
  - compare bounded ANN retrieval with exact distances on a controlled corpus to measure misses.
- `db_integration`: migration up/down, atomic exact-row write, provenance projection/rebuild,
  unchanged-content updates and deletion. Legacy rows without provenance remain ineligible.
- `reasoning_live`: preserve the existing seed-classifier regression gate (reported 54/58) and
  its floors; separately exercise the integrated router with real teacher and memory calls.

**Independent evaluation, frozen before tuning or running the final comparison:**

- Separate calibration and final-test request families; keep duplicates/paraphrases in one
  family. Record dataset hashes, expected labels/rubric, route/model, embedding space, policy,
  seeds and thresholds. The previously inspected English/Italian sets are regression sets.
- Include Italian and English, unseen tool families, no-capability queries, negation, compound
  tasks, conflicting labels, attachments, and identical short messages in different contexts.
- Replay in time order. Memory may learn only from earlier actual executions/teacher decisions;
  never insert gold evaluation labels or future turns. Evaluate memory-empty and memory-warm
  runs separately. Repeat teacher trials to expose stochastic errors; report confidence intervals.
- Compare seeds alone, seeds+teacher, compatible recall/preload, and BM25 versus BM25+dense.
  Report effort confusion (especially hard tasks sent to `none`), actual task/answer success,
  valid-label precision/coverage, tool top-1/recall@5, unused preloads and teacher-attempt share.
- Release requires no new hard-to-`none` cases or lower observed task-success rate than baseline
  on the frozen set. Dense must retain regression floors and improve independent aggregate
  top-1 without degrading either language cohort. An uncertain comparison is not evidence of
  improvement; expand the frozen protocol with fresh cases rather than tuning on failed tests.
- On the predeclared workload, measure paired warm/cold p50/p95 latency, cache tokens and total
  token/cost usage including the teacher and embeddings. Require no observed regression in p95
  completion latency or known total cost. Explicitly report startup/rebuild behavior and misses;
  fewer `tool_search` calls alone do not pass this gate. Do not claim cost parity when unknown.

**End to end on the lab VM**, with fresh prompts outside calibration/regression corpora:

1. Turn A is uncertain. The teacher fires, its source is persisted, and the turn runs its tools,
   recorded with or without exposed reasoning.
2. Wait for observable projection and trace completion with a bounded test deadline; measure the
   lag. Turn B paraphrases A in a separate conversation with identical prior context and route.
   It is decided by `memory`, its tools are preloaded, and the ledger shows no `tool_search` call.
3. After deleting A's conversation, recall no longer returns it.
4. Replay B under changed context and changed route: incompatible labels are not reused. Exercise
   teacher timeout, embedder outage, MCP schema change, concurrent build and process restart.

Attach the paired report, exact prompts/expected outcomes and stage timings. All assertions and
the independent gates must pass; an unexplained aggregate score such as "above 9.8" is not an
acceptance criterion. This specification does not claim any of these new checks has passed.

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
- **A background-only teacher for the current decision.** It cannot correct that turn's effort
  before its first request. This does not mean the seed decision would always be wrong, or rule
  out a separately evaluated asynchronous verification policy later.
- **Composer-only labels.** The composer's explicit effort was used in 0 of 13 conversations.

## PRD

Amendments follow PRD-first: each is written after the measurement it records.

- **§6:** the effort decision, its constants, the document-template measurement, and the graph's
  9-of-16 coverage, extended with the end-to-end result.
- **§10:** traces carry the tool observations of turns that exposed no reasoning.
- **ArcadeDB scope:** `ConversationTurn` gains projected fields and nothing else. The 2026-07-31
  "memory only" decision still holds.

The static corrections in this revision do not amend the PRD with unmeasured claims. Record the
revised query, independent quality results, provenance schema and latency/cost evidence there
after the required live measurement, following CLAUDE.md.

## Primary sources checked on 2026-10-06

- [ArcadeDB vector functions](https://docs.arcadedb.com/arcadedb/reference/extended-functions/vector):
  native candidate filtering, `maxDistance` and grouped retrieval; the radius remains capped by k.
- [ArcadeDB issue #8959](https://github.com/ArcadeData/arcadedb/issues/8959) and
  [GHSA-h2j4-28h8-cj5v](https://github.com/ArcadeData/arcadedb/security/advisories/GHSA-h2j4-28h8-cj5v):
  evidence for the empty-filter correction and 26.10.1 security floor.
- [EmbeddingGemma model card](https://ai.google.dev/gemma/docs/embeddinggemma/model_card): task
  prefixes differ for retrieval, classification and similarity. Document/document recall is a
  measured local choice, not a universal recommendation; changing it requires revalidation.
- [Anthropic advanced tool use](https://www.anthropic.com/engineering/advanced-tool-use) and
  [tool use with prompt caching](https://platform.claude.com/docs/en/agents-and-tools/tool-use/tool-use-with-prompt-caching):
  deferred discovery and the difference between inline tool references and prefix changes.
- [Threshold tuning](https://scikit-learn.org/stable/modules/classification_threshold.html):
  separate tuning and validation; do not treat tuned development scores as independent evidence.
- [Krites, February/March 2026](https://arxiv.org/abs/2602.13165) and
  [Similarity Is Not Validity, September 2026](https://arxiv.org/abs/2609.35908): research on
  verified reuse and the limits of embedding similarity. These are research results about answer
  caches, not validation of Aura's effort router. Context/provenance constraints above are design
  deductions to test locally, not guarantees supplied by those papers.
