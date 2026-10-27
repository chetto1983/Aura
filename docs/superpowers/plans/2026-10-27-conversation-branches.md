# Conversation branches the model and the cockpit can follow — Implementation Plan

**Status (2026-10-27):** implemented and validated live. Open:
- CI on the PR, which also runs the coverage gate (the operator left the gates to CI);
- the mutation gaps of Task 7.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task by task. Steps use checkbox (`- [ ]`) syntax for tracking. One atomic commit per task.

**Goal:**
- A run's model context is its own branch.
- Every turn records the path it continues.
- The cockpit shows the tree, switches branches without calling the model, and continues the
  branch on screen.

**Architecture:**
- **Persistence.** One append statement derives `parent_seq` (explicit, or `seq − 1`) and
  `branch_id`: the parent's, or a fresh one when the parent already has a child.
- **Runner.** A run has an explicit head and loads the path from it. It reports
  `head_seq` and `leaf_seq` as a `STATE_DELTA` before `RUN_FINISHED`.
- **HTTP.** `/agent/run` takes `aura.parent_seq`. The snapshot is the whole tree, with seq ids
  and `parentId`.
- **Cockpit.** It imports the tree into the assistant-ui runtime and wires `setMessages`.

**Tech Stack:** Go (`internal/db` sqlc, `internal/conversations`, `internal/runner`, `internal/agui`,
`internal/share`), React + `@assistant-ui/react` 0.15.31 (`web/src/chat`), Playwright, local compose
stack with `aura-llm`.

**Spec:** `docs/superpowers/specs/2026-10-27-conversation-branches-design.md` (approved 2026-10-27).

## Global Constraints

- **The migration slot is not used.** `branch_id` and `parent_seq` exist since 0017/0095, and no
  schema changes. If one becomes necessary, `ls internal/db/migrations/ | tail -1` gives its number.
- **sqlc v1.31.1** regenerates `internal/db/sqlc`, and the "sqlc generate is in sync" job checks it.
- **Files stay ≤ 600 LOC.** `web/src/chat/ExternalStoreChat.tsx` is at 579: anything new goes to an
  `ExternalStoreChat_*.ts` sibling.
- **A test changes only when it pins the bug.** Today `runner_branch_test.go` asserts that `Turn`
  stays on the linear loader. Its commit says why it changes.
- **Coverage floor 85% plus the package policy.** Re-pin a baseline only after checking that no
  block lost coverage.

## Review Focus

- **The KV-cache prefix invariant.** On a linear conversation the path loader must return the same
  bytes the linear loader did.
- **Concurrency.** "Already has a child" is read under the `FOR UPDATE` lock on the conversation
  row. A fork happens under the thread lock (D10).
- **The cockpit.** No message of another thread survives a thread switch. An imported branch
  survives a switch back and forth.

## Decisions taken while writing this plan

- **The regenerate placeholder stays** in the store (D6): it is hidden in the display. Removing it
  would need an explicit parent on the first turn the agent appends, through every persistence
  path of the runner.
- **D11 is a server check.** A pending pause plus a `parent_seq` other than the turn written last
  is a 409. Disabling the picker in the cockpit would not protect the other channels.

## File structure

| File | Change |
|---|---|
| `internal/db/queries/conversation_turns.sql` | `InsertConversationTurn` takes `parent_seq` and derives `branch_id`. Unused branch queries go. A display read returns seq and parent. |
| `internal/conversations/store_append.go`, `store_branch.go` | `AppendTurnParams.ParentSeq`. `ForkBranch` becomes an append. `ListBranches` and `SetBranchPointers` go. |
| `internal/conversations/store_display.go` (new) | `LoadDisplayTurns`: every turn, with seq and parent, repaired with its seq kept. |
| `internal/conversations/context.go`, `store_managed_history.go` | The linear loader goes, if the runner is its only caller. |
| `internal/runner/runner.go`, `runner_context.go`, `runner_seq_report.go` (new) | Head, `TurnFrom`, and the seq report. |
| `internal/agui/server_run*.go`, `server_run_request.go` | `aura.parent_seq` and the 409. |
| `internal/agui/conversations_branch_api.go` | Fork under the lock; `/branches` and `/select` go. |
| `internal/agui/server_display.go`, `server_project.go`, `server_messages.go` | Seq ids, `parentId`, and the hidden placeholder. |
| `internal/share/service.go` | The latest-leaf path. |
| `web/src/chat/sseAdapter_snapshot.ts`, `messageSeq.ts`, `ExternalStoreChat.tsx`, `ExternalStoreChat_tree.ts` (new), `sseAdapter.ts`, `auraRunBody.ts` | The tree, the import, the seqs, and `parent_seq`. |
| `internal/agui/branch_http_e2e_integration_test.go` (new) | Acceptance 8. |
| `docs/superpowers/verification/2026-10-27-conversation-branches-local.md` (new) | Acceptance 9. |

## Tasks

### Task 0 — PRD amendment (records the measurement before any code)
- [x] Add a dated paragraph in `prd.md` where branches are described (D-09/CHAT-05). It records:
  - the measurement of spec §1;
  - decisions D1–D12;
  - what the measurement does not show.
- [x] Correct the 2026-10-04 caveat: ids `msg-N` are positions, not seqs.
- [x] Commit `docs(prd): measure conversation branches before wiring them`.

### Task 1 — Every append records its path (D4, D10 store half)
- [x] Red: a db_integration test in `internal/conversations`:
  - a chain of appends keeps one branch id;
  - two children of one parent get different ids;
  - an append with `ParentSeq` chains to it;
  - an unknown `ParentSeq` is `ErrTurnNotFound`;
  - `ForkBranch` of a root turn and of a middle turn makes siblings with fresh ids.
