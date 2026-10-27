# Conversation branches the model and the cockpit can follow — design

Date: 2026-10-27
Status: approved by the operator 2026-10-27 ("Navigazione client"). Plan:
`docs/superpowers/plans/2026-10-27-conversation-branches.md`.

## 1. Why

The 2026-10-26 scan (`docs/unwired-and-duplicates-2026-10-26.md`) listed
`GET /api/conversations/{id}/branches` and `POST …/branches/{branchSeq}/select` as routes the
cockpit never calls. The operator asked to wire and validate them, measuring first.

Measured on 2026-10-27, with a db_integration probe: real Postgres, the real `Runner`, and
`agenttest.FakeClient` as the model. The probe did the following:
1. Sent U1 and got A1.
2. Edited U1 into U1′ (`ForkBranch` and `TurnBranch`, which is what `POST /edit` does), and got A1′.
3. Sent U2 and got A2.
4. Regenerated A2 (`ForkBranch` with role assistant and empty content, then `TurnBranch`), and got A2′.

What it showed:
- **The model is sent both branches.** The request for U2 carried U1, A1, U1′, A1′ and U2, in
  that order. `Runner.Turn` loads `LoadManagedHistory`, and its page query
  `ListManagedTurnsPageBySeq` has no branch filter.
- **The history is one flat list.** `GET /threads/{id}/messages` returned 8 messages:
  U1, A1, U1′, A1′, U2, A2, an empty assistant message, A2′. `ListTurnsBySeq` has no branch
  filter either. The empty message is the placeholder `ForkBranch` writes for a regenerate:
  the model context drops it, and the cockpit draws it as an empty bubble.
- **Branch ids stop at the fork.** `ListBranches` reported the leaves 2, 6 and 8, all on the
  canonical branch. Only the forked turns (3 and 7) carry a fresh `branch_id`.
  `InsertConversationTurn` gives every other turn the column default. So a compaction summary
  for the forked path is filed under the canonical branch.
- **The picker cannot switch branches.** `useExternalStoreRuntime` gets no `setMessages`. In
  `@assistant-ui/core` 0.3.31, `switchToBranch` is enabled only when `setMessages` is set
  (`external-store-thread-runtime-core.js:293`), and the picker buttons are disabled without it.
  This was read in the source, not clicked in a browser.
- **One runtime serves every thread.** `AppShell` mounts one `ExternalStoreChat` with no `key`.
  The runtime's message repository therefore keeps every thread it has shown. This was also read
  in the source.
- **`/agent/run` cannot continue an older branch.** Its body carries only the new user text.
  `appendUserTurn` chains the new turn to `seq − 1`, the latest turn written.

What the measurement does not show:
- Production data: no lab VM conversation was read. How many conversations already hold forks
  is unknown.
- Conversations with tool rounds: the probe's turns are text-only.
- The browser: no click was measured.

## 2. Goals, non-goals

**Goals**
1. The model context of a run is the path from the run's head to the root, and never another
   branch.
2. Every new turn records the path it continues, as `parent_seq`, and a `branch_id` that is the
   same along one path and different between siblings.
3. The cockpit opens a thread on the branch written last. It shows `n / m` on a message that has
   siblings, and its arrows switch branch without calling the model.
4. A send continues the branch on screen, whichever it is.
5. A reload, or opening the thread on another device, shows the same tree.

**Non-goals**
- Repairing `branch_id` on turns written before this change. Section 1 says the cost is extra
  summarizer calls, not a wrong context: the loader uses a stored summary only when its boundary
  seq lies on the walked path (`loadManagedBranchTurns`).
- Showing branches on Telegram or WhatsApp. Their turns continue the branch written last, as they
  do today.
- Deleting a branch.

## 3. Decisions

