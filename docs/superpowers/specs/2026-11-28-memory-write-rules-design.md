# Memory writes: the rules where the write happens

## Why

On the lab VM on 2026-11-28 (conversation `01a2223e`, Claude route), Aura wrote facts that broke
its own memory skill: no `memory_entities` call before writing, a description coined as an entity
(`debug di Aura dall'interno`), a fact about that day's test, hand-written `user_turn:current`
source ids. The memory it was asked to tidy already held predicates in two languages
(`has_wife`, `decisione_architettuale`, one with spaces) and paragraph-long statements.

Measured, not assumed:

- `internal/skills/embed/memory-aura/SKILL.md` is 30,939 bytes. The rules exist (line 55
  `source: {}`, line 104 `memory_entities` first, line 165 no session debris), deep in the text.
- By turn #227 the skill's tool result was out of the model's context: Aura had to call
  `read_tool_output` to read it again. A 30 KB tool result is what compaction evicts first.
- The skill has no rule on predicate naming and none on statement length.
- The per-turn memory pointer (`cmd/aura/serve_memory_context.go`) deliberately carries only read
  routing, so it is not the place for write rules.

## Goals

- The few write rules are in front of the model at the moment it writes, whatever compaction did:
  in the descriptions of `memory_upsert_fact` and `memory_batch` and their fields, which ride in
  every request.
- The skill opens with the same rules as a short section, and gains the two missing ones:
  predicate naming and statement length.
- `memory_entities` also returns the predicates in use, so the call the rules already require
  before writing shows both the names and the relations to reuse (added on the operator's word,
  2026-11-28: no memory tool listed predicates, so "reuse the predicate" could not be followed).
- Measured before and after on the lab VM with the operator's Sonnet route.
- Aura can remove an entity left with no facts, with her own tools (added on the operator's
  word, 2026-11-28, after the baseline cleanup left six such entities she could not remove).

## Non-goals

- No change to compaction, and no pinning of skill results.
- No server-side rejection of a bad predicate: the rule and the listing come first, and a
  rejection is decided only if the measurement shows they are not enough.
- No cleanup of the existing memory by hand: that is Aura's job in the cockpit, on the
  operator's word. Where her tools cannot do it, the tool is fixed.

## Approaches

- A: the skill only. Cheap, but in a long conversation the skill leaves the context again.
- **B (chosen by the operator): the tool descriptions and the skill.** The rules sit where the
  write is made and cannot be compacted away; the skill keeps the full reasoning.
- C: keep the skill in context. 30 KB every turn and a compaction change. Rejected.

## Design

1. The rules, one wording shared by both places:
   - call `memory_entities` once before the first write, and take subject and object from it when
     one fits;
   - a new name is a noun someone would ask about again, never a description;
   - the predicate is English snake_case, one verb phrase per concept, reused when an existing
     fact already says the same relation;
   - the statement is one or two sentences;
   - `source: {}` when there are no message ids, never an invented one;
   - nothing that will not matter in a month: no record of what this conversation just did.
2. `memory_upsert_fact`: the tool description gains a short rules paragraph; the `subject`,
   `predicate`, `object` and `statement` field descriptions carry their rule. `memory_batch`
   names the same rules for its `upsert_fact` operations.
3. `SKILL.md`: a "Before any write" section right after the opening, the same rules, plus a
   predicate and a statement subsection under Writing. The rest unchanged.
4. The wording lives once in Go (a constant the two tools share); the skill repeats it in prose,
   and a test keeps the skill's section naming each rule.
5. `memory_entities` gains `predicates`: each predicate of a currently valid fact with how many
   such facts use it, most used first, capped like the entity list, with its own total so a
   truncated list is not read as complete. `arcadedb.Client.ListEntities` runs one more
   grouped query over the FACT edges (the ArcadeDB SQL `GROUP BY` page is read before writing
   it). The tool description says the list is there to reuse a predicate before coining one.

6. Orphan entities. Measured on the VM on 2026-11-28 (read-only query): the six entities the
   baseline left have 0 `FACT`, 0 `MENTIONS` and 3 or 4 `TOUCHED` edges each. `TOUCHED` is the
   reasoning-trace edge the runner writes after every memory tool call that names an entity,
   `memory_forget` included. Both prune statements delete only where `bothE().size() = 0`, so an
   entity Aura ever wrote or tried to forget is never an orphan to the database. Each attempt
   adds another edge. `memory_entities` and `graph_diagnostics` count `FACT` alone and show it
   with no facts. `memory_batch` reported the entities as removed because it counts its own
   model, which also counts `FACT` alone.
   - An entity is an orphan when neither a `FACT` edge nor a `MENTIONS` edge holds it:
     `bothE('FACT','MENTIONS').size() = 0` in the `memory_forget` prune, its `dry_run` count,
     and the `memory_batch` commit. `TOUCHED` never holds it. ArcadeDB deletes a vertex with
     its edges (`sql-delete.adoc`: "a vertex is deleted with its edges all the same").
   - Amended on the final review, operator's ruling 2026-11-28: the first version counted
     `FACT` alone, so an entity with no facts of its own but named in another fact's
     statement was deleted with its `MENTIONS` links, which nothing re-creates. It stays.
     A `MENTIONS` edge goes with the fact it was read from (`fact_rid`), in both forget
     paths, so it never holds a name after its fact is gone, and the names it touched join
     the prune candidates.
   - `memory_batch` `forget {entity}` on an entity with no facts removes the entity. A name
     that does not exist is still `target_not_found`.
   - Amended again after the lab VM run, 2026-11-28 (conversation `01a222e1`): a forget that
     NAMES the entity removes it once no `FACT` edge holds it, `MENTIONS` included, and the
     facts whose statements mention it stay. Only a removal that does not name it (by run,
     by subject) keeps a mentioned name. Held against an explicit forget, the only way to
     clear `Email` was to delete the operator's real preference that mentions it.
   - The batch model and the delete then use the same condition, so the reported count is
     what was removed. A live test checks the entity no longer exists.

## Measurement

The same scripted conversation before (current images) and after (new images), in a fresh
cockpit conversation on Sonnet: the operator states four durable facts in Italian and one detail
of the session. Counted from `aura.tool_invocations` and the memory:

- `memory_entities` called before the first write;
- predicates in English snake_case, and reused where one exists (with the listing, after);
- subjects and objects that are names, not descriptions;
- statement length in characters;
- whether the session detail was written.

The test facts are removed afterwards with `memory_forget` by `run_id`, approved by the operator.

## What this does not prove

- One run per side, one model: a direction, not a rate.
- Long conversations: the run is a few turns. Whether the tool-description rules hold after a
  compaction is argued from where they live, not measured.
