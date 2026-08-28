---
phase: 51-durable-delegation
plan: 07
subsystem: swarm
tags: [swarm, transcript, agui, http, observability, golang]

# Dependency graph
requires:
  - phase: 51-01
    provides: "the aura.ingestion_jobs-backed delegation claim loop and dumpTranscript's shipped append-only writer (internal/swarm/report.go)"
  - phase: 51-05
    provides: "the authenticated AG-UI route shape (auth extraction, identity-scoped conv lookup, error responses) this plan mirrors"
provides:
  - "swarm.ReadTranscript(ctx, runDir, conv, childID, fromOffset) and swarm.ListChildTranscripts(ctx, runDir, conv) — a byte-offset, non-blocking, path-hardened read surface over dumpTranscript's existing JSONL output"
  - "GET /api/conversations/{conv}/swarm/{childID}/transcript — an identity-scoped, 404-hiding, application/x-ndjson HTTP route, wired into the daemon's agui server"
  - "Verified-from-git closure of SWARM-11 (process requirement), plus SWARM-08 and D-09 recorded as satisfied without new production code"
affects: ["51-08 (SC#1 live drive can now watch a worker mid-run through this route instead of blind waiting)"]

# Actuals (#2632)
actuals:
  tokens: 8150
  tasks: 2
  commits: 5

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Shared path-hardening helpers (transcriptDir/transcriptPath) used by BOTH the writer (dumpTranscript, report.go) and the new reader (transcript_api.go), so write and read sides agree on one traversal-rejection rule instead of two"
    - "Byte-offset reads returning complete lines only, reporting the offset of the last complete line — makes a concurrent-writer read deterministic without locking or waiting for EOF"
    - "agui unexported swarmTranscriptReader seam + cmd/aura swarmTranscriptAdapter closing the package-level swarm.ReadTranscript function over chat.cfg.RunDir — the same tools-package interface seam idiom as swarmRunner (internal/agent/tools/swarm_spawn.go), applied to agui instead of the tools package"

key-files:
  created:
    - internal/swarm/transcript_api.go
    - internal/swarm/transcript_api_test.go
    - internal/agui/server_swarm_transcript.go
    - internal/agui/server_swarm_transcript_test.go
  modified:
    - internal/swarm/report.go
    - internal/agui/server.go
    - cmd/aura/serve_agui.go

key-decisions:
  - "The route registers from internal/agui/server_swarm_transcript.go (a sibling file, registerSwarmTranscriptRoutes called from server.go's Mux()), never by growing cmd/aura/serve.go. The plan's files_modified listed cmd/aura/serve.go, but the daemon-side wiring landed in cmd/aura/serve_agui.go's existing wireAGUIServer — the file that already composes every other agui seam (steer inbox, operation registry, asset service). Same concern, correct existing location; not a gap."
  - "SetSwarmTranscripts is unconditionally wired (no feature-flag/nil-by-default posture): AURA_RUN_DIR is required boot config, not an optional sidecar like the graph or voice providers, so there is no legitimate 'disabled' state for this route the way there is for those."
  - "transcriptPath's hardening rejects '..', '/', '\\\\', and NUL in both conv and childID before any os.Open, and dumpTranscript was changed to call the SAME helper rather than duplicate the check — report_test.go stayed byte-identical (git diff --stat shows zero lines changed) because the writer's observable behavior did not change, only its internal path-building call site did."

requirements-completed: [SWARM-10, SWARM-11]

