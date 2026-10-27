# `/compact` on the branch on screen, and no refused edit — Implementation Plan

**Status (2026-10-27):** done, in PR #195. The three files are above the 70% mutation floor.
Open: a live `/compact` with a real summarizer.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task by task. Steps use checkbox (`- [ ]`) syntax for tracking. One atomic commit per task.

**Goal:**
- `/compact` condenses the branch the operator is looking at, and the marker shows that
  branch's summary.
- The cockpit never opens an edit it cannot save.

**Architecture:**
- **Store.** `Compact` takes the leaf seq. `LoadPathCompaction` reads the row of a leaf's branch.
- **HTTP.** Both compaction routes take `?leaf_seq`, validated with the helper
  `aura.parent_seq` uses.
- **Cockpit.** The compaction hook sends the on-screen leaf and keys its read by it. Edit and
  Regenerate render only on a message with a known seq.

**Tech Stack:** Go (`internal/conversations`, `internal/runner`, `internal/agui`), React +
`@assistant-ui/react` 0.15.31 and TanStack Query (`web/src/chat`).

**Spec:** `docs/superpowers/specs/2026-10-27-conversation-branches-design.md` §7 (D13–D15,
acceptance 11–14).

## Global Constraints

- **No migration.** The compaction row is keyed (conversation, branch) since 0096.
- **Files stay ≤ 600 LOC.**
- **A test changes only when the API it calls changes.** The compaction fakes take the leaf.

## Review Focus

- **The default stays the turn written last.** Telegram, WhatsApp and old clients send no leaf.
- **The marker is never another branch's.** It is kept only while the previous leaf is on
  screen.

## Decisions taken while writing this plan

- **A strict branch read for the HTTP path.** `branchIDForLeaf` falls back to the canonical
  branch on any error, which the ladder needs. `LoadPathCompaction` must not draw the canonical
  marker on a read failure, so it uses a strict `branchOfTurn` that `branchIDForLeaf` wraps.
- **The on-screen leaf is the one a send would continue:** `leafSeqOf` of the last message.
  When that message has no seq yet, the request names no leaf and the server takes the turn
  written last. Such a message was written by this tab's run, so that turn is on the branch
  on screen.

## File structure

| File | Change |
|---|---|
| `internal/conversations/compaction_request.go` | `Compact(ctx, id, leafSeq, cfg)`. |
| `internal/conversations/store_compaction.go` | `branchOfTurn`, `LoadPathCompaction`. |
| `internal/runner/interfaces.go`, `runner_compact.go` | The leaf goes through. |
| `internal/agui/server_run_branch.go` | `requireTurnSeq`, shared with the compaction routes. |
| `internal/agui/conversations_compaction_api.go` | `?leaf_seq` on both routes. |
| `web/src/chat/compaction/api.ts`, `useCompaction.ts`, `useThreadCompaction.ts` | The leaf in the POST, the read and the key. |
| `web/src/chat/messageSeq.ts` | `backendSeqOf`, `showsSeq`. |
| `web/src/chat/ExternalStoreChat_messages.tsx` | Edit and Regenerate gated on a known seq. |

## Tasks

### Task 1 — The store and the runner compact a named leaf (D13, acceptance 11)
- [x] Red, db_integration in `internal/conversations`, on a forked thread:
  - `Compact` with the older leaf;
  - `LoadPathCompaction` for each leaf and for 0.
- [x] Green: `Compact(…, leafSeq, …)`, `branchOfTurn`, `LoadPathCompaction`, and the runner seam.
  The runner had no test of `CompactConversation`; one is added, which gives the fake's
  `compacted` field its first reader.
- [x] Gates: vet, build, `-race` on `internal/conversations` (with db_integration) and
  `internal/runner`.
- [x] One commit with Task 2: the agui seam is a type assertion, so a commit with the store
  alone would have answered 503 to every `/compact`.

### Task 2 — HTTP: `?leaf_seq` on both compaction routes (D13, acceptance 12)
- [x] Red, agui unit: 400, 404, and the leaf reaching the compactor and the reader.
- [x] Green: `requireTurnSeq`, used by `continueBranch` and both routes.
- [x] Commit `feat: compact the branch the client names`, with Task 1.

### Task 3 — The cockpit compacts and marks the branch on screen (D14, acceptance 13)
- [x] Red, Vitest: the POST and the read name the on-screen leaf after Previous, and the
  marker stays while a run grows the branch. A switch drops the marker of the branch left.
- [x] Green: `api.ts`, `useCompaction.ts`, `useThreadCompaction.ts`, `messageSeq.ts`.
  - The read waits for a drawn transcript. Without that, opening a thread read the marker
    twice: once with no leaf before the history landed, then with its leaf.
  - Two hand mutations of the placeholder rule each fail their own case: "never keep" and
    "always keep".
- [x] Commit `feat(web): /compact condenses the branch on screen`.

### Task 4 — No refused edit (D15, acceptance 14)
- [x] Red, Vitest:
  - a user message with no seq has no Edit;
  - an assistant message with no seq has no Regenerate;
  - a server refusal leaves the edited text on screen. This one passed before the change: it
    pins the spec's claim rather than a fix.
- [x] Green: `ExternalStoreChat_messages.tsx`.
- [x] `e2e/voice.spec.ts` mocked a run without the seqs every real run reports, and its
  no-voice case reads Regenerate. The mock now carries them.
- [x] Commit `fix(web): offer an edit only where it can be saved`.

### Task 5 — Close
- [x] Gates:
  - `go vet ./...`;
  - golangci-lint 2.14.0 on the touched packages: 0 issues;
  - `-race` on `internal/conversations`, `internal/agui` and `internal/runner`, unit and
    db_integration;
  - in `web/`: `npm run lint`, `npm run typecheck`, and Vitest on `src/chat` (1436/1436, Node 24).
- [x] Playwright on `aura serve`: `voice`, `composer-skills` and `chat`.
  - Two stubs no longer matched the query, and the voice mock had no seqs.
  - A play-count race in the voice spec predates the change: 1 run in 6 on master. It is now
    polled, and 8 runs in 8 passed.
- [x] Mutation on the three files. The scores and their survivors are in the verification
  file.
  After the tests in `271e27e`:
  - `conversations_compaction_api.go`: 91.4%;
  - `store_compaction.go`: 95.7%;
  - `server_run_branch.go`: 75.0% on the unit tier.
- [x] PRD §7: the two items leave "Still open".
- [x] Write `docs/superpowers/verification/2026-10-27-branch-compaction-and-edit-local.md`.
- [x] Set this plan's status.
