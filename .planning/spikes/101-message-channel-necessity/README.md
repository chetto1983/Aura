---
spike: 101
idea: durable-delegation
name: message-channel-necessity
type: standard
validates: "Given a background run that finishes while nobody is watching, when its result must reach the operator, then the measurement shows whether Aura needs agent_message_send and a four-table messaging schema or already owns a working delivery path"
verdict: validated
related: [098, 099, 100]
tags: [messaging, delivery, channels, inventory]
---

# Spike 101: message-channel-necessity

## What This Validates

D-02 asks whether background delegation needs a message channel of its own — whether
`agent_message_send` and the four-table messaging schema of the 2026-06-29 design belong in
Phase 51 at all. INVENTORY BEFORE INVENTION says answer it by enumerating what already
delivers, not by designing a channel and checking after.

## Research

### LibreChat has no message channel. Not a small one — none.

Forty schemas in `packages/data-schemas/src/schema/` and not one is a delivery channel: no
message-send tool, no push table, no notification collection (grep for `message_send`,
`sendMessageTool`, `pushNotification` returns nothing). **The conversation IS the channel.**
A detached run that finishes with nobody attached simply saves its response message
(`api/server/controllers/agents/request.js:700-706`, an upsert keyed by `messageId`) and the
operator sees it the next time they open the conversation. When every client has left
mid-generation, the same closure saves what it has with `unfinished: true` (`request.js:291-349`).

Delivery, for LibreChat, is not a subsystem. It is a row in the conversation plus a client
that eventually reads it.

### Aura already has both delivery modes, and neither is a messaging schema

| the operator is… | mechanism | state |
|---|---|---|
| **present, mid-turn** | the steer rail — `internal/steer.Inbox`, `drainSteer` | shipped (Phase 52); spike 098 proved the rail carries a worker report and that it needs its own ENVELOPE, not its own channel |
| **absent** | `aura.pending_notifications` + `internal/cron/deliver.go` → `channels.Registry.DeliverToIdentity` | shipped (Phase 20 R4/R7) |

The absent-operator path is a complete delivery system: a durable outbox with
`notify_after` backoff, an `attempts` budget, `last_error`, `status ∈ {pending, failed,
delivered}`, and a tri-state contract that refuses to double-deliver —
*"(true,nil)=delivered → stop; (false,nil)=no channel owns the identity → caller falls back
to the route; (false,err)=owns-but-failed → caller queues a same-channel retry and does NOT
fall back"* (`deliver.go:20-28`).

## Results

### Measured live: a cockpit-originated background run delivered to Telegram

The operator scheduled a reminder **from the web cockpit** and it arrived **on Telegram**.

| | observed |
|---|---|
| scheduled task | `kind=reminder`, `notify_route` **empty**, identity `b130c94d…` |
| run | `completed` at 22:16:39, `summary = SPIKE101-DELIVERED`, 4 ms |
| where it landed | Telegram |
| rows in `aura.pending_notifications` | **0** |
| rows in any messaging table | **0** — none exists |

Two things fall out of that, and the second corrects the code's own headline example.

**1. The happy path never touches the outbox.** Delivery is inline via
`DeliverToIdentity`; `pending_notifications` receives a row only on deferral (quiet hours)
or failure. So the durable queue is the retry substrate, not the delivery substrate — which
is the right shape, and it is already built.

**2. Delivery goes to the channel that OWNS the identity, not to the channel the request
came from.** `Registry.DeliverToIdentity` (`internal/channels/registry.go:119-144`) walks
every started channel in `sort.Strings` name order and takes the first that claims the
identity — *first-delivers-wins*. It has no notion of origin. `deliver.go`'s comment
illustrates the mechanism with *"a reminder set in a Telegram DM lands back in that DM"*,
which is true but is not why: in that example Telegram is simply the only channel that can
push. This run proves the general case, because origin and destination were different.

For Phase 51 that is the useful half: **a delegated worker's completion will reach the
operator wherever they can be pushed to, regardless of where they asked.** That is exactly
what background delegation needs, and it works today.

### The gap is a path, not a channel

`internal/swarm` and `internal/agent` contain no reference to `DeliverToIdentity`,
`pending_notifications`, or any notification store — grep returns nothing. The swarm cannot
reach the delivery system that already exists. Nothing needs building; something needs
connecting.

### Two facts worth flagging rather than burying

