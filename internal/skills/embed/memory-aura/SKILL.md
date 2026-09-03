---
name: memory-aura
description: Aura's long-term memory — storing a durable fact, recalling what is known about a person or a thing, reading back what was said in past conversations or why something was concluded, and correcting or erasing what is wrong. Load this BEFORE the first memory tool call of a session, not after: whenever the operator asks what you know or remember about them or anyone else, whenever they reveal a preference, a decision, a relationship or any detail worth keeping, whenever they say a stored fact is wrong or out of date, and whenever the question is about an earlier conversation or an earlier line of reasoning. The tools will answer without it, and that is the trap: they answer without the shared entity vocabulary, the POLE classes, the bitemporal correction verbs, or the trace-to-conversation pivot, which is how a memory ends up with one name per fact and nothing connected to anything.
---

# Long-term memory

The memory is not a transcript. Aura already keeps the conversation in its own store;
this holds what should outlive it — facts, and the entities they connect. Two questions
decide everything: *does this still matter in a month?* and *when it is wrong, which act
repairs it?*

Every fact is **bitemporal**: it carries the window during which it was true. A fact is
never overwritten. That single property is what makes the third case below — *it was
true and no longer is* — the easy one rather than the impossible one.

## The eleven tools, and which question each answers

| Tool | Reach for it when |
|---|---|
| `memory_upsert_fact` | one durable fact to store, or one to correct |
| `memory_batch` | several changes that must all land or none — and all bulk writing |
| `memory_search` | a topic, and you do not have a name |
| `memory_facts_about` | you HAVE a name; also `depth: 2` for what it connects to |
| `memory_entities` | which names and classes exist — read this BEFORE writing |
| `memory_digest` | the whole memory as one index, read once and kept |
| `memory_recall` | what was SAID or THOUGHT, rather than what is true |
| `memory_merge_entities` | the same thing was recorded under two names |
| `memory_forget` | it should never have been recorded |
| `memory_reembed` | the embedder was down, or the model changed |
| `graph_schema` | you are about to write a query by hand |

The first six answer *what is true*. `memory_recall` is the only one that reaches the
record underneath — and it is the one most often left unused when it was the right
answer.

**Four ride in every turn; seven are one `tool_search` away.** `memory_recall`,
`memory_upsert_fact`, `memory_batch` and `memory_entities` are always in front of you.
The rest exist and are reached by loading them first — the same way `web_search` is
reached — so a call that names one without loading it will not find it. Prefer the four
when they answer the question, and they usually do: `memory_recall` with `entity` is
`memory_facts_about`, with `query` it is `memory_search`, and `memory_batch` carries
`forget` and `merge_entities` as operations. Load the others when you actually want
them — a dry-run `memory_forget`, a `graph_schema` before writing a query by hand.

## Writing

One verb writes: `memory_upsert_fact`, with `subject`, `predicate`, `object` and a
`statement` in natural language. The statement is what gets indexed and searched, so
write it as a sentence someone would recognise, not as a triple restated.

Every write also carries one `source` object, and it takes `memory_ids` and nothing
else — the run is stamped by the host rather than asserted by you, so `source: {}` is a
complete and correct value when there are no message ids to cite. (`memory_forget` DOES
take a `run_id`, and requires one. The asymmetry is deliberate: you can revoke a whole
run you never had to name, because the host named it for you.)

Replaying an exact fact is safe. The same subject, predicate, object and statement
attaches your source to the existing fact instead of creating a second edge, and the
same source twice changes nothing. This holds for a fact written with a window that has
already closed too, so re-running an import of historical records enriches provenance
rather than multiplying rows.

The subject and object entities are created for you. There is no separate "add entity"
step, and looking for one is how the graph ends up with facts that connect to nothing.

### Classing an entity: one closed set, plus a free refinement

Every entity is stored as one of **six** classes — `Person`, `Object`, `Location`,
`Event`, `Organisation`, `Other` — the POLE model used for investigative graphs. The set
is closed on purpose: it is the one part of the vocabulary that cannot grow, so two
writers a month apart still agree on what kind of thing something is.

