# Audit — offloading conversation context to ArcadeDB, and where "lessons" live

Date: 2026-09-03. Measured against the running stack (Postgres `aura`, ArcadeDB 26.9.1,
identity `bb78065b`).

Two questions were asked: how lessons are handled, and whether the conversation record now
in ArcadeDB could shrink the input context — "in theory infinite". The second question is
the right one to have asked *now*, because the context ladder was designed when Postgres
was the only record. The memory MCP added a second one, and nothing in the ladder knows.

## 1. Lessons: there is no mechanism

`Aura learned_lesson tool_search_strategy` exists in the live memory. It is one fact with
an invented predicate. Grepping the tree for any lesson machinery returns nothing:

    internal/agent/tools/swarm_status.go:101   // …the swarm_spawn precedent's own lesson.
    internal/arcadedb/browse.go:65             // …teaches exactly the wrong lesson.

Both are prose in comments. **A lesson today is an ordinary fact whose predicate someone
chose once.** That is not automatically wrong — the graph is bitemporal and a lesson is a
durable statement about how to work, which is exactly what a fact is — but three things
follow from it having no shape:

- Nothing distinguishes a lesson from any other fact at retrieval time, so a lesson
  competes with trivia on the same ranking.
- The predicate is not in any closed vocabulary, so the next writer coins `lezione`,
  `learned`, or `insegnamento`. This is the same drift the POLE classes were introduced to
  stop one level up, and the entity-name drift the skill measures at 211 names for 108
  facts.
- A lesson has no subject discipline. `Aura learned_lesson tool_search_strategy` makes the
  AGENT the subject of everything it ever learns, which turns `Aura` into the hub every
  lesson hangs off and nothing else can connect to.

If lessons are to be first class, the cheapest version that would work is a convention
rather than a mechanism: one predicate (`learned_lesson`), the SUBJECT being the thing the
lesson is about rather than the agent, and the lesson's trigger in the statement. That is a
paragraph in the memory-aura skill, not code. It is deliberately not implemented here
because the operator has not asked for a lesson type, and inventing a vertex class for it
would widen the closed POLE set for one use.

## 2. The context ladder, and what it does not know

Today a turn's input is assembled from **Postgres** and squeezed by a deterministic ladder
(`internal/conversations/context.go`):

| Stage | What it does |
|---|---|
| L0 | the system turn, kept byte-identical for the KV-cache prefix |
| messages[1] | the always-block, rebuilt per turn |
| L1 | tool outputs cleared to pointers; the bytes stay retrievable via `read_tool_output` |
| L2 | budget gate against `hardCap` (model window − max output); WARN at 0.75× |
| L2.3 | a compaction the branch already has stays in force |
| L2.4 | LLM summarisation, durable and cached (`/compact`) |
| L2.5 | deterministic drop of the oldest pair — the fail-safe |
| — | `ErrContextWindowExceeded` → "start a new chat with `aura chat new`" |

History pages from Postgres at `AURA_HISTORY_HARD_CAP_TURNS` (default 50).

The ladder treats eviction as **loss**. L1 is the exception and it is the shape worth
copying: a cleared tool output is not gone, it is a pointer to a sidecar the model can
reopen. L2.5 has no such pointer — it drops prose, and the terminal answer is to abandon
the thread.

## 3. What the MCP actually added

ArcadeDB now holds `Conversation` and `ConversationTurn` with **both** a full-text index on
`content` and a vector index on `embedding`, and `memory_recall` pages them (`recent`,
`open`, `scroll`) and searches them (`semantic`). Each turn carries a `source_ref` of the
form `postgres://aura/conversations/<id>/turns/<n>`, so a projected turn is addressable
back to its authority.

So the two halves of "infinite" already exist, separately:

- **tool output** → offloaded to sidecars, reopened with `read_tool_output` (L1, shipped)
- **prose** → projected to ArcadeDB, retrievable by `memory_recall` (shipped)

What does not exist is the join: the ladder never tells the model that what it dropped is
retrievable, and never checks that it *was* retrievable before dropping it.

## 4. The measurement that bounds the idea

ArcadeDB is **not** a copy of the conversation. Counted today:

| conversation | Postgres turns | ArcadeDB turns |
|---|---|---|
| `01a06705-…` | 39 | 24 |
| `01a05c23-…` | 178 | 72 |

The gap is deliberate, not lag. `internal/conversations/store_projection.go` selects:

```sql
AND t.role IN ('user', 'assistant')
AND t.tool_call_id IS NULL
AND t.tool_calls IS NULL
AND (NULLIF(BTRIM(t.content), '') IS NOT NULL OR t.content_sidecar_path IS NOT NULL)
```

ArcadeDB holds the **dialogue**, not the transcript: every tool call, every tool result,
and every assistant turn that carried only tool calls is excluded. For `01a06705` that is
39 = 12 user + 19 assistant + 8 tool, of which 24 project — exactly user plus the assistant
turns that said something.

That is a good design for a memory and a hard bound on this idea: **an "infinite context"
built on ArcadeDB can only be infinite in prose.** Tool traffic is recoverable, but through
the sidecar, not through memory.

Two further limits, both measured:

- **The projection lags.** `projection_updated_at` for `01a06705` read 11:37:21 while its
  last turn was 11:30:07. On a conversation seconds old the vertex does not exist yet, and
  `memory_recall` currently REFUSES the whole call in that window (see §6).