- **Telegram is the only `Deliverer` in the tree.** It is the sole implementation of the
  interface and the only channel started in this deployment, so *first-delivers-wins* has
  never actually chosen between two candidates. If a second pushable channel is ever added,
  `sort.Strings` decides — alphabetical order, not preference. That is a design smell in
  waiting, not a bug today.
- **The cockpit cannot be a delivery destination**, because it implements no `Deliverer` and
  structurally cannot push to an absent operator. That made an operator who asked from the
  cockpit get answered on their phone — which was a bug, and is fixed below.

## The bug this spike found, and its fix

The operator's own run WAS the measurement: they scheduled `SPIKE101-DELIVERED` from the
cockpit, it arrived on Telegram, and the cockpit conversation ended at *"Promemoria
programmato"* having never learned the outcome.

Nothing was missing from the data. `scheduler_tasks.origin_conversation_id` was recorded all
along (`01a04023-b54d-…` on that very task) and is threaded into the `Job` — only the
approval-pause path ever read it.

The fix is LibreChat's own rule, and it is the one thing LibreChat makes durable: **the
conversation IS the channel**. So the push stays and a record joins it, because they answer
different people — a push finds an operator who is elsewhere, a record answers the one still
looking at the conversation they asked from. `Dispatch` now also appends the outcome to
`OriginConversationID` via a cron-local `ConversationRecorder` (the `ChannelDeliverer`
idiom), reusing `isSilentSuccessKind` so a housekeeping sweep stays quiet, recording
failures for every kind on notify's D-21 reasoning, and warning rather than failing the run
if the write does not land.

Validated live after a rebuild — same cockpit path, same reminder:

| | before | after |
|---|---|---|
| origin conversation's last turn | seq 6, *"Promemoria programmato"* | **seq 7, `assistant: SPIKE101-DELIVERED`** at 22:38:39 |
| Telegram push | delivered | delivered (unchanged) |

Commit: `fix(cron): answer where the question was asked`.

## Verdict — validated

**`agent_message_send` and the four-table messaging schema do NOT belong in Phase 51.**

1. The reference implementation has no message channel at all — the conversation is the
   channel.
2. Aura has two delivery paths already, one for a present operator (the steer rail) and one
   for an absent one (outbox → identity-owning channel), and the second was measured
   end-to-end on the live stack, across a channel boundary, with zero rows in any messaging
   table.
3. What is missing is a path from the swarm into that system, not a system.

Building the schema would put a third delivery mechanism beside two working ones. Spike 098
already named the real gap on the present-operator side and it is not a channel either: the
rail carries a worker's report fine, but `<user_steer>` declares the operator as author, so
a worker report needs a **second envelope**, not a second channel.

## Investigation Trail

1. Grepped the tree for `agent_message_send`: it exists only in planning documents and the
   stale 2026-06-29 design. Never built.
2. Listed the schema for anything message-shaped: found `pending_notifications` and
   `adaptive_outbox` — two delivery substrates already in place.
3. Read `pending_notifications`' columns and its four queries, then `cron/deliver.go` and
   `cron/dispatch.go`, and established the outbox is the deferral/retry path rather than the
   primary one.
4. Read LibreChat's forty schemas looking for a channel and found the absence.
5. The operator scheduled `SPIKE101-DELIVERED` from the cockpit; it arrived on Telegram.
   Confirmed the run in `agent_job_runs` and the empty outbox in Postgres.
6. Read `Registry.DeliverToIdentity` to explain the cross-channel landing, and found the
   selection is alphabetical first-delivers-wins with no origin concept — which corrects the
   headline example in `deliver.go`'s own comment.

## What This Spike Does NOT Prove

- **One delivery, one channel, one identity.** Telegram is the only `Deliverer` that exists,
  so nothing here exercises the fan-out's choice between candidates or its
  owns-but-failed leg.
- **The outbox was never exercised.** Its retry, backoff and dead-letter behaviour are read
  from the schema and the queries, not measured — the happy path bypassed it entirely, and
  no failure was induced.
- **Nothing here says how a worker's report should be phrased or attributed.** That is spike
  098's second-envelope finding, and it is unaffected by this one.
- **The cockpit-answers-on-Telegram behaviour was observed, not judged.** Whether a
  delegated result should follow the operator to their phone or wait for them in the cockpit
  is a Phase 51 decision this spike deliberately leaves open.
