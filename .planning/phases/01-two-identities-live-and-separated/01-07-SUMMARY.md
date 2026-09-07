---
phase: 01-two-identities-live-and-separated
plan: 07
subsystem: research
tags: [authula, totp, rfc6238, e2e-02, open-questions, measurement]

# Dependency graph
requires: []
provides:
  - "01-AUTHULA-TOTP-CONTRACT.md — the measured Authula TOTP enrollment wire contract (v1.43.0): POST /totp/enable returns a plaintext base32 secret in an otpauth:// URI; POST /totp/verify validates a code against the same secret; verdict: headlessly automatable"
  - "01-RESEARCH.md § Open Questions fully dispositioned — Q1 and Q3 (editorial, plan-time) plus Q2 (measured this plan), heading marked closed"
affects: ["01-06 (the D-16 closing harness's forced-first-login login step for identity B)"]

# Actuals (#2632)
actuals:
  tokens: 27000
  tasks: 2
  commits: 2

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Contract measured from the installed module cache ($(go env GOMODCACHE)), never from a running server or a docs site — go doc for the exported surface, then source reads for the parts go doc summarizes but doesn't answer"

key-files:
  created:
    - .planning/phases/01-two-identities-live-and-separated/01-AUTHULA-TOTP-CONTRACT.md
  modified:
    - .planning/phases/01-two-identities-live-and-separated/01-RESEARCH.md

key-decisions:
  - "TOTP enrollment is headlessly automatable: /totp/enable's otpauth:// URI carries the plaintext base32 secret as an ordinary query parameter (not encryption, not QR-only), and /totp/verify checks a code against the same decrypted secret — confirmed by reading enable_usecase.go, verify_totp_usecase.go and totp_service.go directly, not inferred from the API summary alone."
  - "First-login enrollment enforcement is NOT wired today: EnforceFirstLogin only sets two Authula user-metadata markers; cmd/aura/serve_onboarding.go's own comment states the login-time code that reads them 'is wired at the cutover (plan 12)', and a repo-wide grep found no other reference. This is out of scope for what this plan measures but is recorded as a documented limit so plan 01-06 does not assume the redirect exists."

requirements-completed: []  # E2E-02 is shared with 01-01 and 01-06 (neither has a SUMMARY yet) — the shared-ID gate defers marking it complete until every declaring plan finishes (#2388); see update_requirements step in this SUMMARY.

coverage:
  - id: D1
    description: "Authula TOTP enrollment wire contract measured from the installed module (v1.43.0) and recorded in 01-AUTHULA-TOTP-CONTRACT.md, with a one-line verdict and a 'what this does not show' perimeter"
    requirement: "E2E-02"
    verification:
      - kind: other
        ref: "go list -m github.com/Authula/authula (exit 0, v1.43.0) + go doc github.com/Authula/authula/plugins/totp (exit 0, non-empty exported surface) + test -s 01-AUTHULA-TOTP-CONTRACT.md"
        status: pass
    human_judgment: false
  - id: D2
    description: "01-RESEARCH.md § Open Questions Q2 closed with the measured verdict, heading re-marked resolved, Q1/Q3 dispositions untouched"
    requirement: "E2E-02"
    verification:
      - kind: other
        ref: "grep -c 'RESOLVED' 01-RESEARCH.md == 4; grep -c '^[0-9]\\.' == 3; git diff -U0 confined to the Open Questions heading + Q2 hunk"
        status: pass
    human_judgment: false

duration: 7min
completed: 2026-09-07
status: complete
---

# Phase 1 Plan 07: Authula TOTP Enrollment Contract Summary

**Measured from the installed `github.com/Authula/authula v1.43.0` module (no live server): TOTP enrollment IS headlessly automatable — `POST /totp/enable` returns a plaintext base32 secret in a standard `otpauth://` URI, and `POST /totp/verify` accepts an RFC 6238 code computed from it.**

## Performance

- **Duration:** 7 min
- **Started:** 2026-09-07T21:52:00Z (approx, per STATE.md session start)
- **Completed:** 2026-09-07T21:59:00Z
- **Tasks:** 2 completed
- **Files modified:** 2 (1 created, 1 modified)

## Accomplishments

- Read `github.com/Authula/authula/plugins/totp`'s exported surface via `go doc` and, where the
  summary didn't answer the question, its source under `$(go env GOMODCACHE)` directly:
  `routes.go`, `enable_handler.go`, `enable_usecase.go`, `verify_totp_usecase.go`,
  `totp_service.go`, `types.go` — never a running server.