- **It is derived and prunable.** The projector's own interface calls it "the rebuildable
  derived graph boundary", with delete, prune and tombstone paths. Nothing that must
  survive can be stored only there.

## 5. What it would take, concretely

The enabling fact is that the watermark already exists: the `Conversation` vertex carries
`projected_through_seq`, written by the projector as its high-water mark.

1. **Gate eviction on projection.** L2.5 may only drop a prose turn whose `seq` is at or
   below that conversation's `projected_through_seq`. A turn not yet projected is dropped
   into nothing, which is today's behaviour and the thing to stop.
2. **Leave a pointer, exactly as L1 does.** Replace the dropped span with one line naming
   how to get it back — the conversation id and the anchor — so the model can page it with
   `memory_recall`. A drop the model cannot see is indistinguishable from a memory it never
   had.
3. **Keep `ErrContextWindowExceeded` for the unprojectable remainder.** Tool traffic and
   unprojected turns still have no memory path; the honest terminal state stays.

That is a small change with a real payoff: the thread stops being abandoned at the window
edge, and the model regains what it dropped on demand rather than being told to start over.
It is written here rather than implemented because it changes eviction semantics for every
conversation, which is the operator's call, and because point 1 must land before point 2 —
a pointer to a turn that was never projected is worse than the drop it replaces.

## 6. The action half of a reasoning trace is essentially unwritten

Counted today on the same memory:

| type | records |
|---|---|
| `ReasoningTrace` | 84 |
| `ReasoningStep` | 90 |
| `ReasoningToolCall` | **7** |
| `INVOKED` | 7 |

The seven are `send_file` (6) and `memory__memory_recall` (1). Across ninety reasoning
steps, that is the whole record of what was DONE.

It is not a bug. `internal/runner/runner_reasoning_graph.go` gates capture on an
allow-list of six tools — `memory_batch`, `memory_forget`, `memory_recall`,
`memory_upsert_fact`, `send_file`, `task` — and returns early for everything else. The
design is defensible on privacy grounds: arguments are never stored, only an
`argument_digest`, so a conservative list avoids putting shell output, file contents and
document text into a durable graph.

Two consequences follow, and both are worth stating rather than discovering later:

- **A trace records thinking, not acting.** `shell_exec`, `write_file`, `search_files`,
  `document_search`, `web_search` and `swarm_spawn` never appear. An empty `tool_calls`
  means "not recorded", never "no tools ran", and a trace must not be read as an audit of
  what happened. The memory-aura skill now says so, because it had begun teaching the
  opposite.
- **It closes the second door on §4.** Tool traffic is absent from `ConversationTurn` by
  the projection filter AND absent from `ReasoningToolCall` by this allow-list. The only
  surviving copy is the sidecar behind `read_tool_output` — per-run files, not a queryable
  store. So the bound in §4 is structural from two independent directions: an offloaded
  context can be recovered in prose and in nothing else.

Whether to widen the allow-list is a privacy decision, not a technical one, and is left
where it belongs.

## 7. Cockpit defect: a label filter returned an empty graph

Reported from the UI and reproduced: filtering the memory graph to `ReasoningToolCall`
showed "no memories in the graph yet" — 0 nodes, 0 connections — against a database
holding 7 of them, with the schema panel correctly listing 15 node types.

The backend query is fine: `SELECT FROM `ReasoningToolCall` LIMIT 50` returns all seven,
and the Studio serializer renders them. The loss is in `overview`
(`internal/agui/graph_arcadedb.go`). Edge types are queried first and every edge query
returns its ENDPOINTS, so `raw.Vertices` fills with `Conversation` and `ConversationTurn`
records the caller never selected. The vertex loop then guarded on
`len(raw.Vertices) >= nodeCap`, tripped immediately, and never queried the chosen type at
all — after which `projectStudioGraph` discarded every accumulated vertex for having the
wrong label. Nodes and edges both came back empty, because an edge is only kept when both
its endpoints survived.

Fixed: the budget now counts the vertices the caller's label selection would actually keep
(`countAllowedVertices`). With no label selected the set allows everything, so the previous
count and the previous bound are unchanged.
`TestArcadeGraphViewFilteredLabelSurvivesEdgeEndpoints` pins it, and fails with
`nodes = []` against the old guard.

## 8. Defect found while auditing

`memory_recall` refuses on a conversation that is not yet projected:

    memory_recall: active-source conversation is not owned by the authenticated identity

Observed live at 11:27:03, one second after that conversation's first turn. The check
(`cmd/arcadedb-mcp/tool_memory_recall.go:357`) revalidates that the caller owns the
"active source" conversations, and the validated ids are used for exactly one thing —
`ExcludeConversationIDs` (line 201), so the current conversation is not recalled into
itself. Excluding a conversation you do not own is a no-op, and the tenancy boundary is
already the per-identity database.

Refusing the entire recall because an EXCLUSION target cannot be confirmed is
disproportionate, and it fires on the opening turns of every new conversation — the first
memory read of a session. The error also reads as a security violation, which is what made
the agent apologise for laziness in a session where it had actually been blocked.

Proposed, not applied: treat "not yet projected" as "nothing to exclude" rather than as a
refusal. Left for the operator because the code is adjacent to an ownership boundary.
