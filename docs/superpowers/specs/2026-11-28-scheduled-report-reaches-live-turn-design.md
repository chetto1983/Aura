# A scheduled run's report reaches the turn that is waiting for it

## Why

Measured on the lab VM on 2026-11-28, conversation `01a221cc-65f6-722a-9960-56fc84e01f53`
("Cosa dai di me"). The operator asked Aura to schedule a weekly cleanup report and fire it
once with `run_now`. Both runs completed (`aura.agent_job_runs`, tasks `01a221d6…` and
`01a221d9…`, status `completed`, summary "Nessuna anomalia rilevata…"), and
`Dispatch.recordToOrigin` appended both reports to the conversation (turns #105 and #130).
Aura saw neither. It slept 20 + 25 + 28 s, read `task list` four times, told the operator
that "nessun report è comparso in questa conversazione", and cancelled and recreated a task
that was already working.

Three causes, read from the code:

1. `recordToOrigin` only appends to `aura.conversation_turns`. A live turn never re-reads the
   store, so a record written during the turn is invisible until the next operator message.
   Delegations solve the same problem with a second copy on the steer queue
   (`DelegationDelivery.Deliver`), which `LlmAgent.drainSteer` delivers after every tool
   round. The cron has no such copy.
2. The record is plain assistant text. In the next turn (#142) the model had #105 and #130 in
   its history and still did not recognise them as the job's report: they read as its own
   words. Delegations prefix their record (`attributedWorkerReport`) for exactly this reason
   (T-51-38). The cron does not.
3. Turn #130 landed between the tool call #129 and its result #131. On reload,
   `repairToolPairs` gives #129 a synthetic "recovered" result and drops the real #131 as an
   orphan. Harmless for a `sleep`, but a mutating call would read as "outcome indeterminate".
   Delegation records can land the same way.

Aura also had no way to read what a run produced: `task` has no history action, although
`cron.Store.ListRunsForTask` already serves the cockpit's run board.

## Goals

- A scheduled run that records into its origin conversation also pushes the same text on the
  steer queue, so a turn running in that conversation sees it at its next tool round.
- The record in `aura.conversation_turns` names its author: a scheduled task, its kind and id.
- On reload, a message that landed between an assistant tool call and its results is moved
  after those results, so the real results survive. This covers cron and delegation records.
- `task action=history task_id=…` returns the task's recent runs: start time, status, summary,
  error. Owner-scoped like `cancel` and `run_now`.
- `run_now`'s result tells the model where the report will appear and that it should not
  sleep and poll for it.

## Non-goals

- No wake of an idle conversation. `backgroundCompletionDispatcher` would start an LLM turn
  for every recorded run, `pim_watch` included (every few minutes), and it waits on the thread
  lock, so it would not reach a live turn anyway. The record already reaches the next turn.
- No multiple notify routes per task. The operator's second request from that conversation is
  left for a later spec.
- No change to what Telegram, WhatsApp or e-mail receive.

## Approaches

For the turn that lands between a call and its result (cause 3):

- **A — repair on reload (chosen by the operator).** `repairToolPairs` moves the intruding
  messages after the call's results. Local to one function, and it fixes every writer at once.
- B — the cron holds its record while a turn is live. Rejected: the cron would have to know
  whether the conversation is running, which is a new coupling, and delegations would keep
  the bug.

For reaching the live turn (cause 1), my call: a plain steer push, the delegation pattern, not
the background-completion wake (see Non-goals). Steer rows expire after
`AURA_STEER_QUEUE_TTL_SEC` (900 s), so a conversation nobody is using does not fill its
32-row cap.

## Design

1. `steer.SourceScheduler = "scheduler"`, accepted by `steer.IsRuntimeSource` and given an
   envelope name in `runtimeSteerEnvelopes`. The text is generated content, so it takes the
   untrusted tool-output envelope like the other runtime sources.
2. `cron.Dispatch` gets an optional steer publisher (a consumer-declared one-method
   interface). `recordToOrigin` pushes the same text after the append. A push failure is a
   WARN, like a record failure: the work is done and is on the ledger. Wiring is in
   `cmd/aura/serve_dispatch.go`, from the steer store the agent already drains.
3. The record text is prefixed with its author, for example
   `[Scheduled task report — agent_job 01a221d9]`. Only the stored record: the steer copy is
   attributed by its envelope, and the notification text is unchanged.
4. `repairToolPairs`: when an assistant message with tool calls is followed by one or more
   messages that are not tool results, and the results for those call ids follow them, emit the
   call, its results, then the intruders. The rewrite applies only when every result is found.
   Otherwise today's recovery path runs.
5. `task action=history`: `task_id` (required), `limit` (default 5, max 20). Summary bounded
   per run. Ownership checked the way `run_now` checks it.
6. `run_now` result text: the report is recorded in this conversation and reaches the running
   turn after its next tool call; `task action=history` reads it; do not sleep to wait for it.

## What this does not prove

- That the model, handed the steered report, uses it instead of polling. That is measured on
  the lab VM: `run_now` of an `agent_job`, then one ordinary tool call, then the reply.
- `pim_watch` records reaching a live turn: the same path, not measured separately.
- The reload repair is exercised by unit tests on fixtures shaped like turns #129–#131, and on
  the VM by reloading conversation `01a221cc…`.

## Left to the builder

- The exact prefix wording, the history page format and the summary bound.
- Whether `record_origin_test.go` assertions change with the prefix (they will; justify in
  the commit).
- Regenerating `internal/agent/tools/testdata/deferred_manifest.json` for the new action.
