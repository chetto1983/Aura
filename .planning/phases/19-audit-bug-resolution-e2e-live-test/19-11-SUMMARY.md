---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 11
subsystem: validation
tags: [live-signoff, layer-2, telegram, scheduler, agui, shell, paid-agent]
requires: ["19-01", "19-02", "19-03", "19-04", "19-05", "19-06", "19-07", "19-08", "19-09", "19-10"]
provides:
  - Layer-2 live operator sign-off evidence for all user-observable findings
affects: []
tech-stack:
  added: []
  patterns: [containerized real-agent live run, CDP Telegram harness, DB/protocol ground-truth assertions]
key-files:
  created:
    - docs/audit/19-LIVE-SIGNOFF-2026-06-10.md
key-decisions:
  - "Run the real paid agent in a linux container on aura_default (clean /bin/sh shell_exec + direct docker-network DB) instead of the Windows host."
  - "H9 + H1 transport edge cases: Layer-1 (premature_close.sse / ValidateSequence) is the binding proof + a healthy-path live control; deterministic live triggers not reproducible on demand."
  - "Reminder agnostic-channel delivery gap discovered live = a NEW finding (not a Phase-19 audit item); designed + deferred to a follow-up, does not block this phase."
patterns-established:
  - "Every live repro carries a non-r.Reply ground-truth assertion (DB row / tool trace / rendered body / scheduler DB state)."
requirements-completed: [H1, H2, H3, H4, H5, H6, H7, H9, M-b, M-e]
duration: 1 day (operator-driven, paid)
completed: 2026-06-10
---

# Phase 19 Plan 11: Layer-2 Live Operator Sign-Off Summary

**Every user-observable Phase-19 finding has a recorded live before/after repro driven by the real paid agent / real operator action on the running stack, each with a ground-truth assertion that does not read `r.Reply`.**

## Accomplishments

Three blocking human-verify checkpoints, all signed off (operator: Davide):

- **Task 1 — `aura chat` host loop (H4/H5/M-b + H9).** Containerized real paid agent (linux/amd64 @ `0ab722e5`, `aura_default`, clean `/bin/sh` shell_exec). A real prompt ran a command that orphaned a `( sleep 120 & )` grandchild + 66 KB stdout + `exit 7`. **H4:** 55.8 s wall-clock, no hang (would block ~120 s unfixed); `tool_invocations`=shell_exec×4, all returned. **H5:** the agent reported exit code 7 despite self-noting truncation at ~1960 B (reserved-tail footer survived). **M-b:** the turn answered (no silence / no hand-off); persisted assistant turn. **H9:** Layer-1 `premature_close.sse` binding + healthy-path live control.
- **Task 2 — Telegram via CDP + manual upload (H2/H3/M-e).** Real bot against the fresh build. **H2:** turn error rendered `❌ Errore: llm: provider returned HTTP 400` (sanitized reason, not the bare status glyph). **H3:** a 6 MB corrupt `.docx` → `convErr` log + rendered `Conversione del documento non disponibile.` (not eternal "elaborando…"). **M-e:** `ask_user` pause (DB `paused_states` pending) → `/cancel` → DB `resumed_answer={"action":"cancel"}` + rendered `Richiesta annullata.` + keyboard cleared.
- **Task 3 — scheduler + AG-UI on the running stack (H6/H7 + H1).** **H6:** in-window agent_job → deferred `pending_notifications` row (`notify_after`=window end) → sweep delivered after window (`delivered`). **H7:** whatsapp route (no MCP) → `failed` row → bounded retry `attempts 0→1→2→3`, capped at 3. **H1:** Layer-1 `ValidateSequence` binding + live control (AG-UI up; all live Telegram turns drove the fanout conformantly).

## Evidence

`docs/audit/19-LIVE-SIGNOFF-2026-06-10.md` — BEFORE (cited from the 2026-06-10 audit) / AFTER (live observed) per finding, with DB / tool-trace / rendered-body / scheduler-DB / protocol-conformance ground truth.

## Preconditions met

- Layer-1 matrix (19-01..19-10) green; full `go build ./...` clean.
- Migration 0013 applied to the live DB (`schema_migrations` version 13, `aura.pending_notifications` present).

## Discovered follow-up (NOT a Phase-19 finding)

Reminder agnostic-channel delivery gap (reminders set via Telegram don't notify back via Telegram) — operator-flagged; execution-ready design at `.planning/spikes/reminder-agnostic-channel.md`. Deferred.

## Verification

This plan is `autonomous: false` (manual paid operator sign-off, NOT CI). Its verification IS the recorded before/after evidence in the sign-off doc. Operator sign-off recorded 2026-06-10.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*
