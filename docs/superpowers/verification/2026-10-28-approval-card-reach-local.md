# A pending card keeps its buttons within reach on a phone — local verification

Date: 2026-10-28. Branch `ccr-648b75d9-4hbqnw`, on master `1ea08488`. Report: the operator's
screenshot of a plugin-install approval on a phone (360 px wide, telemetry footer open). It
showed the question and the answer field, and below them nothing: no Answer, no Decline, no
composer, and no scroll that brought them up.

## Cause

The pending cards (`ThreadApprovalCards`) sit between the thread and the composer, outside the
thread's scroller, inside the chat root, which is `overflow-hidden`. A card taller than the room
left pushed its own footer and the composer below the root's clip. A finger cannot scroll an
`overflow-hidden` box. Script can: `click()` and `focus()` scroll every ancestor, which is why
the existing specs clicked Answer on the phone profile and passed. The committed mobile baseline
`calm-prism-mobile-dark.png` shows the same defect: the second card is cut at its buttons, and the
third card and the composer are missing.

## Fix

The region holding the cards is `min-h-0 overflow-y-auto overscroll-contain`. It shrinks to the
room left above the composer and scrolls on its own.

## Measured

`e2e/approval-card-reach.spec.ts` puts the report's card on a 360×800 viewport with the
telemetry footer open. It scrolls only with the wheel over the card, and acts at the pixel: a
target counts as reachable when its centre is in the viewport and `elementFromPoint` there hits it.
- Before the fix, against `aura serve` built from `1ea08488` (`chrome` and `mobile-chrome`): the
  locked composer had a viewport ratio of 0. Twelve wheel steps never brought Answer within reach.
  The failure screenshot matches the report.
- After the fix, both projects pass. Answer, Decline, Cancel run and the field are reachable; the
  composer is fully in view. Typing `ok` and tapping Answer sends
  `{"action":"accept","content":"ok"}` and the card reads "Answered.".
- The plugin approval resolves on the action, not the words (`newChallengeResumeHook`,
  `cmd/aura/serve_adapters.go`): `ok` with Answer approves.

No regressions, on the same serve with `chrome` and `mobile-chrome`:
- `chat.spec.ts`, `chat-calm-prism.spec.ts`, `shell.spec.ts` and `shell-overscroll.spec.ts`: 33
  passed, 13 skipped by their own project guards.
- The four calm-prism screenshot comparisons differ. On mobile, the region now scrolls and the
  composer shows. On desktop at 1440×1000, the composer's bottom row, clipped before, is now whole;
  the rest is font antialiasing. CI regenerates these baselines on the runner
  (`--update-snapshots`, the TEMP harvest step in `ci.yml`), and the committed ones already predate
  the Board tab. They are not regenerated here: an off-runner render would not match the runner's.

Web gates on Node 24.22.1: `lint`, `lint:contract`, `mutation:contract`, `format:check`,
`typecheck`, `dup`, `deadcode`, and Vitest (3381 tests in 396 files) are clean. The dist was rebuilt
and the change is embedded. `go test -race ./internal/webui/` passes, and so does `make file-size`.

## What this does not show

- A real phone. Chromium's mobile emulation and the wheel stand in for a touch scroll.
- iOS Safari: the `mobile-safari` project needs the HTTPS proxy and was not run.
- The keyboard: on a real phone the on-screen keyboard shrinks the viewport while the field is
  focused. That was not exercised.
