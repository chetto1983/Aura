# Cancelling a task the conversation did not create asks the operator first

Date: 2026-11-26. Status: approved by the operator in chat (option "chiede se non è suo").

## Why
On 2026-11-25, while the operator pushed the model to schedule a notification, the model ran `task list`, then
`task cancel` on two tasks. One of them was a task the operator had scheduled from the CLI, in no conversation.
Nobody was asked (consolidation ledger, check 9 attempt). `task cancel` stops a task for good. Today it is classified
`Normal` (`internal/gateway/classify.go` `taskFixedTiers`), and the gateway stops a turn only on `Destructive`
(bf6dd4cad, so the operator stops answering "yes" by reflex). Nothing else stands between the model and the
operator's own schedule.

## Goals
- A `task cancel` of a task this conversation scheduled runs at once, as today. Example: "annulla il promemoria che
  hai appena messo".
- A `task cancel` of any other task returns an approval challenge, and the task is cancelled only on the repeat
  after the operator accepts. "Any other task" means one scheduled from another conversation, from the CLI or from
  the cockpit, i.e. its `origin_conversation_id` is empty or different.
- The approval is bound to that one task. It cancels at most once and cannot be replayed onto another id.

## Non-goals
- No change to the gateway's gating rule (only `Destructive` stops a turn) or to `pause`, `resume` and `run_now`.
- No change to cancellation from the cockpit or the CLI: those are the operator's own hands.
- No split of `task` per action. That proposal comes later and carries this rule over.

## Approaches considered
1. **Ask if the task is not the conversation's own (chosen).** It uses the in-tool challenge that `shell_exec` and
   `plugin_pack` already use (`ApprovalStore[T]`, `newChallengeResumeHook`, `ask_user kind=approval`). The gateway's
   classifier stays pure (no DB read).
2. Always ask on cancel. This was rejected: it brings back the reflex "yes" for the common case.
3. Downgrade a foreign cancel to pause. This was rejected by the operator: the task stays listed until the operator
   acts.

## Design
- The task tool's store seam exposes the target task's origin conversation; the cron adapter in `cmd/aura` already
  has it. The tool compares it with the conversation id in the call context.
- Own task: cancel as today.
- Foreign task:
  - The tool creates a challenge whose digest binds the task id and what the operator is shown.
  - It returns the trusted `*_approval_required` payload in `plugin_pack`'s shape, with the question, `repeat` and
    `resume_context`.
  - The question is Aura's own text. It names the task: id, kind, schedule or next fire, and a capped preview of the
    payload.
  - The repeat consumes the approval and cancels.
- A resume hook built with `newChallengeResumeHook` records the accept.
- A decline, an approval for a different task, or a second repeat cancels nothing.

## Known consequences

The rule follows a call's conversation, so headless runs get these consequences (review of the
build, 2026-11-26):
- An agent_job runs with its task's origin conversation as the call's conversation. A job
  scheduled from a chat therefore cancels, with no card and unattended, itself and any task that
  chat scheduled. That is narrower than before, when it could cancel anything.
- A job scheduled from the CLI or the cockpit has an empty origin, so it can no longer cancel
  itself. Its challenge is keyed on the job's technical session, and a headless ask_user is
  refused (D-25). A "repeat until Y, then cancel yourself" job scheduled that way keeps firing
  until the operator cancels it.
- A swarm worker keeps the parent's `task` tool and its origin. It cancels the parent
  conversation's own tasks at once, but it cannot get a foreign cancel approved: its challenge is
  keyed on the worker's own session, which no resume hook reaches. The parent can make the same
  cancel and get its own card.
- The card shows up to 160 runes of the payload, flattened but not quoted so the model can copy
  it byte for byte, inside a result marked trusted. A payload derived from an email can thus put
  instruction-shaped text into trusted context. It cannot forge a line of the card, and the steer
  scrub still runs.

## Left to the builder
- Field and type names.
- The question's exact wording, which must match the existing approval texts in `internal/approvaltext`.
- The digest inputs beyond the task id.
- Where the resume hook is registered, next to the plugin one.

## What done means
The unit tests cover:
- own task → cancelled with no challenge;
- foreign task, and one with empty origin → challenge, and nothing cancelled;
- approve + repeat → cancelled once;
- a second repeat → a new challenge;
- an approval for task A on task B → challenge, and nothing cancelled.

Then live, on the lab VM, through the cockpit:
- the model asked to cancel a CLI-scheduled `[aura-misura]` reminder raises an approval card;
- after the accept the task is cancelled;
- a reminder scheduled in the same chat cancels with no card.

A dated file in `docs/superpowers/verification/` records the run.