- [x] Green:
  - the SQL change and sqlc regeneration;
  - `ParentSeq` threaded through `appendTurnWrites` and `insertTurnAndAggregates`;
  - `ForkBranch` rewritten on the append;
  - `SetTurnBranchPointers`, `SetBranchPointers`, `ListBranches` and `ListBranchLeaves` deleted
    once their callers are gone. The test seeds that used `SetBranchPointers` rely on the
    append's own pointers.
- [x] Gates: vet, build, `-race` on `internal/conversations` (unit and db_integration).
- [x] Commit `feat(conversations): every turn records the path it continues`.

### Task 2 — A run loads its own path and reports its seqs (D3, D7)
- [x] Red: `internal/runner` db_integration, the probe with assertions (acceptance 1 and 2).
  Unit tests with `fakeConvStore`:
  - the head is the user seq, the fork leaf, or `MAX(seq)` on a resume;
  - the last event before the end carries `head_seq` and `leaf_seq`.
- [x] Green:
  - `turnInput.parentSeq` and `TurnFrom`;
  - `loadTurnHistory(head)`;
  - the seq report in `runner_seq_report.go`;
  - the linear loader deleted if dead;
  - `runner_branch_test.go` rewritten, with the reason in the commit.
- [x] Byte identity: the path loader on a linear conversation equals the old linear output. The
  test is written before the linear loader is deleted. The CI "KV-cache prefix invariant" job
  stays green.
- [x] Timing: 2,000 turns through the path loader, recorded in the commit body. Measured at
  1,000 and 5,000 turns instead (`a582061`).
- [x] Commit `fix(runner): a run's history is its own branch`.

### Task 3 — HTTP: `parent_seq`, the pending-pause guard, the fork under the lock (D2, D9, D10, D11)
- [x] Red, agui unit tests:
  - `aura.parent_seq` 0, string or negative is 400; an unknown one is 404;
  - with a pending pause, a stale branch is 409;
  - the detached path passes it on;
  - `/edit` forks only after taking the lock;
  - `/branches` and `/select` answer 404/405.
- [x] Green: the changes, and `cmd/aura/serve_webui_test.go`'s route list.
- [x] Commit `feat(agui): a run continues the branch the client names`.

### Task 4 — The snapshot is the tree (D5, D6, D12)
- [x] Red: agui unit tests on the projection (acceptance 4), with a repaired orphan tool result
  keeping its seq. A share snapshot of a forked conversation holds one path.
- [x] Green: `LoadDisplayTurns`, the projection, and share.
- [x] Commit `feat(agui): the history snapshot carries the conversation tree`.

### Task 5 — The cockpit imports the tree and continues the branch on screen
- [x] Red, Vitest (acceptance 6 and 7):
  - the snapshot becomes branchable items: a tool row maps to its card, a placeholder and a worker
    report re-parent;
  - the import binds the messages;
  - Previous after a load shows the other branch with no fetch;
  - a send posts `parent_seq` = `leafSeq`;
  - the `STATE_DELTA` seqs land on the live messages;
  - thread A's messages are gone after a switch to B.
- [x] Green: `ExternalStoreChat_tree.ts`, the adapter (`setMessages`, `onImport`),
  `sseAdapter.ts`, `auraRunBody.ts`, and `messageSeq.ts` without the `index + 1` guess.
- [x] Gates: `npm run lint`, `npm run typecheck`, `npm test` in `web/`, Stryker on the touched
  files if the job covers them.
- [x] Commit `feat(web): the cockpit navigates and continues conversation branches`.

### Task 6 — HTTP E2E (acceptance 8)
- [x] `internal/agui/branch_http_e2e_integration_test.go`, db_integration:
  - `NewServer(runner.New(...real store, FakeClient...), store, ServerConfig{})`;
  - run, edit, then run with `parent_seq`;
  - assert each `FakeClient` request and the `GET /threads/{id}/messages` tree.
- [x] Commit `test(agui): drive branches end to end over HTTP`.

### Task 7 — Gates
- [x] `make quality`: lint, deadcode, file-size, race, vuln. Green at `1aa073a`.
- [ ] `bash scripts/coverage_docker.sh` with the package policy. Re-pin only after checking.
  Left to CI's coverage gate on the PR.
- [ ] Mutation ≥ 70% (`go-mutesting`) on:
  - `store_append.go` (the branch derivation): 100%;
  - `runner_context.go`: 58.3%. All nine survivors are in code this plan did not touch (the
    compaction summarizer, the memory context and recall, the profile block);
    `loadTurnHistory` has none;
  - the snapshot projection: not measured, because the run was stopped.
- [x] Stryker on `ExternalStoreChat_tree.ts`: 93.5%. `messageSeq.ts` 93.1%,
  `sseAdapter_snapshot.ts` 76.7%.

### Task 8 — Live validation (acceptance 9)
- [x] Bring up the local stack with `aura-llm` (gemma-4-12B through `scripts/fetch_llm_model.sh`)
  and `aura serve`. Gemma was dropped for the reviewer as the model, by operator choice: see
  the verification file.
- [x] Run a Playwright script:
  - send, edit, Previous, send, reload, with a regenerate before the reload for the third leaf;
  - read the tree from `GET /threads/{id}/messages`.
- [x] Write `docs/superpowers/verification/2026-10-27-conversation-branches-local.md`: what it
  measured, and what it does not show.

### Task 9 — Close
- [x] Update `docs/unwired-and-duplicates-2026-10-26.md`: the branch routes are deleted, and the
  flow is wired.
- [x] Update the PRD paragraph with the measured results.
- [ ] Set this plan's status. Open the PR after #192 merges.
