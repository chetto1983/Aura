---
name: memory-aura
description: Use when writing to, reading from, or correcting Aura's long-term memory graph — storing a durable fact or preference, recalling what is known about someone or something, or fixing a memory that is wrong, outdated, or should never have been recorded.
---

# Long-term memory

The memory graph is not a transcript. Aura already keeps the conversation; this holds
what should outlive it — facts, preferences, and the entities they concern. Two
questions decide everything: *does this still matter in a month?* and *when it is
wrong, which verb repairs it?*

## What belongs here

Write when you learn something durable:

- a fact that will still be true tomorrow → `memory_add_fact` (subject, predicate, object)
- how the operator wants to be treated → `memory_add_preference` with a category, and
  `applies_to` when it concerns a specific person, place or organization. That argument
  is what connects the preference into the graph instead of leaving it dangling off the
  user, so recall can reach it by structure and not only by wording.

Do **not** record the passing shape of a conversation — what you just did, what file you
just opened, that a command succeeded. If it would not matter in a month, it does not
belong here. A graph full of session debris is worse than an empty one: it dilutes
retrieval and every wrong entry becomes something someone has to find and remove later.

## Recall

`memory_search` is the free-text lookup across facts, entities and preferences. Its
`facts` bucket tries an **exact subject** first and only then falls back to similarity,
so when you already know the subject by name, pass the name — you get its facts rather
than whatever scored nearest.

`memory_get_entity` is the one to reach for when you have a NAME and want what is known
about it. It returns the entity, the facts recorded about it, its neighbours in the
graph, and — the part that matters — **`other_matches`, any duplicates recorded under
the same name.** Use it before correcting anything: fixing one of two entities with the
same name and walking away leaves the wrong one in place, still being returned.

## Correcting: three situations, three different acts

Getting this wrong loses information silently, so pick deliberately.

**The content is wrong, the memory has a right to exist** → `memory_update`.
The name is misspelled, the category is off, the triple says the wrong thing. Corrects
in place: keeps the id, the relationships and the history, and refreshes the embedding
so search stops returning it under the old wording.

**It should never have been recorded** → `memory_forget`.
Test residue, something captured by mistake, something the operator asked you to erase.
Nothing should remain. `memory_forget` also breaks a wrong **relationship** — pass
`node_type=relationship` with `source_id`, `target_id` and `relationship_type`, in
either direction. That case is not cosmetic: the deduplicator writes `SAME_AS` edges on
its own between entities it believes are the same, and a wrong one is how the graph
degrades by itself — the entity it points at becomes an attractor that later writes of
that name merge onto.

**It was true and no longer is** → neither of the above is right.
Updating overwrites the past; forgetting denies it ever happened. When the history
matters, record the new state and leave the old one addressable.

## The trap: never correct by re-adding

`memory_add_fact`, `memory_add_preference` and `memory_add_entity` deduplicate what you
pass against what already exists and **merge into the closest match without changing its
stored text**. So "I will just add the corrected version" does not correct anything — it
re-merges onto the old wording, every time, silently, and you are left believing you
fixed it.

This is not hypothetical. Aura's graph carried two contradictory rules about deferred
tools, both at `confidence: 1.0`, precisely because each attempt to correct one was
another `add_*` that merged back onto the original.

Correcting requires an **id**. They come from:

- the `add_*` response — field `id`, or `matched_entity_id` under `deduplication` when
  it merged into something that already existed
- `memory_get_entity`
- a `memory_search` result

If you find yourself about to re-add something to fix it, stop and go get the id.
