# Craft preview cancellation — review F1

Date: 2026-10-22, Europe/Rome. Fix round1 for the Task2 review of `5f88ffa43`.

## Reproduction

The new live regression starts an actual Python child inside a disposable offline Craft
box. The child installs `SIG_IGN` for SIGTERM and writes its PID before cancellation.
On the reviewed code, RunPreview returned cancellation while PID19 remained sleeping,
with PPid1. The test failed after2.06s with the child's live `/proc/19/stat` record.
The test subsequently killed the child and destroyed its disposable box.

A transport unit test independently reproduced Wait returning code0 after a single
`ExecInspect` response containing `Running:true`; three inspections and final code17
were expected. The review finding was therefore confirmed in both the live process and
the shared execution boundary.

## Shared correction

The existing shared PID-file mechanism now snapshots descendants before signaling them,
records process start times to avoid escalating against reused PIDs, sends TERM, and
escalates surviving descendants to KILL after a short grace period. The wrapper catches
TERM, waits for its foreground child and exits, allowing that child to be reaped instead
of continuing a compound command. The wrapper is killed last if it remains resistant.
The termination command and the original Docker exec must both be confirmed exited.

Kill remains nonblocking and once-guarded. One bounded shared termination worker runs
outside caller registry locks; Wait joins it even when output had already closed.
Running executions are polled instead of treating a provisional exit code as completion.
Inspection/termination have bounded contexts and termination-stream closure follows the
deadline. The same mechanism handles synchronous execution cancellation and ambiguous
attach failures after an exec id has been created. There is no preview-only supervisor.

## Unconfirmed exit and availability

The shared `ErrExecUnconfirmed` marker survives canceled contexts and transport wrapping.
An unconfirmed preview closes its original stream but retains its scratch directory and
admission slot. Queued and future previews fail promptly as unavailable503. No background
retry loop or automatic reset starts additional work whose predecessor may still be alive.

This deliberately sacrifices preview availability when remote exit cannot be proven.
Recovery requires confirmed process-tree or box cleanup before resetting the service;
retained scratch must only be removed after that confirmation. A service reset alone
does not establish that the old remote work has stopped.

## Verification

The resistant-child live test is now GREEN: the child's PID is absent before RunPreview
returns (initial fixed run0.58s). The test is included in the existing unconditional native
CI matrix through the TestCraftPreviewNative prefix.

Focused tests also establish bounded Running inspection, nonblocking/idempotent Kill,
ambiguous-start cleanup, typed-cause preservation, original-stream closure, retained scratch
and admission, prompt rejection of queued/future work, and unavailable precedence over
cancellation. Changed-surface vet/build and all five package race suites passed:
usersandbox1.228s, documents4.676s, agui37.455s, cmd/aura64.508s and agent/tools12.764s.
Live shared execution regressions passed6.367s; the offline native matrix passed6.408s,
including the resistant child's PID absence before return in0.75s.

Fresh combined document coverage is **1021/1165 statements =87.639485%**, using860 unique
blocks across5160 repeated rows from six passing disposable-Postgres consumer suites.
Native `go tool cover` confirms87.6%. No prior denominator, percentage averaging, policy
waiver or baseline change was used. Full official release covdata gating remains Task5.

Five valid isolated overlay mutants were killed (100%): Running treated as exit, masking
unknown exit with cancellation, releasing uncertain admission, deleting uncertain scratch
and blocking Kill. Normal commit-hook details and the exact fix range are in the task report.

## Review round2: completion and cancellation ordering

Base: `acd29b3a5`, measured 2026-10-22, Europe/Rome. The background-shell reaper defers
Kill until after Wait returns. The original round1 handle admitted a termination worker
even after successful completion. A second ordering let Kill start during the last successful
Docker inspection, then allowed Wait to return code17/nil while that worker was still blocked.
Both orderings were reproduced by daemon-free transport tests before this correction.

The handle now coordinates confirmed completion and termination admission under one short
mutex. A Kill after confirmed completion does nothing. A Kill admitted before completion
owns the result: Wait joins its bounded worker and preserves its exit code or typed uncertainty,
even if the concurrent inspection reports successful exit. Multiple Wait calls share the
same settled result. Docker I/O and joins happen outside that mutex, preserving nonblocking,
idempotent Kill and existing caller registry lock ordering. No preview lifecycle policy changed.

Round2 verification: targeted vet/build and all three unit suites passed (usersandbox0.160s,
agent/tools3.776s, cmd/aura29.545s). Serial race suites passed in1.284s,12.442s and64.742s
respectively. Live shared execution/stdin/EOF/kill race regressions passed2.613s. The offline
native race matrix passed6.307s across nine committed fixtures, normal cancellation0.40s and
resistant-child cancellation0.60s, confirming PID absence before return. Both valid lifecycle
overlay mutants were killed: removing the completed guard and bypassing a concurrent
termination worker. Document source and its coverage denominator are unchanged by round2;
no new whole-repository discovery or coverage-policy exception was used. Exact commands,
logs, commit and scoped re-review range are in the task report.
