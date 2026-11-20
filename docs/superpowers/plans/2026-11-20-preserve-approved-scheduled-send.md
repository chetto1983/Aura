# Preserve an approved scheduled send during automatic continuation

> **For agentic workers:** Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` to implement this plan. Steps use checkboxes.

**Goal.** Keep a reviewed scheduled message active through the model's automatic acknowledgment and allow a later operator request to cancel it.

**Architecture.** Use the committed review answer and the latest operator input on the active conversation branch. On a continuation, the runner supplies the task tool with a check scoped to the caller and target task. Both review transports reach this runner path.

**Tech stack.** Go, PostgreSQL, sqlc, Telegram and the cockpit AG-UI API. [Approved design and baseline measurement](../specs/2026-11-20-preserve-approved-scheduled-send-design.md)

## Global constraints

Apply the check before the store can mutate the task or its draft. Preserve existing operator controls, unrelated task actions and the scheduler's send claim. Use the operator's own mailbox for live delivery acceptance.

## Review focus

1. A cockpit continuation arrives in a separate HTTP request from the approval.
2. A Telegram callback starts a turn without a new operator message.
3. A genuine operator steer during a run must take effect once persisted. A runtime-only update must preserve the approval.
4. A retry or restart must recover the approval for the correct owner, branch and task.
5. An unsuccessful approval lookup must leave the task and draft intact.

## Task 1 — Enforce the decision at the task tool

**Files.** Add the owner-scoped query in `internal/db/queries/message_drafts.sql` and regenerate its sqlc bindings. Expose the lookup in `internal/messagedrafts`. Wire it from `internal/runner` when starting a continuation. Apply it to the ID-targeted operations in `internal/agent/tools/task.go` and `task_pause.go`.

- [ ] Start with a scheduled draft approved through `ResolveMessageDraft`. Drive a fresh runner continuation over the stored answer. Make the model list tasks and attempt to cancel the reviewed one. Observe the regression before implementing the check.
- [ ] Match owner, conversation, active branch, accepted scheduled draft and requested task ID in the durable lookup. Check for later operator input when a mutation is attempted.
- [ ] Cover all four targeted actions. Assert the task remains active, its due time is unchanged and its draft remains approved. Refuse mutation on a lookup error. Exercise a second task through its ordinary mutation path.
- [ ] Exercise retry, branch isolation, a runtime-only update and genuine operator input. A fresh cancellation before the due time must leave the task cancelled and the draft declined.
- [ ] Run Go tests, race checks and tagged PostgreSQL integration tests for the affected packages. Verify the Telegram callback and cockpit review still lead to a continuation with no fresh user message.

## Task 2 — Repeat the measured delivery

- [ ] Deploy the verified revision in the authorized lab. Schedule a uniquely marked email for the authenticated operator and approve it through the review card.
- [ ] Record the review status, pending-pause count, automatic turn and status of the approved task before it fires. If the model tries a mutation, verify the persisted task and draft after that call.
- [ ] Follow the scheduler and the provider through the due time. Verify the terminal send outcome and a single message matching the exact marker. Record deployment and live results separately in the verification report.
