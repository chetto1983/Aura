# `/compact` on the branch on screen, and no refused edit — Implementation Plan

**Status (2026-10-27):** in progress.

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
- **The on-screen leaf is the last message with a known seq.** A message whose run did not
  report one was written by this tab, on this branch.

## File structure

| File | Change |
|---|---|
| `internal/conversations/compaction_request.go` | `Compact(ctx, id, leafSeq, cfg)`. |
| `internal/conversations/store_compaction.go` | `branchOfTurn`, `LoadPathCompaction`. |
| `internal/runner/interfaces.go`, `runner_compact.go` | The leaf goes through. |
| `internal/agui/server_run_branch.go` | `requireTurnSeq`, shared with the compaction routes. |
| `internal/agui/conversations_compaction_api.go` | `?leaf_seq` on both routes. |
| `web/src/chat/compaction/api.ts`, `useCompaction.ts`, `useThreadCompaction.ts` | The leaf in the POST, the read and the key. |
| `web/src/chat/messageSeq.ts` | `backendSeqOf`, `branchLeafSeq`, `showsSeq`. |
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
- [ ] Red, Vitest: the POST and the read name the on-screen leaf after Previous, and the
  marker stays while a run grows the branch.
- [ ] Green: `api.ts`, `useCompaction.ts`, `useThreadCompaction.ts`, `messageSeq.ts`.
- [ ] Commit `feat(web): /compact condenses the branch on screen`.

### Task 4 — No refused edit (D15, acceptance 14)
- [ ] Red, Vitest:
  - a user message with no seq has no Edit;
  - an assistant message with no seq has no Regenerate;
  - a server refusal leaves the edited text on screen.
- [ ] Green: `ExternalStoreChat_messages.tsx`.
- [ ] Commit `fix(web): offer an edit only where it can be saved`.

### Task 5 — Close
- [ ] Gates: `go vet`, build and `-race` on the touched packages, plus db_integration on
  `internal/conversations`. In `web/`: `npm run lint`, `npm run typecheck` and `npm test`.
- [ ] PRD §7: the two items leave "Still open". The verification file records what this closes.
- [ ] Set this plan's status.
