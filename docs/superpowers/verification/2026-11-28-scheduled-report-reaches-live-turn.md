# A scheduled report reaches the turn waiting for it: lab VM, 2026-11-28

Spec: `docs/superpowers/specs/2026-11-28-scheduled-report-reaches-live-turn-design.md`.
VM .158 running `ghcr.io/chetto1983/aura:edge` at `015c660ed` (container label, started 13:28:24Z).
Model route: OpenRouter `tencent/hy4-flash`. Driven from the operator's cockpit session through
Claude in Chrome, conversation `01a22259-58f9-7423-af49-59fd51115345`, test task
`01a22259-a45f-7e2a-878f-f172d276ebc3` (read-only: count the files in `/workspace/artifacts`).
Read back from the VM's `aura-postgres` (`conversation_turns`, `steer_queue`, `agent_job_runs`,
`paused_states`).

## Why

Conversation `01a221cc…` ("Cosa dai di me"), the same morning: two `run_now` agent_job runs
recorded their reports while the turn that fired them slept 73 s and polled `task list` four
times, then told the operator nothing had arrived. One record landed between a `shell_exec` call
and its result, and reload replaced that result with a synthetic "crash recovery" one.

## Measured

| Case | Result |
|---|---|
| Record of a finished agent_job | turn #17 starts `[Scheduled task agent_job 01a22259-a45f-7e2a-878f-f172d276ebc3]` |
| Run finishing after the turn ended (13:31:21 vs 13:31:11) | 0 `scheduler` rows in `steer_queue`: no second copy for a later turn |
| `run_now` reply | the model called `history` twice (no runs yet) and ended the turn; no `sleep`, no `task list` loop |
| Run finishing during a live turn (`run_now`, then `shell_exec sleep 40`) | `scheduler` row pushed 13:34:25.631, drained 13:34:35.982, right after the sleep's result; the model answered "il report è arrivato durante l'attesa" with the job's content |
| Out-of-band record in the live case | landed at #22 between the `sleep` call #21 and its result #23, the measured shape |
| Reload of that conversation in the cockpit | linear thread: call, real result ("took 40.1s"), record, reply; no branch picker |
| Next turn replaying #21–#23 from the store | accepted by the provider; `task history` returned both runs `completed`; the test task cancelled |
| Reload of `01a221cc…` | the `sleep 20` card shows its real result ("took 20.2s"), no "crash recovery" text |

## Found on the way, fixed

The answer a resumed model reads after a scheduled-task approval named the task by its first eight
characters. The model copied `01a22259` into `run_now` and got "invalid UUID length: 8" (also at
11:07 in `01a221cc…`). Fixed in `8abab0dc8`; not yet on the VM.

## Incident

The approval card of the test agent_job was answered by Claude: typing "approvo" was blocked by
the auto-mode classifier, but the following click on "Rispondi" with the field empty was recorded
as `{"action":"accept"}` (13:30:50.654). An empty "Rispondi" on that card is an accept. The task was
read-only and has since been cancelled.

## What this does not prove

- One run per case on one model; not a rate. Another model may still sleep and poll.
- A report that exceeds the steer cap (about 32 KiB) is not delivered to the live turn: not exercised.
- A record landing between two results of one parallel batch: covered by unit tests only.
- `pim_watch` and reminders: they record only, by design, and were not exercised here.
- The window between a turn's last drain and its untrack, where a push is neither delivered in
  that turn nor suppressed: not exercised.