coverage:
  - id: T1
    description: "A live worker's transcript is readable mid-run through an authenticated, identity-scoped route: ListChildTranscripts/ReadTranscript never block, offsets are monotonic across sequential reads, and a hostile childID/conv (.., ../.., a/b, a\\b, NUL) is rejected before any filesystem call"
    requirement: SWARM-10
    verification:
      - kind: unit
        ref: "internal/swarm/transcript_api_test.go (mid-run partial read, offset monotonicity, empty/missing-directory discovery, not-yet-written child, hostile-childID/conv table x2)"
        status: pass
      - kind: unit
        ref: "internal/agui/server_swarm_transcript_test.go (foreign-identity 404, malformed-conv 404, nil-reader 404, happy-path ndjson, reader-error opaque 404)"
        status: pass
    human_judgment: false
  - id: T2
    description: "SWARM-11 (process requirement) verified from git rather than rebuilt; SWARM-08 and D-09 recorded as satisfied without new production code"
    requirement: SWARM-11
    verification:
      - kind: manual
        ref: "git log/merge-base evidence in this SUMMARY's 'Task 2' section"
        status: pass
    human_judgment: false

# Metrics
duration: single session (continuation from a rate-limit-interrupted prior session)
completed: 2026-08-28
status: complete
---

# Phase 51: Durable delegation — Plan 07 Summary

**A backgrounded worker's transcript is now readable mid-run through an authenticated, identity-scoped, 404-hiding HTTP route reading dumpTranscript's existing JSONL output — and SWARM-11's process requirement is closed by git evidence, not by rebuilding what the design-gate spike already proved.**

## Performance

