---
phase: 51
slug: durable-delegation
status: validated
nyquist_compliant: false
wave_0_complete: true
created: 2026-08-27
validated: 2026-08-30
---

# Phase 51 - Validation Strategy

Phase 51 has a current live acceptance and a green executable verification matrix. The
validation remains `nyquist_compliant: false` because crash-after-partial-side-effects was not
exercised live. This plan does not mark the phase complete; the orchestrator/verifier owns that
transition.

## Evidence Reconciliation

- PRD Amendment #183 and `live-check/cockpit/RESULTS.md` are the accepted fresh-image Phase 51
  live result: six verdicts passed at 9.9/10 on local llama.cpp, with no OpenRouter request.
- The approved checkpoint is already satisfied. No new human checkpoint or live drive is needed.
- The planned monolithic `drive-sc.sh` was not created. The measured delivery-envelope drive that
  produced Amendment #183 supersedes that speculative artifact; no missing script is fabricated.
- PRD Amendment #177 retired and deleted `scripts/quality_snapshot_gate.sh`. The historical
  quality snapshot is not rewritten and the deleted command is not reported as passing.
- PRD Amendment #187 now has a current four-run authenticated Cockpit E2E for Ollama reasoning,
  context discovery, final-answer visibility, route restore, and process stability.
- No closure claim is made for items from the deleted legacy eval harness that were not run.

## Current Verification

| Surface | Result |
|---|---|
| Go unit suite | PASS - WSL `go test -count=1 ./...` |
| Go static/build | PASS - WSL `go vet ./...` and `go build ./cmd/aura`; one transient NTFS/WSL embed-visibility error was investigated and the immediate rerun passed |
| Go race | PASS - `./internal/llm/... ./internal/agent/prompt ./internal/agui ./internal/settings ./cmd/aura` |
| Web lint/typecheck | PASS - `npm run lint`, `npm run typecheck` |
| Web tests | PASS - 226 files / 1905 tests; 91.23% statements, 85.13% branches, 90.47% functions, 93.15% lines |
| Web production build | PASS - two builds from `npm ci`, 4,331 modules each, zero regenerated diff |
| Packaging | PASS - fat-image contract test |
| Patch hygiene | PASS - `git diff --check` |

## Per-Task Verification Map

Every planned task ID has a final disposition. `PASS (superseded)` means the originally named
artifact or gate was retired by measured PRD evidence, not silently skipped.

| Task ID | Requirement | Result | Evidence |
|---|---|---|---|
| 51-01-T1 | SWARM-03, SWARM-09 | PASS | `51-01-SUMMARY.md`; queue/claim tests and live tracer |
| 51-01-T2 | SWARM-03 | PASS | `51-01-SUMMARY.md`; daemon claim loop uses shipped `runChild` |
| 51-01-T3 | SWARM-03 | PASS | `51-01-SUMMARY.md`; early-return live tracer |
| 51-04-T2 | SWARM-07 | PASS | `51-04-SUMMARY.md`; host-derived provenance and forget/read-back tests |
| 51-04-T3 | SWARM-07 | PASS | `51-04-SUMMARY.md`; 70+ live race stress runs against ArcadeDB |
| 51-02-T2 | SWARM-03, SWARM-09 | PASS | `51-02-SUMMARY.md`; durable steer queue integration evidence |
| 51-02-T3 | SWARM-09 | PASS | `51-02-SUMMARY.md`; atomic expiry-plus-trace evidence |
| 51-03-F1 | SWARM-01 | PASS | `51-03-SUMMARY.md`; brief context separation tests |
| 51-03-F2 | SWARM-02 | PASS | `51-03-SUMMARY.md`; live-cap schema tests |
| 51-05-T1 | SWARM-04, SWARM-05 | PASS | `51-05-SUMMARY.md`; depth and nested synchrony race tests |
| 51-05-T2 | SWARM-08 | PASS | `51-05-SUMMARY.md`; delegated-dispatch fingerprint guard |
| 51-06a-T2 | SWARM-06 | PASS | `51-06a-SUMMARY.md`; fenced resume and RLS integration tests |
| 51-10-T1 | SWARM-03 | PASS | `51-09-SUMMARY.md` and `51-10-SUMMARY.md`; live RLS record correction |
| 51-10-T2 | SWARM-03, SWARM-09 | PASS | `51-10-SUMMARY.md`; nudge tri-state and concurrency tests |
| 51-06b-T1 | SWARM-06 | PASS | `51-06b-SUMMARY.md`; atomic pause-and-park integration test |
| 51-06b-T2 | SWARM-06 | PASS | `51-06b-SUMMARY.md`; resume, no replay, promoted-tool and sibling isolation tests |
| 51-06b-T3 | SWARM-06 | PASS | `51-06b-SUMMARY.md`; expiry trace and queue resolution tests |
| 51-07-T1 | SWARM-10 | PASS | `51-07-SUMMARY.md`; identity-scoped path-hardened transcript route |
| 51-07-T2 | SWARM-11 | PASS | `51-07-SUMMARY.md`; amendment-before-implementation git evidence |
| 51-09-T1 | SWARM-03, SWARM-04, SWARM-09 | PASS | `live-check/d03/RESULTS.md`; inactivity model and reaping |
| 51-09-T2 | SWARM-03 | PASS | `51-09-SUMMARY.md`; retired wall-clock knob removed |
| 51-09-T3 | SWARM-03 | PASS | `live-check/d03/RESULTS.md`; long-run, stall, boot gate and post-fix rerun |
| 51-08-T1 | SWARM-01..10 | PASS (superseded) | Amendment #183 replaces the unbuilt monolithic driver with accepted measured evidence |
| 51-08-T2 | SWARM-01..10 | PASS | `live-check/cockpit/RESULTS.md`; 6/6 at 9.9/10, checkpoint approved |
| 51-11-T1 | SWARM-12 | PASS | `51-11-SUMMARY.md`; card and report artifact tests |
| 51-11-T2 | SWARM-12, SWARM-10 | PASS | `51-11-SUMMARY.md`; stable child IDs and terminal markers |
| 51-11-T3 | SWARM-12, SWARM-09 | PASS | `live-check/envelope/RESULTS.md`; grouped fan-out delivery |
| 51-11-T4 | SWARM-10 | PASS | `51-11-SUMMARY.md`; scoped fact-based `swarm_status` |
| 51-11-T5 | SWARM-12, SWARM-10 | PASS | `live-check/envelope/RESULTS.md`; operator-approved 5/5 envelope |
| 51-12a-T1 | SWARM-12, SWARM-10 | PASS | `51-12a-SUMMARY.md`; transcript SSE tests and race run |
| 51-12a-T2 | SWARM-12 | PASS | `51-12a-SUMMARY.md`; metadata stream and preview normalization |
| 51-12b-T1 | SWARM-12 | PASS | `51-12b-SUMMARY.md`; read-only worker pane and picker tests |
| 51-12b-T2 | SWARM-12 | PASS | `51-12b-SUMMARY.md`; one push status stream, no polling |
| 51-12b-T3 | SWARM-12, SWARM-10 | PASS | `live-check/cockpit/RESULTS.md`; live pane and delivery envelope |
| 51-12b-T4 | SWARM-11, SWARM-12 | PASS (corrected) | Amendment #183 recorded after the drive; Amendment #177 retired the prose freshness gate |
| 51-08-T3 | SWARM-01..10 | PASS (corrected) | Current executable matrix above is green; Amendment #177 forbids the deleted snapshot gate |

