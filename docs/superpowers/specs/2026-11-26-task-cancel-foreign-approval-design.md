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