- **Duration:** single continuation session (prior session was terminated by an HTTP 429 provider rate limit mid-Task-1; its four commits and one uncommitted diff were verified by the orchestrator before this session resumed)
- **Tasks:** 2
- **Files modified:** 7 (668 insertions, 10 deletions across the plan's 5 commits)

## Accomplishments

- **Task 1 — identity-scoped transcript read surface (SWARM-10).** `internal/swarm/transcript_api.go` adds `ReadTranscript(ctx, runDir, conv, childID, fromOffset)` and `ListChildTranscripts(ctx, runDir, conv)` over `dumpTranscript`'s existing append-only JSONL output (`internal/swarm/report.go`). Reads are byte-offset based and return complete lines only, reporting the offset of the last complete line — a concurrent writer never causes a duplicate or a torn line, and a read never blocks waiting for the worker to finish. `transcriptDir`/`transcriptPath` reject `..`, either path separator, or a NUL byte in `conv`/`childID` before any `os.Open` call, and `dumpTranscript` was changed to share these same helpers so the write and read sides enforce one path rule instead of two independently-maintained ones — `report_test.go` is byte-identical (`git diff --stat` shows zero lines changed). The route is exposed at `GET /api/conversations/{conv}/swarm/{childID}/transcript?offset=N` (`internal/agui/server_swarm_transcript.go`), resolving the conversation through `s.conv.GetForIdentity` BEFORE touching the filesystem, mirroring `handleGetConversation`'s ladder exactly: a foreign/absent conversation, a nil (unwired) reader, an empty `childID`, and any `ReadTranscript` error (including a rejected hostile `childID`) all render the SAME opaque 404, so the wire never discloses which occurred. The happy path serves `application/x-ndjson` with `X-Content-Type-Options: nosniff` and echoes the new resume offset in `X-Transcript-Offset`. The daemon wiring (`cmd/aura/serve_agui.go`) closes `swarm.ReadTranscript` over `chat.cfg.RunDir` in a `swarmTranscriptAdapter`, satisfying `agui`'s unexported `swarmTranscriptReader` seam without `agui` importing `internal/swarm`'s concrete types — unconditionally wired in `wireAGUIServer`, since `RunDir` is required boot config with no legitimate disabled posture.
- **Task 2 — SWARM-11 verified from git, not rebuilt.** No production code was written for this task. See the Task 2 section below for the full git evidence trail and the recorded satisfied-without-new-code requirements.

## Task Commits

1. **Task 1 RED** — `8442b363e` (test): failing tests for the transcript read surface (SWARM-10) — `ReadTranscript`/`ListChildTranscripts` stubbed to compile but always error
2. **Task 1 GREEN** — `74a3a1fcf` (feat): implements the identity-scoped transcript read surface — `report.go` and `transcript_api.go` now share the same hardened path helpers
3. **Task 1 RED (HTTP)** — `357faec39` (test): failing tests for the SWARM-10 transcript HTTP route — handler stubbed to answer 501 unconditionally, route registered in `server.go`'s `Mux()`
4. **Task 1 GREEN (HTTP)** — `894c33a5b` (feat): serves the SWARM-10 transcript route, owner-scoped, 404-hiding
5. **Task 1 wiring** — `8be9ef1b6` (feat): wires the SWARM-10 transcript reader into the daemon's agui server (`cmd/aura/serve_agui.go`) — the piece left uncommitted when the prior session was terminated by a provider rate limit; committed this session after re-running the full WSL verification

**Plan metadata:** this SUMMARY's commit (docs: complete plan)

## Files Created/Modified

- `internal/swarm/transcript_api.go` (145 LOC) — `ReadTranscript`, `ListChildTranscripts`, `transcriptDir`/`transcriptPath` (hardened, shared with the writer), capped at 1 MiB per read (T-51-31)
- `internal/swarm/transcript_api_test.go` (230 LOC) — mid-run partial read, offset monotonicity, empty/missing-directory discovery, not-yet-written child, hostile-`childID`/`conv` table (`..`, `../..`, `a/b`, `a\b`)
- `internal/swarm/report.go` (91 LOC, +26/-19 net) — `dumpTranscript` now calls the shared `transcriptPath` helper; `report_test.go` unchanged (zero-diff confirmed)
- `internal/agui/server_swarm_transcript.go` (110 LOC across both commits) — `handleSwarmTranscript`, `registerSwarmTranscriptRoutes`, the `swarmTranscriptReader` seam
- `internal/agui/server_swarm_transcript_test.go` (147 LOC) — foreign-identity 404, malformed-conv 404, nil-reader 404, happy-path ndjson, reader-error opaque 404
- `internal/agui/server.go` (+13 LOC) — registers `registerSwarmTranscriptRoutes` alongside the other colocated route registrations in `Mux()`
- `cmd/aura/serve_agui.go` (+17 LOC) — `swarmTranscriptAdapter`, `aguiServer.SetSwarmTranscripts(...)` in `wireAGUIServer`

## Task 2: SWARM-11 verified from git

**Amendment predates implementation.** `git log --format='%h %cs %s' -- prd.md | head -20` and `git merge-base --is-ancestor` confirm: commit `a798f6005` ("docs(prd): close the Phase 51 design gate with measurement (#154)", 2026-08-27 07:27:02 +0200) IS an ancestor of `d5b14b2b8` ("feat(51-01): durable delegation enqueue + real claim/reclaim path", 2026-08-27 12:22:36 +0200) — the first `feat(51-*)` implementation commit of this phase, roughly five hours later the same day. The amendment (`prd.md:8504-8585`) narrates and ratifies all four spike outcomes (D-01/D-02/D-03 measurements plus the three live defects found while driving the real agent) before any of Phase 51's implementation code exists.

**The three defect-fix commits are measurement byproducts, not implementation racing ahead of the amendment:**
- `67d24aee4` (2026-08-26, `fix(agent): let a swarm worker dispatch its own tools`) — the operation-fingerprint mismatch that denied 100% of worker tool calls, found and fixed while driving the design-gate spike
- `791dcd7e0` (2026-08-26, `fix(gateway): close the reservations a delegated dispatch opens`) — the orphaned-reservation bug found the same way
- `268580e23` (2026-08-27, `fix(cron): answer where the question was asked`) — the scheduled-run-outcome-never-reaches-origin bug

All three predate `a798f6005` (2026-08-27 07:27:02) or land the same day before it, and the amendment's own text narrates all three as part of the measured outcome it ratifies — they are not code that shipped after the amendment claimed to close the gate.

**Requirements satisfied WITHOUT new production code in this plan or phase:**

- **SWARM-08** — fixed by `67d24aee4` (2026-08-26): `deriveToolOperationContext` (`internal/agent/idempotency_operation.go:41`) used to key its re-entry passthrough guard on `OperationScope` alone, matching `swarm_spawn` against every one of the ten `OperationScopeAgent` tools a worker may call and denying 100% of worker dispatches (41/41 in spike 099). Fixed to key on scope AND fingerprint; re-measured at 0 denials, 3 real `shell_exec` executions with captured output. Guarded by the extended test built in plan 51-05.
- **SWARM-11** — PRD Amendment #154, evidence above.
- **D-09** — duplicate memory-write suppression already works: `internal/arcadedb/memory.go`'s `UpsertFact` (line 272) derives a content key via `factIdentity(fact)` (line 292) and calls `c.attachFactSource(ctx, factKey, fact.Source, now)` (line 300) BEFORE ever creating a new fact — two workers learning the same thing already produce one fact with two sources, the behavior SWARM-07/SC#5 needs. Proven by plan 51-04's concurrency test (70+ live `-race` stress runs, zero failures) rather than built for this plan.

**Honest starting point for this phase's Definition of Done — the `docs/aura-quality-snapshot.md` "Swarm E2E" row is only PARTLY covered.** Reproduced verbatim from `51-RESEARCH.md`'s "The `docs/aura-quality-snapshot.md` 'Swarm E2E' row — what it does NOT cover" section (re-measured 2026-08-27, `0e7cc821a`): the live E2E harness `internal/eval/harness_swarm_e2e_test.go` **remains deleted** (removed with the rest of `internal/eval`), so:

- mail+WhatsApp MCP read-back — NOT run, no number claimed
- the `<1.5×` timing ratio vs. a sequential control — NOT run
- the `≥90%` LLM-judge score — NOT run
- the no-over-spawn check — NOT run

These four gaps are NOT retroactively closed by the design-gate spikes, and are NOT closed by this plan (which touched only observability, not the E2E harness). They must not be assumed closed by plan 51-08's own scoring — plan 51-08 owns the live SC#1-#5 drive and must produce its own evidence for these four items or explicitly carry them forward as open.

## Decisions Made

See `key-decisions` in the frontmatter. The load-bearing one: the daemon wiring for `SetSwarmTranscripts` lands in `cmd/aura/serve_agui.go`'s existing `wireAGUIServer`, not in `cmd/aura/serve.go` as the plan's `files_modified` frontmatter listed — the concern (registering agui seams) already lives in `serve_agui.go`, which composes every other agui dependency (steer inbox, operation registry, asset service, share service). Following the plan's own instruction to "register from a sibling file instead of growing it" if `serve.go` approaches 600 LOC, the wiring instead went to the file that was already the right place for an agui seam, rather than to `serve.go` at all.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 4 boundary, resolved as correct-location-not-gap] `serve_agui.go` vs `serve.go` for the daemon wiring**
- **Found during:** resume-point verification (Task 1 final step)
- **Issue:** the plan's `files_modified` frontmatter lists `cmd/aura/serve.go`, but the prior session's (verified) uncommitted diff wired `SetSwarmTranscripts` into `cmd/aura/serve_agui.go`'s `wireAGUIServer` instead
- **Assessment:** not a deviation requiring a fix — `serve_agui.go` is the existing, correct composition point for every other agui seam (steer inbox at line ~101 of the same function, operation registry, asset service). Wiring here rather than in `serve.go` is the SAME concern (daemon-side agui composition), landed at its already-correct existing location. Left unchanged.
- **Files affected:** none (no code change needed — documented as a record correction only)

**2. [Process] Session continuation after a provider rate limit**
- **Found during:** session start
- **Issue:** a prior executor session was terminated by an HTTP 429 provider rate limit mid-Task-1, after committing 4 of the plan's 5 commits and leaving one file's wiring diff uncommitted but already verified working (`go build`/`go vet`/`go test -race` green in WSL by the orchestrator)
- **Fix:** this session re-verified the resume point (`git log`, `git status`), re-checked every Task 1 acceptance criterion against the tree, re-ran the full WSL verification suite independently, then committed the remaining wiring as the plan's 5th commit before proceeding to Task 2
- **Files modified:** `cmd/aura/serve_agui.go` (committed as `8be9ef1b6`)

---

**Total deviations:** 1 record correction (no code change), 1 process note (session continuation)
**Impact on plan:** none of the plan's truths, prohibitions, or acceptance criteria changed.

## Issues Encountered

- **Provider rate limit (HTTP 429) killed the prior session mid-Task-1.** Recovery followed the operator's standing "commit atomici, piano piano, controlla tutto" instruction: every completed-work claim was re-verified against the actual tree and git log before being trusted (all 4 prior commits present, all 5 Task 1 acceptance criteria individually re-checked, the uncommitted diff re-read and its content matched against the resume-instructions description) before this session added anything.

## Verification (all in WSL, the project's authoritative host)

- `go build ./...`: clean
- `go vet ./internal/swarm/... ./internal/agui/...`: clean
- `go test -race -count=1 ./internal/swarm/ ./internal/agui/ ./cmd/aura/`: all three `ok` (`internal/swarm` 3.246s, `internal/agui` 17.495s, `cmd/aura` 37.169s) — re-run independently in this session with the wiring diff staged, before committing it
- Pre-commit hook (gofmt, file-size, vet, lint) green on the wiring commit: `0 issues`, `1 named file(s) within the 600-LOC cap`
- Acceptance criteria checked individually against the tree: `func ReadTranscript(` and `func ListChildTranscripts(` present (`transcript_api.go:80,119`); `git diff --stat 10e7f5eac..HEAD -- internal/swarm/report_test.go` empty; hostile-`childID`/`conv` table covers `..`, `../..`, `a/b`, `a\b` (`transcript_api_test.go:164,186,208`); foreign-identity 404 route test present (`TestSwarmTranscriptRouteForeignIdentityIs404`); `goleak` package convention present via `internal/swarm/main_test.go`'s `goleak.VerifyTestMain(m)` (package-level, not per-test — the package's established convention); `wc -l` on `report.go`/`transcript_api.go`/`serve_agui.go`/`serve.go` = 91/145/266/564, all ≤ 600
- Task 2's own `<verify>` (`git log --format='%h %cs %s' -- prd.md | head -20`) run and its evidence incorporated above, extended with `git merge-base --is-ancestor` for the precise ordering claim