- `subject_pole` / `object_pole` state the class outright. Both `memory_upsert_fact` and
  `memory_batch` take them.
- Omit them and the class is derived from `subject_kind` / `object_kind`.
- A class outside the six does **not** fail the write and does not widen the set: the
  entity lands in `Other` and the reply says so with `request_refused` on that entity.
  Read it — a silent `Other` and a refused one look identical in the graph.

`kind` survives as the concrete refinement inside the class, exactly as the model
intends: `Officer` beside Person, `Vehicle` and `Phone` beside Object, `PostCode` beside
Location. The class is the frame; the kind is the detail.

**The class is decided once, at first write.** An entity that exists as a Person is not
re-minted as an Object because a later fact typed it differently — the reply reports the
divergence with `held_pole` instead. If the class is wrong, that is a correction, not a
re-write.

State the class when you know it, and state it especially in `memory_batch`, which is
where most entities get coined. The derivation only knows the kinds it has seen; a kind
it does not recognise lands in `Other` with nothing said about it.

### Read the vocabulary before you write. Every time.

The entity names in this memory are a **shared vocabulary**, not per-fact scratch space.
They are the only thing that makes two facts meet: a subject invented for one fact alone
can be recalled by its own name and by nothing else.

So the order is fixed, and it is one extra call for a whole session of writing:

1. **`memory_entities` first**, once, before the first write. Read the list and keep it.
   Each entry carries its `pole` and its `kind`, so one read tells you which names exist
   AND how they are classified. Check `total`: the listing is capped, and a total larger
   than the list means you are seeing part of the memory.
2. **Take every subject and object from that list** when anything on it is the thing you
   mean. Same spelling, same case.
3. **Coin a new name only when nothing on the list fits** — and then coin a NAME, not a
   description. The test is simple: *would anyone ever ask about this by this name again?*
   `ArcadeDB` yes. `la lentezza percepita di Aura sui turni banali` never.
4. **Read what the write answers.** When it introduces a name the memory had never held,
   the reply carries `coined` with the closest existing names. If one of them was what you
   meant, `memory_merge_entities` folds the mistake onto it while the fact is one write old
   rather than one month old.

**Kinds are a vocabulary too, and the same list carries them.** Reach for a kind already
in use before inventing a label; a memory holding `Tool`, `Person` and `Environment` does
not also want `Utility`, `Human` and `Machine`. The six classes above bound the damage a
new kind can do, but they do not remove the cost of three labels for one idea.

### Founding one, when the list comes back empty

On an empty or nearly-empty memory the loop above degenerates: there is nothing to reuse,
and — this is the part worth knowing — **`coined` goes silent for every name, well-chosen
or not.** It can only report neighbours the memory already holds, so on a founding write
it has none to report. Silence there is not validation. It is the absence of an opinion.

So the discipline has to come from you, and it is one question asked of the whole corpus
before the first write rather than of each fact as it arrives: *which nouns does this
material keep coming back to?* Found those, and let everything else live in the
`statement`. Where a note's own phrasing is a description, name the thing the description
is ABOUT — a note about being told to read the documentation first is a fact about
`Operator` and `ArcadeDB`, not about `la lettura della documentazione`.

Measured on this memory's own founding, 2026-09-03: twelve notes written that way produced
**fifteen entities, all recognisable nouns**, and ten of the twelve facts landed in a
single connected component. The previous 107-fact memory, written without the rule, had
reached 211 entities and 29% connectivity.

The cost of skipping this is not theoretical, and it is not small. Measured on a real
memory of 108 facts on 2026-09-03: **211 distinct entities, of which 207 were used exactly
once.** Only one name — `Aura` — was used by as many as three facts. The vocabulary the
corpus actually had was hiding in the prose, where `MCP`, `Phase`, `Neo4j` and `ArcadeDB`
each recur in eight to twenty statements; the structured endpoints beside them were
freshly invented every single time. Nothing could connect, because nothing was ever named
twice.

