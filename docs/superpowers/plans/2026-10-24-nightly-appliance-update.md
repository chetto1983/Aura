# Nightly appliance update: implementation plan

**Status (2026-10-24):** Tasks 1–3 done on the branch. Task 4 needs the lab VM, Task 5 needs
Task 4.

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans, task by task, with
> test-driven-development.

**Goal:** a pending build restarts Aura on its own only at night (`AURA_UPDATE_AT`), not after
15 idle minutes. Everything else about updates stays as it is.

**Architecture:** the decision stays in `deploy/aura-update-consent.sh`, which records
`apply_at` in `update/status`. `internal/hostupdate` reads it, `GET /api/system/update` returns
it, and the admin's dialog shows it.

**Tech Stack:** bash with GNU `date`; Go; React with i18next.

**Spec:** `docs/superpowers/specs/2026-10-24-appliance-update-notice-design.md` (N1–N3).

## Global Constraints

- **The consent library and the updater are payload files.** `make payload-manifest` refreshes
  `scripts/payload_manifest.txt` in the same commit.
- **Only the idle rule changes** (operator, 2026-10-24: "basta che togli solo l'inattività").

## Review Focus

1. **`apply_at`.** It is the first `AURA_UPDATE_AT` strictly after the first pending build, in
   host local time across DST, and it survives newer builds.
2. **No day-time restart is left with nobody asking.** A failure moves the night on, and a
   postponement still holds until the deadline.
3. **Compatibility.** An older Aura ignores the new `apply_at` key.

## Decisions taken while writing this plan

- **The live-run check stays at night.** A restart under a running tool or job loses its work.
  "Inactivity" is the 15-minute spell without use.
- **GNU `date` steps the day with no time beside it.** It reads "03:00 +1 day" as the zone offset
  +1.

## File structure

- `deploy/aura-update-consent.sh`, `deploy/aura-image-update.sh`,
  `scripts/aura_update_consent_test.sh`, `scripts/payload_manifest.txt`;
- `internal/hostupdate/status.go`, `internal/agui/system_update_api.go` and their tests;
- `web/src/update/{systemUpdateApi.ts,UpdateDecisionDialog.tsx,updateTime.ts}`,
  `web/src/i18n/resources.update.ts`, and the tests in `web/src/update/__tests__/`;
- `docs/INSTALL.md`, `CHANGELOG.md`.

### Task 1: the updater decides at night (`7b1a23e`)

- [x] Test first: the night, its default and validation, DST, a run in flight, a stale or
  unreadable report, a postponement at night, the deadline, a failure, a request at noon.
- [x] `UPDATE_AT`, `next_apply_at`, `aura_is_quiet`, `apply_at` in `status`, a failure moving it
  on; `IDLE_SECONDS` removed.
- [x] `aura_update_consent_test.sh` and `aura_image_update_test.sh` green, `shellcheck -S
  warning` clean, the manifest refreshed.

### Task 2: the daemon and the modal (`e6df958`)

- [x] `Status.ApplyAt` and the API's `apply_at`; `go test -race` on `hostupdate` and `agui`,
  `cmd/aura`, golangci-lint clean.
- [x] The headline from `apply_at`, or from the end of a longer postponement. The tests in both
  languages; `vitest`, `tsc`, `oxlint`, prettier clean.

### Task 3: docs

- [x] `docs/INSTALL.md` "When an appliance restarts", `CHANGELOG.md` Unreleased.

### Task 4: the lab VM run

- [ ] Publish a build in the day: it is pending with tonight's `apply_at`, the admin's dialog
  says so, and nothing restarts before 03:00.
- [ ] The restart happens at the first tick after 03:00.
- [ ] Repeat with a job running at 03:00: it waits for the job.
- [ ] Record it in `docs/superpowers/verification/<date>-nightly-appliance-update.md`.

### Task 5: PRD and the operator's messages

- [ ] Amend the PRD paragraph on appliance updates from Task 4's measurement.
- [ ] N3.
