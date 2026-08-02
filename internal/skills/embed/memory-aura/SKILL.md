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

The subject and object entities are created for you. There is no separate "add entity"
step, and looking for one is how the graph ends up with facts that connect to nothing.

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
- `memory_digest` — the whole memory as a compact index, one line per entity. Read it
  ONCE and keep it rather than searching repeatedly. Check `covered`: when it is false
  the memory is larger than the index and search is still needed for the rest.
- `memory_entities` — just the names, when you need to know what exists.

Both `memory_search` and `memory_facts_about` take `as_of`: pass an instant to ask what
was true THEN instead of what is true now.

## Correcting: three situations, three different acts

**It was true and no longer is** → `memory_upsert_fact` with `supersedes: true`.
This is the ordinary case and the one the store is built for. It closes the old fact's
window and opens a new one; both stay answerable, and "what did I believe last month"
keeps working. Use it for anything that CHANGED — a move, a new role, a reversed
decision.

Note that `supersedes` closes every still-valid fact sharing that subject and predicate.
That is right for a single-valued relation ("lives in") and wrong for a multi-valued one
("likes"), which is why it is a flag you set rather than a default.

**It should never have been recorded** → `memory_forget`.
Test residue, something captured by mistake, something the operator asked you to erase.
Nothing should remain, and there is nothing to keep addressable. It takes an `entity`, or
a `subject`/`predicate`/`object` triple, or a `source_run_id` to sweep one run's writes.
Run it with `dry_run: true` first and read what it would remove.

**The same thing was recorded under two names** → `memory_merge_entities`.
Fold the duplicate onto the survivor; its facts move rather than dying with it. Prefer
this over forgetting a duplicate — forgetting destroys the facts only the duplicate knew.
If the target does not exist yet, this is a rename.

## The trap that is gone, and the one that replaced it

The old memory deduplicated on write and merged a correction back onto the original
wording, silently — so "I will just add the corrected version" corrected nothing, and
the graph once carried two contradictory rules about deferred tools for exactly that
reason. **That trap no longer exists**: an upsert writes a new fact, always.

The trap now is the opposite one. Because nothing is overwritten, a correction written
WITHOUT `supersedes` leaves both versions valid at the same instant, and retrieval will
happily return the stale one. When something changed, say so with the flag; when
something was merely added, do not.