- Established the enrollment entry point (`POST /totp/enable`, session-authenticated) and traced
  the exact secret from `TOTPService.GenerateSecret()` through `BuildURI` (plaintext, in the
  `otpauth://` query string) to `VerifyTOTPUseCase.Verify`'s `TokenService.Decrypt(record.Secret)`
  — proving the enable-response secret and the verify-time secret are the same value, by source,
  not by assumption.
- Recorded the contract in `01-AUTHULA-TOTP-CONTRACT.md` with all six required items: pinned
  version, entry point, response shape, verify-step match, one-line verdict, and a "what this
  does not show" section naming five limits (source-read only; one pinned version; silent on
  Aura's own route mounting; silent on whether first-login enforcement is actually wired today —
  it is not; `SkipVerificationOnEnable`'s effective value inferred from defaults, not a live dump).
- Closed Q2 in `01-RESEARCH.md` § Open Questions with the verdict quoted verbatim and linked, and
  re-marked the section heading as fully resolved — Q1 and Q3's plan-time dispositions untouched.

## Task Commits

Each task was committed atomically:

1. **Task 1: Measure the Authula TOTP enrollment contract from the installed module, and record it** - `a1c63fd65` (docs)
2. **Task 2: Close Q2 — the last undispositioned question in RESEARCH.md** - `4ce9b11e5` (docs)

**Plan metadata:** see final commit below.

## Files Created/Modified

- `.planning/phases/01-two-identities-live-and-separated/01-AUTHULA-TOTP-CONTRACT.md` - the measured contract: version, entry point, response shape, verify match, verdict, limits, and full source citations
- `.planning/phases/01-two-identities-live-and-separated/01-RESEARCH.md` - Q2 disposition appended, § Open Questions heading re-marked resolved

## Decisions Made

- No workaround was needed: the measurement resolved to "automatable," so no blocker report was
  required and the plan proceeded through both tasks without deviation.
- The "what this does not show" section explicitly separates the plugin's wire contract (measured,
  positive) from Aura's own first-login enforcement wiring (unmeasured here, and independently
  confirmed absent by a repo-wide grep) — so plan 01-06's harness design cannot mistake one for
  the other.

## Deviations from Plan

None - plan executed exactly as written. Both tasks' acceptance criteria and `<verify>` blocks
passed on first attempt; no fix-attempt cycles were needed.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Plan `01-06` (wave 4, `depends_on: 01-07`) can now design its forced-first-login harness login
  step against a measured contract instead of an assumption: it should call `POST /totp/enable`
  after email/password login, parse the `secret` query parameter from the returned `otpauth://`
  URI, compute an RFC 6238 code (6 digits, 30s period), and complete enrollment via
  `POST /totp/verify` with the `totp_pending` cookie — using `01-AUTHULA-TOTP-CONTRACT.md` as the
  source of truth for exact field names and routes.
- `01-RESEARCH.md` § Open Questions now carries a disposition for all three questions; no later
  plan in this phase needs to re-open or re-derive any of them.
- `E2E-02` is not yet marked complete in `REQUIREMENTS.md` — it is shared with plans `01-01` and
  `01-06`, neither of which has a SUMMARY yet (the shared-ID gate, #2388, defers marking a
  multi-plan requirement complete until every declaring plan finishes, so the first plan to land
  does not flip it before its siblings prove their own halves).

---
*Phase: 01-two-identities-live-and-separated*
*Completed: 2026-09-07*

## Self-Check: PASSED

- FOUND: `.planning/phases/01-two-identities-live-and-separated/01-AUTHULA-TOTP-CONTRACT.md`
- FOUND: `.planning/phases/01-two-identities-live-and-separated/01-07-SUMMARY.md`
- FOUND: commit `a1c63fd65` (Task 1)
- FOUND: commit `4ce9b11e5` (Task 2)
- Plan-level `<verification>` re-run: `go list -m github.com/Authula/authula` → `v1.43.0` (pass); `go doc github.com/Authula/authula/plugins/totp` → non-empty exported surface (pass); `01-AUTHULA-TOTP-CONTRACT.md` non-empty with verdict + limits section (pass); `01-RESEARCH.md` § Open Questions has 4 `RESOLVED` occurrences across 3 questions, `git diff -U0` confined to the heading + Q2 hunk (pass); `git diff --name-only 8b1c502ee..4ce9b11e5` names only the two planning artifacts (pass).