## User Setup Required

None — no new env var, no manual step.

## Next Phase Readiness

- SWARM-10 and SWARM-11 are both closed. SWARM-10 is code-complete and test-verified; SWARM-11 is verified from git.
- 51-08 (the live SC#1-#5 drive) can now use `GET /api/conversations/{conv}/swarm/{childID}/transcript` to watch a backgrounded worker mid-run instead of waiting blind, and inherits this plan's open gap: the four uncovered Swarm E2E quality-snapshot items (mail/WhatsApp read-back, `<1.5×` timing ratio, `≥90%` judge score, no-over-spawn check) are NOT closed by this plan and must be either produced live or explicitly carried forward as open by 51-08.
- No new env var, no new migration, no schema change — this plan is additive-only (new files plus a shared helper in `report.go`).

---
*Phase: 51-durable-delegation*
*Completed: 2026-08-28*

## Self-Check: PASSED

- Five task commits plus the SUMMARY commit present in `git log --grep=51-07` (spot-checked by the orchestrator: `8442b363e`, `74a3a1fcf`, `357faec39`, `894c33a5b`, `8be9ef1b6`, `e0db96c73`)
- `key-files.created` all exist on disk (`transcript_api.go` 145 LOC, `server_swarm_transcript.go` 100 LOC)
- Post-merge gate after wave 4, in WSL: `go build ./...` clean, `make test` (full untagged suite, 90 packages) exit 0
- `execute:wave:post` gates: schema-drift none, ui.safety-gate no block
