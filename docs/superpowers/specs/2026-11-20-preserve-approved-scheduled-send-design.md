# Preserve an approved scheduled send during automatic continuation

The operator approved this follow-up on 2026-11-20. It applies to the scheduled message review introduced in [Scheduled delivery defaults and messages, Part 2](2026-11-05-scheduled-delivery-defaults-and-messages-design.md).

## What happened

On revision `4ed06afd9`, the operator approved a scheduled email whose only recipient was the authenticated Google account owner. The review endpoint returned HTTP 200 with status `approved`, and the conversation had zero pending pauses. Its transcript then showed the review answer followed by model calls to list, pause and cancel the same task before its due time. A Gmail search for the exact test marker returned zero rows and zero warnings.

Approval therefore reached the runner. The transcript locates the cancellation in the automatic turn that followed it. Delivery of an approved message still needs its own end-to-end proof.

## Operator decision

After the operator approves a scheduled email or WhatsApp message, Aura may resume the conversation to acknowledge the decision. During that continuation, model calls to `task` must preserve the approved send and its schedule. This covers `cancel`, `pause`, `resume` and `run_now` for that task ID. The model may list tasks and use normal management actions on unrelated tasks.

The next genuine operator message restores the usual task controls. An explicit cancellation before the send claim must still decline the scheduled draft and cancel its task. Tool replies and system-generated updates during the continuation carry no new operator instruction.

## Where to enforce it

Both review surfaces use `Runner.ResolveMessageDraft`. Telegram drives the continuation with `Runner.Turn(ctx, conversationID, nil)`. The cockpit starts a separate `/agent/run` request with an empty message list. The runner can recover the accepted scheduled review from the owner's persisted conversation branch and bind the matching task IDs to the model's task-tool decisions.

Check the target before the task tool calls the store. Read the committed approval and subsequent operator input from the active branch so a retried request, a process restart or another branch cannot change the decision. If the approval state cannot be read during a continuation, return an error before attempting the targeted mutation. Existing draft dispatch remains responsible for claiming the send once at its scheduled time.

## Acceptance

- Approve through the cockpit or Telegram. Automatic attempts to change the reviewed task leave its stored schedule, active task and approved draft intact.
- An unrelated task remains manageable during the same turn. A subsequent operator request can cancel the approved send before its due time.
- Repeat the live email case with a unique marker, addressed only to the authenticated operator. Record the approval response, continuation, state up to the due time, scheduler disposition and provider-side result. A successful delivery requires a recorded send and one matching message in the authorized mailbox.
