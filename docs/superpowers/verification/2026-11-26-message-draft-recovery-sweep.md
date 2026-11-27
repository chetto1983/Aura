# Message draft recovery sweep: the live check (2026-11-26)

Plan: `docs/superpowers/plans/2026-09-28-tool-rendering-message-drafts.md`, "Task 4, revised", step R4.
The defect it closes is in `2026-11-25-pim-tool-split-vm-checks.md`, "Stale pending drafts": on the lab
VM a review stayed `pending` 46 days past its `expires_at`, because expiry was applied only when the
draft was read.

## Tree and stack

| | |
|---|---|
| Tree | branch `ccr-d742c0b3-jvadt4-draft-recovery`, on master `1e1c1cfe1`, with R1–R3 uncommitted (the binary reports `…1e1c1cfe187e+dirty`) |
| Stack | the core stack of a cloud container: `aura-postgres`, `aura-arcadedb`, `aura-llama-embed` |
| Database | disposable `aura_e2e`, owned by `aura_migrate`, 134 migrations applied |
| Daemon | `aura serve`, profile `single_user_hardened`, env of `.claude/skills/aura-model-in-the-loop/env.e2e.sh` |
| Method | the boot of `aura-model-in-the-loop` (steps 1–7). No model turn was driven: the sweep runs without one |

## Method

1. The daemon was booted once, and the operator bootstrapped (`201`).
2. The daemon was stopped (SIGTERM). Four drafts of the operator were inserted by SQL, as the superuser.
   The arguments were placeholders.
3. The daemon was started again. Its boot runs the sweep once (`SweepNow`), then every minute.
4. With the daemon running, a fifth draft was inserted, with an expiry 15 seconds away. Nothing read it.
5. The daemon was stopped (SIGTERM).

## What was measured

### The boot sweep

| Draft | Seeded as | After the boot sweep |
|---|---|---|
| `call-stale` | `pending`, `expires_at` 46 days ago | `expired`, `resolved_at` set |
| `call-interrupted` | `dispatching`, dispatch begun 31 minutes ago | `uncertain`, `interrupted`, `resolved_at` set |
| `call-fresh` | `pending`, `expires_at` in 30 minutes | `pending`, unchanged |
| `call-recent` | `dispatching`, dispatch begun 5 minutes ago | `dispatching`, unchanged |

The log line, at 21:15:13 UTC: `aura serve: closed abandoned message drafts`, `expired=1`,
`interrupted=1`.

### The minute tick

| | UTC |
|---|---|
| `call-tick` inserted, expiry +15 s | 21:15:24 |
| Its expiry | 21:15:39 |
| Closed `expired` by the tick | 21:16:13 |

The tick came one minute after the boot sweep, as `messageDraftRecoveryInterval` sets. Its log line:
`expired=1`, `interrupted=0`.

### Content and shutdown

| Check | Result |
|---|---|
| Lines in the daemon log that hold the seeded message text | 0. The sweep logs counts only |
| SIGTERM to process exit | 5 s, ending `graceful shutdown complete` |

The run also logged `observability shutdown … context deadline exceeded` at exit. It appears on the
first boot too, which ran before any draft existed. It comes from the telemetry exporter, which has no
collector in this container, and not from the sweep.

## What this does not show

- **No real model and no real send.** The drafts were inserted by SQL, not written by an agent turn
  through the send interception, and no PIM or WhatsApp bridge was mounted. The send that a restart
  cuts short is covered by the db_integration test
  `TestASendCutShortByARestartResumesAsUncertainWithoutASecondSend` (`internal/runner`), not here.
- **Not the lab VM.** The 46-day draft of the VM was reproduced by its age, not read from the VM. The
  VM is cleaned by the first boot of a release that carries this sweep.
- **The cockpit.** No review card was open while the sweep ran. A card already on screen keeps showing
  a swept draft until the next load; this run does not measure that.
- **Scheduled drafts.** None were seeded here. That the sweep leaves them to their task is shown by
  `TestRecoverClosesTheDraftsNobodyReopens` (`internal/messagedrafts`), not by this run.
- **Several identities.** Only the operator owned drafts. The per-identity loop is covered by the unit
  tests in `cmd/aura/message_draft_recovery_test.go`.