A healthy memory's vocabulary grows far more slowly than its facts. If a hundred facts
have produced a hundred names, the memory is a filing cabinet with one document per
drawer.

**The object becomes an entity too, and that is the easier half to get wrong.** It is not
an inert value slot: `object` mints a vertex under the same unique name index the subject
does. A guard rejects objects that read as sentences — over ~80 characters, or ending in
`.`/`!`/`?` — with `fact object reads as prose, not an entity name`. But it judges SHAPE,
not sense, so a tidy little phrase like `11ms single query latency on RTX A2000` sails
straight through and becomes a permanent entity that one fact points at and nothing will
ever name again. Ask of every object what you ask of a subject: *would anything else here
ever refer to this by this name?* If not, it is a reading, and readings belong in the
`statement` — the field that gets embedded and searched. Name the object for the thing,
not for the measurement of it.

Do **not** record the passing shape of a conversation — what you just did, which file you
opened, that a command succeeded. If it would not matter in a month it does not belong
here. Memory full of session debris is worse than empty memory: it dilutes retrieval, and
every wrong entry is something someone has to find and remove later.

## Recall

- `memory_search` — free text across the statements. It fuses a full-text leg with a
  vector leg, so it reaches a fact whose wording differs from the question, and across
  languages: a question in Italian finds a fact written in English.
- `memory_facts_about` — when you have a NAME. It walks that entity's edges in **both
  directions**, so it returns the facts where the entity is the object as well as the
  subject. That matters more than it sounds: the things a memory is asked about are
  usually spoken *about* rather than speaking.

  Prefer it over search whenever a name is available, and not as a style preference:
  a fact written minutes earlier did not appear in the top two of a `memory_search`
  phrased in another language, while `memory_facts_about` on its exact subject returned
  it first. Search ranks; this one *addresses*.

  It takes a `depth`. At the default **1** it answers with what is stated about that
  entity directly. At **2** it widens to the neighbourhood: facts that mention something
  this entity's facts also mention. Reach for 2 when the question is about how a subject
  *connects* to the rest — "what does X have to do with Y", "what else touches this" —
  and stay at 1 when you want only what is asserted of it.

  `retrieval.path` tells you which QUERY ran — `graph` for one hop, `mentions` for two —
  and nothing about whether it found anything. Reading `mentions` as "the widening
  worked" is a mistake that has already been made: the only evidence is the count.
  **If depth 2 returns the same facts as depth 1, the neighbourhood is empty.**

  It is empty for a real reason worth knowing. The `MENTIONS` edges the second hop walks
  are not written when a fact is written — a periodic sweep builds them by scanning
  statements for the names of entities the memory already knows. Until that sweep has run
  over your writes, depth 2 traverses a graph with no edges in it and answers exactly like
  depth 1. A memory that was written minutes ago is the normal case for this.
- `memory_digest` — the whole memory as a compact index, one line per entity. Read it
  ONCE and keep it rather than searching repeatedly. Check `covered`: when it is false
  the memory is larger than the index and search is still needed for the rest.
- `memory_entities` — the names with their class and kind, when you need to know what
  exists.

Both `memory_search` and `memory_facts_about` take `as_of`: pass an instant to ask what
was true THEN instead of what is true now.

Every hit carries a `fact_key` naming that one fact, whichever of these found it. Keep it
with the fact if you might correct it later — it is what `supersedes_fact_key` takes to
close exactly that fact and no other. A fact whose window has already closed carries none:
history is readable, not correctable, and correcting a closed fact is not a thing to want.

## What happened, and what was thought: `memory_recall`

Facts are what the memory CONCLUDED. `memory_recall` reaches the record underneath — the
turns that were actually exchanged, and the reasoning behind them. It is one tool, and
`mode` is the whole decision. The modes are `semantic`, `recent`, `open`, `scroll` and
`reasoning`; anything else is rejected before the call runs.

