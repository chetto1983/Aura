# Memory write rules and orphan entities: lab VM, 2026-11-28

Spec: `docs/superpowers/specs/2026-11-28-memory-write-rules-design.md`.
Plan: `docs/superpowers/plans/2026-11-28-memory-write-rules.md`.
VM .158. Model route: Anthropic `claude-sonnet-5-5`, reasoning "Auto", both sides. Driven from the
operator's cockpit session through Claude in Chrome. Read back from `aura-postgres`
(`tool_invocations`, `conversation_turns`) and from the identity's ArcadeDB database with read-only
`SELECT`s. Every memory write and removal below was made by Aura through her own tools; nothing was
written to either database by hand.

| Side | Images (container label) | Conversation |
|---|---|---|
| Before | `015c660ed` | `01a22279-04ac-7cf3-82d5-3a9457072e3b` |
| Orphan cleanup, first try | `a08fb65fd` | `01a222c2-5e09-7213-a259-f2052a75f56c` |
| Orphan cleanup, second try | `8515123db` | `01a222e1-0567-7e47-a1ec-90d4063d8fb9` |
| Named entity | `4721cf43a` | `01a2231d-c019-79d9-9dbb-c108aa20cf2c` |
| After | `4721cf43a` | `01a2231e-9699-7714-b951-9897db5865ea` |

## The four messages, before and after

Sent one turn at a time in a fresh conversation, never mentioning memory: a brother and his city;
the languages used at work; work reminders by email from January instead of Telegram; a morning
spent trying the sandbox browser.

| Measure | Before | After |
|---|---|---|
| `memory_entities` before the first write | yes | yes, and it returned `predicates` |
| Predicates written | `is_brother_of`, `lives_in`, `uses_at_work`, `uses_for_scripts`, `prefers_work_reminders_via` | `has_brother`, `lives_in`, `uses` ×3 |
| English snake_case | 5/5 | 5/5 |
| Reuse of a predicate in use | none: two coined for one relation (`uses_at_work`, `uses_for_scripts`) | `uses` (in use since 2026-10-31) reused for all three languages; `has_brother` follows `has_wife`, `has_daughter` |
| New entities are names | 6/6 | 5/5 |
| Statement length | 21–96 chars, one sentence | 32–68 chars, one sentence |
| `source` | `{}` | `{}` |
| Reminder channel (message 3) | written, as a new predicate beside `prefers_notification_channel` | not written: Aura answered that the delivery channel is a setting, not a memory, and pointed at the Profile panel |
| Session detail (message 4) | not written | not written |

The first batch of the after side failed with `idempotency_conflict`: Aura chose the key
`paolo-brother-torino-1`, which the before side had already bound to a different request. She
retried with a dated key and the write went through. Model-chosen idempotency keys can collide
across conversations; the refusal kept the memory consistent.

Both sides' test facts were removed by Aura on request, in the same conversation. After the second
cleanup, `uses` is back to its one fact from before the test, the memory holds 30 entities, and the
only entity with no FACT edge is one that predates the test.

## The six orphan entities

The before side's cleanup left Paolo, Torino, Go, TypeScript, Python and Email with no facts, and
no tool could remove them. Measured with a read-only query: 0 FACT, 0 MENTIONS, 3–4 `TOUCHED`
edges each. The prune deleted only where `bothE().size() = 0`, and the runner writes a `TOUCHED`
edge after every memory call that names an entity, `memory_forget` included.

| Try | What happened |
|---|---|
| 1 (`a08fb65fd`) | Dry runs: `entities: 1` for five names, `0` for Email (two `MENTIONS` edges from a live fact about mail notifications). The six real calls were withheld for approval. Aura relayed the card as `ask_user kind=choice` with the gateway's question, options and `resume_context` copied exactly; the operator chose "for this conversation"; every resume hook acts on `kind=approval` alone, so the grant was dropped with no log line and the retry was withheld again. Fixed in `8515123db`: a relayed host approval context makes the pause an approval. |
| 2 (`8515123db`) | Aura used `memory_batch` (the operator's standing approval on it, granted 2026-10-11, so no card). With Email in the batch it was refused, "the statements of other facts still name it", and the atomic batch changed nothing; without it, five `entities: 1`. Email stayed, but the only way left to remove it was to delete the fact that mentions it. Fixed in `918bfbae3`: a forget that names an entity removes it, with its `MENTIONS` edges, and keeps the facts that mention it. |
| 3 (`4721cf43a`) | `memory_forget {entity: Email}`: dry run `entities: 1, facts: 0`, approval card as an approval, real call `entities: 1, facts: 0`. Read-only check: 0 of the six names left; the mail-notification fact intact. |

## What this does not prove

- One run per side, one model, one route: a direction, not a rate.
- The after side never met the reuse case the rules were written for. The reminder-channel message
  was not written at all, so whether Aura would now extend `prefers_notification_channel`
  instead of coining a predicate beside it is not measured.
- Not writing message 3 is a change of judgement, not obviously an improvement: a preference that
  starts in January is arguably worth a fact. One run does not say which side is right.
- Short conversations with the skill freshly loaded. Whether the rules in the tool descriptions
  hold after a compaction has evicted the skill is argued from where they live, not measured.
- The implicit-removal rule (a name another fact mentions survives a removal that does not name
  it) is covered by the live tests, not by a VM run.