| # | Decision | Why |
|---|---|---|
| D1 | Navigation is client-side. assistant-ui switches branches inside its own repository; no request is made. | Operator, 2026-10-27. `POST …/select` re-runs the model, so a click would answer again. |
| D2 | `/agent/run` takes an optional `aura.parent_seq`: the last turn of the branch on screen. Without it, the head is the turn written last. | Only the client knows which branch is on screen. Channels and old clients keep today's behaviour. |
| D3 | A run's head is explicit: the appended user turn (`Turn`), the fork leaf (`TurnBranch`), or the turn written last (a resume). Its history is the path loaded from that head. | Goal 1. The linear loader has no notion of a branch. |
| D4 | A new turn inherits its parent's `branch_id`. A turn appended under a parent that already has a child starts a new branch id. | Goal 2. It covers `ForkBranch` and `parent_seq` alike. |
| D5 | The snapshot carries every turn of every branch, with `id: msg-<seq>` and `parentId: msg-<parent_seq>` (null at the root). | Goal 5. Ids become seqs, which removes the "position, not seq" caveat the PRD records for search and compaction (2026-10-04). |
| D6 | A regenerate placeholder (an assistant turn with no content, no tool calls and no reasoning) is not drawn. Its children hang from its parent. | Measured: an empty bubble today. The model context already drops it. |
| D7 | Each run reports its seqs on the stream. A `STATE_DELTA` before `RUN_FINISHED` carries `head_seq` and `leaf_seq`. | Live messages have no seq. Today the cockpit guesses `index + 1`, which a fork or a tool round makes wrong. |
| D8 | The history load imports the tree with `runtime.thread.import(ExportedMessageRepository.fromBranchableArray(…, { headId }))`. Each imported message is bound to its cockpit object (`bindExternalStoreMessage`), and `setMessages` and `onImport` are wired. | The base `import` clears the repository first (`base-thread-runtime-core.js:313`), so threads stop sharing one. Binding is what lets a switch to an imported branch hand back the cockpit's own objects (`getExternalStoreMessages`). All are public exports of `@assistant-ui/react` 0.15.31. |
| D9 | `GET …/branches`, `POST …/branches/{seq}/select`, `ListBranches`, `ListBranchLeaves` and any code left without a caller are deleted. | D1 leaves them with no caller (CLAUDE.md: no dark code). |
| D10 | `ForkBranch` runs under the thread lock, before the re-run, not before taking the lock. | Today a run that appends between the fork and `TurnBranch` would become the fork's parent. |
| D11 | A run whose `parent_seq` is not the turn written last is refused with 409 while the conversation has a pending pause. | A resume appends after the turn written last. A run on another branch in between would graft the resumed tool result onto it. |
| D12 | The share snapshot shows the path of the branch written last. The owner export keeps every turn. | Share is what a reader sees. Export is the data. |

## 4. Design

### 4.1 Persistence

- `InsertConversationTurn` gets an optional `parent_seq`. When it is null, the parent is
  `seq − 1`, as today.
- `branch_id` comes from the parent in the same statement:
  - the parent's branch, when the parent has no other child;
  - a fresh uuid, when it already has one;
  - canonical, at the root of an empty conversation.
- The append keeps its `FOR UPDATE` lock on the conversation row, so "already has a child" is
  read under the lock.
- `ForkBranch` becomes an append with `parent_seq = diverge.parent_seq` and keeps its
  `ErrTurnNotFound`. `SetTurnBranchPointers` and `SetBranchPointers` go if nothing else calls them.
- `AppendTurnParams.ParentSeq` (int, 0 = default) carries the parent. Only `appendUserTurn` and
  `ForkBranch` set it.

### 4.2 Runner

- `turnInput` gains `parentSeq`. `Turn` keeps its signature. A new
  `TurnFrom(ctx, convID, parentSeq, userMsg)` sets the parent, and so does the
  `TurnWithModelUserMessage` variant. The exact shape is in the plan.
- Head:
  - the seq of the appended user turn;
  - the `TurnBranch` leaf;
  - for a resume (no user message), `MAX(seq)`.
- `loadTurnHistory` always calls `LoadManagedHistoryForBranch(head)`. The linear loader
  (`LoadManagedHistory`, `loadManagedLinearTurns`) goes if nothing else calls it. The
  `TestTurn*` pin in `runner_branch_test.go` that says `Turn` stays linear is rewritten:
  it pins the bug.
- At the end of a run, after persistence, the runner yields one event with
  `StateDelta{"head_seq": h, "leaf_seq": l}`, where `l` is `MAX(seq)` read under the lock.
- `parent_seq` must name a turn of the conversation, or the run fails with
  `ErrTurnNotFound`, which maps to 404.

### 4.3 HTTP

- `/agent/run`: `aura.parent_seq` is a positive int. A value ≤ 0 or a non-int is a 400. Both the
  detached and the inline path pass it on.
- D11's 409 is checked before the run starts.
- `POST /edit` is unchanged on the wire, apart from D10.
- `GET /threads/{id}/messages`: the ids and `parentId` of D5, and the placeholder of D6. The
  turns come from a new store read that returns seq and parent with every message.
  `repairToolMessagePairs` must keep that association: a repaired or synthesized message
  carries the seq of the turn it stands for. The plan pins the rule with a test.
- `retention_api` (owner export) keeps every turn. `share` uses the latest-leaf path.