This is the most under-used tool in the surface, and the two halves below are why: a
conversation and a trace answer questions no fact can.

**Two ways this goes wrong, both observed on 2026-09-03 in a real session.**

*Answering a backward-looking question from the facts.* Asked "what did we say in earlier
conversations", the reply listed stored facts — the dog's name, the favourite colour, the
preferred channel. Those are what is TRUE, not what was SAID. The question was about the
record, and the record is `recent`. A fact list is a confident answer to a question nobody
asked.

*Describing the mechanism instead of reading it.* Asked how its reasoning trace works, the
reply was an essay about thinking and acting and observing, and it went as far as "it is
not a tool I call, it is the way I think". **That is false here.** A trace is a stored
record with an id, ordered steps and the tool calls made inside them, and `reasoning` opens
it. When the question is about your own reasoning, the honest answer is retrieved, not
composed — open a real trace and say what it actually shows. Explaining the concept when
the record was one call away is the same failure as inventing a fact.

### Facts, from inside recall

- **`semantic`** with `query` — a topic, when no name is at hand. Same hybrid retrieval
  `memory_search` uses.
- **`semantic`** with `entity` — a name you already know. `entity` is a PARAMETER, not a
  mode: passing it switches to graph traversal, the same addressing `memory_facts_about`
  does. `predicate` narrows it further. (Asking for `mode: entity` is rejected — that is
  not one of the five.)
- **`as_of`** on either — what was true THEN rather than now.

### Conversations: reading a discussion instead of sampling it

- **`recent`** — when the question is about *before* rather than about a topic. "What were
  we doing", "what do you remember about me", "where did we leave it". Reach for this the
  moment a question looks backward, because the default mode will not find it: `semantic`
  matches wording, and a question about the past rarely shares wording with the past. It
  answers "I recall nothing" and sounds authoritative doing it.
- **`open`** with a `conversation_id` and an `anchor_seq` — a window of turns FORWARD from
  that anchor, the anchor included. This is the entry point; `limit` sizes the window.
- **`scroll`** — continues where `open` stopped.

  **Hand the `next_cursor` straight back.** It carries the conversation, the anchor, the
  direction and the page size, so `mode: scroll` with nothing but `cursor` is the whole
  call. You may repeat those fields, but a value that CONTRADICTS the cursor is refused
  rather than silently preferred.

  **No `next_cursor` means there is no next page.** Each page begins past the last turn the
  previous one delivered, so following the cursor reads every turn exactly once and stops
  on its own. `direction: before` walks backward from the anchor instead.

Two things about turns that will otherwise mislead you:

- **`turn_seq` is not contiguous.** A conversation can run 1, 2, 3, 6, 7, 10 — the gaps are
  turns that are not part of the readable record. Never compute "how many turns" from the
  last seq, and never assume `anchor_seq + 1` exists.
- **Every turn carries a `source_ref`** of the form
  `postgres://aura/conversations/<id>/turns/<n>`. That is the citation to give when you
  report what was said, and it is the same addressing a reasoning trace uses — which is
  what makes the pivot below possible.

### Reasoning traces: why something was concluded

- **`reasoning`** — the provider-visible thinking. **No other mode returns it**, by design:
  a trace is not evidence of what was decided, only of what was considered.
  - `query` searches across traces and ranks them.
  - `trace_id` opens ONE trace, and that is the form worth knowing: it returns the ordered
    `steps`, each with its own `provider_summary`, and on the steps that made them, the
    `tool_calls`.

A tool call carries `tool_name`, `status`, `duration_ms`, a short `observation` of the
outcome, `artifact_refs` for anything produced, and an `argument_digest` — a hash, not the
arguments. **The arguments were never stored**, so "what exactly did it pass" is a question
this record cannot answer, and saying otherwise is inventing it.