## Live Acceptance

### Phase 51 delivery envelope

`live-check/cockpit/RESULTS.md` records six passing verdicts at 9.9/10 on fresh healthy image
`sha256:699ce1260f16f7974f6d9885121ad8b109f22e5f867065a76d626c54f9ee95ca`, routed to local
llama.cpp `gemma-4-12b`. The drive covered durable cards, owned Markdown report assets, one
terminal notification per fan-out, live worker threads, fact-based progress, and long-result
layout. No OpenRouter request participated. Telegram evidence is cited only in its existing
redacted structural form; no content, session material or identifiers are repeated here.

### Amendment #187 current E2E

Four authenticated Playwright runs passed on the installed Ollama image
`sha256:ed60b940c495248e2747b7d57adfb4d8b23c7b30dbdd1a731599dff2d92e399b`: one normal run and
`--repeat-each=3`. Every run changed the active route between Ollama and llama.cpp from the
Cockpit, observed `/api/me` `context_window=262144`, asserted the exact Ollama reasoning-level
set `{auto, off, low, mid, high}`, sent a real `/agent/run` with `aura.effort=high`, observed non-empty streamed reasoning and
the visible final sentinel, then restored the prior route. No OpenRouter request participated.

The Aura container was byte-for-byte stable before and after: PID `19645`, StartedAt
`2026-08-30T10:43:20.631079759Z`, RestartCount `0`, the same image, and healthy.

That equality is scoped to the four-run evidence window only. The operator reports that the
shared container was recreated afterward at `2026-08-30T11:05:53Z` on image prefix
`ff68727c...`; the earlier tuple is therefore not a current final no-restart baseline. Plan 51-08
performed no Docker or live-test action after that recreation and makes no claim about the new
runtime state.

## Residual Risks And Non-Claims

| Item | Verdict |
|---|---|
| Crash after a worker performs partial side effects but before the ledger write | **OPEN residual risk.** Not exercised live; no exactly-once claim is made. |
| Deleted legacy eval harness items | Not claimed as covered. No mail/WhatsApp read-back, timing-ratio, judge-score, or no-over-spawn closure is inferred. |
| `awaiting_input` fan-out notification timing | Not covered by Amendment #183; a parked sibling can delay the terminal fan-out notification. |
| Worker-pane daemon-restart recovery and mid-run stream reconnect | Not proven by the accepted live drive. |
| Multiple eligible channel deliverers | Not proven; the accepted evidence covers the shipped single-owner route. |
| Ollama limits | The current E2E proves the installed profile and 262,144 discovery, not generation near that limit or every Ollama model. |

## Validation Sign-Off

- [x] Every task ID has an evidence-backed disposition.
- [x] The approved human checkpoint is represented by the accepted live evidence.
- [x] Current Go, race, web, build, packaging, and patch-hygiene checks are green.
- [x] No retired quality-snapshot command was run or recreated.
- [x] No OpenRouter request participated in the accepted current live evidence.
- [x] No Telegram content, session data, identifiers, screenshot, trace, or video was added.
- [ ] Crash-after-partial-side-effects exercised live.

**Approval:** accepted live checkpoint; phase completion remains with the orchestrator/verifier.