### 4.4 Cockpit

- `snapshotToThreadMessages` returns `{ message, parentId }[]` and the head id:
  - a merged tool row maps its seq to its card, so a child of a tool row hangs from the card;
  - a skipped worker report and a hidden placeholder map to their parent's message;
  - `metadata.custom` carries `backendSeq`, the first seq of the message, and `leafSeq`, the
    last turn folded into it.
- The history effect builds the repository, binds every message, and imports it.
  `onImport` sets `messages`.
- `useExternalStoreRuntime` gets `setMessages` and `onImport`.
- `onNew` sends `parent_seq`, the `leafSeq` of the last visible message, when it is known.
- The streamed assistant receives `head_seq` and `leaf_seq` from D7. The fold writes
  `backendSeq` on the user message it sent, and `backendSeq = head_seq + 1` and
  `leafSeq = leaf_seq` on the assistant message.
- `backendSeqAt` no longer guesses `index + 1`. An unknown seq is `undefined`:
  - an edit or a regenerate on such a message is refused;
  - a send omits `parent_seq`.

  Corrected 2026-10-27: the edit composer cannot keep the text. In `@assistant-ui/core` 0.3.31,
  `DefaultEditComposerRuntimeCore.handleSend` resets and closes it right after `runtime.append`,
  whatever `onEdit` does. A refused edit posts nothing and loses the edited text.

## 5. Acceptance (machine-checkable)

1. **db_integration, runner.** After fork, send and regenerate, every request the model
   received holds only its own path. This is the probe of §1 with assertions.
2. **db_integration, runner.** `aura.parent_seq` pointing at A1 after a fork:
   - the model receives U1, A1, U3;
   - U3's parent is 2 and its `branch_id` is canonical;
   - the turn after it inherits that branch id.
3. **db_integration, conversations.** Two children of one parent have different `branch_id`s.
   A chain of appends keeps one.
4. **agui unit.** The snapshot ids are `msg-<seq>` and the `parentId`s match the stored
   pointers. A placeholder is not drawn, and its child hangs from the placeholder's parent.
5. **agui unit.** `aura.parent_seq`:
   - 0, a string or an unknown seq is refused;
   - with a pending pause, a stale branch gets 409.
6. **Vitest.** After a history load with two branches:
   - the picker reads `2 / 2`;
   - Previous shows the other branch without a fetch;
   - a send after Previous posts that branch's `leafSeq`.
7. **Vitest.** Switching thread A to thread B leaves none of A's messages in the runtime
   (`runtime.thread.export()`).
8. **db_integration, HTTP E2E** (`internal/agui`). It runs on real Postgres, the real `Runner`
   and `agenttest.FakeClient`, through `NewServer(...).Mux()`:
   - `POST /agent/run`, then `POST /edit`, then `POST /agent/run` with `parent_seq` set to
     the first answer;
   - each request the model received holds only its own path;
   - `GET /threads/{id}/messages` returns the tree.
9. **Live run, recorded** in `docs/superpowers/verification/2026-10-27-conversation-branches-local.md`
   (operator choice, 2026-10-27). Playwright drives `aura serve` on the local stack. The model
   planned was compose's `aura-llm` (gemma-4-12B); the run used the reviewer instead (correction
   below):
   - send, edit, then go back with the arrow;
   - send again, then reload;
   - the tree holds three leaves, and the reload opens on the branch sent last.

   Corrected 2026-10-27 by the run:
   - those steps leave two leaves, and the third needs a regenerate, which the run adds;
   - the model was the reviewer through a llama.cpp-shaped endpoint, not gemma-4-12B (operator
     choice: on 4 CPU cores and no GPU the estimate was tens of minutes per turn).
10. Coverage gate and package policy green. Mutation ≥ 70% on the critical files the plan names.

## 6. Risks

- **The KV-cache prefix invariant.** The path loader must return the same head bytes the linear
  loader did for a linear conversation. The CI gate "KV-cache prefix invariant" must stay green,
  and the plan adds a byte-identity test on a linear conversation.
- **The recursive walk on long conversations.** It is paged
  (`ListManagedBranchPathPage`, `AURA_HISTORY_HARD_CAP_TURNS`). Timing 2,000 turns is a plan step.
- **Experimental APIs.** `bindExternalStoreMessage` and `unstable_onBranchChange` are marked
  experimental. Only `bindExternalStoreMessage` is used, and Vitest pins it.
- **Old clients.** A client without `parent_seq` continues the turn written last, which is the
  branch it shows unless it navigated.
