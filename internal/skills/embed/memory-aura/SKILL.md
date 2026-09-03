---
name: memory-aura
description: Use when writing to, reading from, or correcting Aura's long-term memory — storing a durable fact, recalling what is known about someone or something, or repairing a memory that is wrong, outdated, or should never have been recorded.
---

# Long-term memory

The memory is not a transcript. Aura already keeps the conversation in its own store;
this holds what should outlive it — facts, and the entities they connect. Two questions
decide everything: *does this still matter in a month?* and *when it is wrong, which act
repairs it?*

Every fact is **bitemporal**: it carries the window during which it was true. A fact is
never overwritten. That single property is what makes the third case below — *it was
true and no longer is* — the easy one rather than the impossible one.

## Writing

One verb writes: `memory_upsert_fact`, with `subject`, `predicate`, `object` and a
`statement` in natural language. The statement is what gets indexed and searched, so
write it as a sentence someone would recognise, not as a triple restated.

Every write also carries one `source` object with a `run_id` and optional `memory_ids`.
Use `subject_kind` and `object_kind` when the type is known. Replaying an exact fact is
safe: the same source is idempotent and a different source is attached to the existing
fact instead of creating another edge.

The subject and object entities are created for you. There is no separate "add entity"
step, and looking for one is how the graph ends up with facts that connect to nothing.

**Reuse the names the memory already knows.** A subject invented for one fact alone is an
island: it can be recalled by its own name and by nothing else. Before writing, it costs
one `memory_entities` or one `memory_digest` to see whether this thing already has a name
here, and to spell it the same way. The cost of skipping that is measurable — a memory of
107 facts imported one-note-at-a-time, each with a bespoke subject, held **209 entities**
and not one fact shared a neighbour with another until a linking sweep was written to
repair it after the fact. Facts are worth more connected than filed.

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
  and stay at 1 when you want only what is asserted of it. The reply says which you got
  in `retrieval.path`: `graph` for one hop, `mentions` for two, so a wide answer is never
  mistakable for a lucky one.
- `memory_digest` — the whole memory as a compact index, one line per entity. Read it
  ONCE and keep it rather than searching repeatedly. Check `covered`: when it is false
  the memory is larger than the index and search is still needed for the rest.
- `memory_entities` — just the names, when you need to know what exists.

Both `memory_search` and `memory_facts_about` take `as_of`: pass an instant to ask what
was true THEN instead of what is true now.

## What happened, as opposed to what is true

Facts are what the memory concluded. `memory_recall` reaches the record underneath — the
turns that were actually exchanged, and the reasoning behind them. One tool, and the
`mode` is the whole decision:

- **`recent`** — when the question is about *before* rather than about a topic. "What were
  we doing", "what do you remember about me", "where did we leave it". Reach for this the
  moment a question looks backward, because the default mode will not find it: `semantic`
  matches wording, and a question about the past rarely shares wording with the past. It
  answers "I recall nothing" and sounds authoritative doing it.
- **`semantic`** with `query` — a topic, when no name is at hand.
- **`entity`** with a name you already know — the same addressing `memory_facts_about`
  does, from inside this tool.
- **`open`** then **`scroll`** with a `conversation_id` — page through one conversation's
  turns in order. `open` puts you at an anchor, `scroll` moves with a `cursor` and a
  `direction`. This is how you read a discussion rather than sample it.
- **`reasoning`** — the provider-visible thinking traces. **No other mode returns them**,
  by design: they are not evidence of what was decided, only of what was considered. Ask
  for them when the question is *why* something was concluded, and say plainly that a
  trace is a deliberation, not a commitment.

Weak evidence makes it abstain explicitly rather than pad the answer. Take the abstention
at face value — say the memory has nothing rather than reaching for the nearest thing it
did return.

## Two more, rarely

- `memory_batch` — several writes as ONE transaction, with an idempotency key: ordered
  upserts, precise supersessions, merges and forgets that must all land or none. Use it
  when a correction spans more than one fact and a half-applied change would be worse
  than no change. For a single write, `memory_upsert_fact` is plainer and plainer is
  better.
- `graph_schema` — the vertex and edge types with their properties and live counts. Read
  it before writing a query by hand, so the names are the real ones rather than the ones
  you expected.

Every hit carries a `fact_key` naming that one fact. Keep it with the fact if you might
correct it later — it is what `supersedes_fact_key` takes to close exactly that fact and
no other. See "Correcting" below.

## Correcting: three situations, three different acts

**It was true and no longer is** → `memory_upsert_fact` with `supersedes: true`.
This is the ordinary case and the one the store is built for. It closes the old fact's
window and opens a new one; both stay answerable, and "what did I believe last month"
keeps working. Use it for anything that CHANGED — a move, a new role, a reversed
decision.

`supersedes: true` closes exactly one fact: the still-valid one sharing that subject and
predicate. If none matches, or if several do, the call REFUSES rather than guess — it
returns `refused: true`, `superseded: 0`, a `reason`, and the `candidates` it found. That
is a successful call, not an error, and nothing was written or closed.

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
support. A fact supported by another source remains. Run it with `dry_run: true` first
and read what it would remove.

Forgetting is the one operation with no undo, so it is also the one where a filter that
matches nothing is the likeliest outcome. **If a forget reports that it matched no facts,
the filter is wrong — sending it again unchanged cannot make it right.** Go and look:
`memory_entities` for the exact spelling, or `memory_search` for the `fact_key` and the
`run_id` a recall hit already carries. Provenance is usually the strongest handle there
is; a whole import is one `run_id`, where the same removal by entity is dozens of calls.

"Erase everything" is not one call and should not be attempted as one. Find the handles
first — the distinct `run_id`s, or the entity list — then remove them one at a time,
counting as you go.

**The same thing was recorded under two names** → `memory_merge_entities`.
Fold the duplicate onto the survivor; its facts move rather than dying with it. Prefer
this over forgetting a duplicate — forgetting destroys the facts only the duplicate knew.
If the target does not exist yet, this is a rename.

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

Exact replay deduplicates only the same subject, predicate, object and statement. A
different correction written WITHOUT `supersedes` leaves both versions valid at the
same instant. When something changed, say so with the flag; when another independent
fact was merely added, do not.