**And most tool calls are not recorded at all.** Only a short allow-list is written to a
trace — the memory writes, `send_file`, `task` — so `shell_exec`, `write_file`,
`search_files`, `document_search` and the rest leave no entry. Measured on this memory:
90 reasoning steps hold 7 tool calls between them. An empty `tool_calls` therefore means
"not recorded", NEVER "no tools ran", and a trace is not an audit of what was done. Say
what the steps say; for what was actually executed, the conversation turns are the record.

The trace-level `provider_summary` is the steps run together, which is what `query`
matches. Read the STEPS, not that blob: the blob is where a five-step deliberation looks
like one long muddle.

**Traces expire; facts do not.** Every trace carries `expires_at` (thirty days out from
when it was created). A question about last quarter's reasoning has no answer, and the
honest reply is that the trace is gone — not a reconstruction from the facts it produced.

### The pivot, which is where this gets powerful

A trace carries `conversation_id` and `turn_seq`. A conversation turn carries the same
`source_ref` shape. So the two halves join, and the join is the whole point:

- *"Why did it do that?"* → `reasoning` with a `query`, then `trace_id` on the best hit to
  read the steps and the tool calls.
- *"…and what was actually said around it?"* → `open` that trace's `conversation_id` at its
  `turn_seq`, then `scroll`.
- *"…and what did we conclude?"* → `memory_facts_about` on the entities involved.

Going the other way works too: a fact's `sources` carry the `memory_ids` and the `run_id`
that produced it, which is how you get from a conclusion back to the discussion that
reached it.

Weak evidence makes every mode abstain explicitly rather than pad the answer. Take the
abstention at face value — say the memory has nothing rather than reaching for the nearest
thing it did return. `conversation_exhausted` is not a failure: it says the conversation
ended, where `conversation_anchor_not_found` says the anchor you asked for holds no turn.

## Several changes as one

`memory_batch` applies ordered `upsert_fact`, `supersede_fact`, `merge_entities` and
`forget` operations as ONE transaction under an idempotency key. Use it when a correction
spans more than one fact and a half-applied change would be worse than no change, and use
it for bulk writing — it is where most entities get coined, so it is where stating
`subject_pole` / `object_pole` pays most.

Three things to know before reaching for it:

- **It is final-state, so operations see each other.** A `forget` that removes an entity
  leaves nothing for a later operation to name.
- **A `forget` that matches nothing ABORTS the batch** (`target_not_found`, live state
  unchanged), where the standalone `memory_forget` would return zeros and change nothing.
  Order destructive operations so each one still has a target.
- **The idempotency key is bound to the request.** Replaying the identical batch returns
  the original result with `replayed: true`; reusing the key with a different payload is
  refused outright.

Its shape is a tagged union, and that is worth stating because it is the one thing here a
reader guesses wrong. Each operation is a `type` plus ONE payload object named after what
it does — the fields do not sit at the top level:

```
{"idempotency_key": "...", "operations": [
  {"type": "upsert_fact",     "fact":   {subject, predicate, object, statement, source, …}},
  {"type": "supersede_fact",  "fact":   {…, "supersedes_fact_key": "<fact_key>"}},
  {"type": "merge_entities",  "merge":  {"source": "<duplicate>", "target": "<survivor>"}},
  {"type": "forget",          "forget": {"entity": "<name>"}}
]}
```

A trace from 2026-09-01 shows the cost of not knowing this: the agent sat guessing whether
the key was `id` or `key` rather than `fact_key`, and which object `forget` and
`merge_entities` wanted their fields in.

For a single write, `memory_upsert_fact` is plainer, and plainer is better.

## Maintenance, when the operator asks

- `memory_reembed` — writes the vectors facts are searched by. With no argument it fills
  only the facts that have none, which is the gap an embedder that was down leaves behind.
  With `all` it recomputes EVERY fact's vector in one call, which is what an embedding-model
  change requires: vectors written by a different model live in a different geometry and
  answer worse without ever saying so. `batch` is the size of one round, not a ceiling on
  the work.
- `graph_schema` — the vertex and edge types with their properties and live counts. Read
  it before writing a query by hand, so the names are the real ones rather than the ones
  you expected.

## Correcting: three situations, three different acts

**It was true and no longer is** → `memory_upsert_fact` with `supersedes: true`.
This is the ordinary case and the one the store is built for. It closes the old fact's
window and opens a new one; both stay answerable, and "what did I believe last month"
keeps working. Use it for anything that CHANGED — a move, a new role, a reversed
decision.

`supersedes: true` closes exactly one fact: the still-valid one sharing that subject and
predicate. If none matches, or if several do, the call REFUSES rather than guess — it
returns `refused: true`, `superseded: 0`, a `reason`, and the `candidates` it found. That
is a successful call, not an error, and **nothing at all was written**: no fact closed, no
fact created, and not even the entities the refused fact would have introduced.

A refusal is the multi-valued case surfacing. "lives in" has one still-valid fact and
closes cleanly; "likes" has several, and the flag alone cannot say which one you meant.
Read the `candidates`, pick the fact you are correcting, and send the write again with
`supersedes_fact_key` set to that candidate's `fact_key`. Every recall hit carries a
`fact_key` for exactly this — it names one fact, and passing it back closes that one and
no other.

The refusal exists because the alternative was worse: `supersedes` used to close ALL of
them and report it as an ordinary success, so a single correction to a multi-valued
relation quietly destroyed every other value it held.

**It should never have been recorded** → `memory_forget`.
Test residue, something captured by mistake, something the operator asked you to erase.
Nothing should remain, and there is nothing to keep addressable. It takes an `entity`, a
`subject`/`predicate`/`object` triple, or `source: {run_id: ...}` to detach one run's
support. A fact supported by another source remains. Entities left without any fact go
too, unless you pass `keep_orphan_entities`. Run it with `dry_run: true` first and read
what it would remove.

Forgetting is the one operation with no undo, so it is also the one where a filter that
matches nothing is the likeliest outcome — and it reports that as a plain `0`, not as an
error. **If a forget reports that it matched no facts, the filter is wrong; sending it
again unchanged cannot make it right.** Go and look: `memory_entities` for the exact
spelling, or `memory_search` for the `fact_key` and the `run_id` a recall hit already
carries. Provenance is usually the strongest handle there is; a whole import is one
`run_id`, where the same removal by entity is dozens of calls.

"Erase everything" is not one call and should not be attempted as one. Find the handles
first — the distinct `run_id`s, or the entity list — then remove them one at a time,
counting as you go.

**The same thing was recorded under two names** → `memory_merge_entities`.
Fold the duplicate onto the survivor; its facts move rather than dying with it, carrying
their provenance with them, and the duplicate's name is gone afterwards. Prefer this over
forgetting a duplicate — forgetting destroys the facts only the duplicate knew. If the
target does not exist yet, this is a rename.

It rewrites the duplicate's name where it appears in a statement, so the full-text index
stops answering under a name that no longer exists. It can only rewrite the name as
WRITTEN: a statement that referred to the entity by other words still describes the
world as it was before the merge. Read the merged facts back if the wording matters.

## Say what the result says

Every one of these tools answers with counts — how many facts it wrote, closed or removed —
and those counts are the only thing you know. A reply that removed five facts is not a
memory that is now empty, and reporting it as one is worse than reporting the failure,
because nobody will look again.

So: read the number, and let it be the claim. When the operator asked for something
sweeping — everything about X gone, the whole import undone — the honest close is a second
read that shows the state, not the write's own receipt. `memory_digest` returns `facts` and
`entities` for exactly this; a `memory_facts_about` on the name you were asked to erase
should come back empty. One extra call turns "I removed 5" into "nothing remains", or into
"5 went, 103 are still there" — and the second sentence is the one worth saying.

## The correction trap

Exact replay deduplicates only the same subject, predicate, object and statement — and,
for a fact with a window, the same window. A different correction written WITHOUT
`supersedes` leaves both versions valid at the same instant. When something changed, say
so with the flag; when another independent fact was merely added, do not.
