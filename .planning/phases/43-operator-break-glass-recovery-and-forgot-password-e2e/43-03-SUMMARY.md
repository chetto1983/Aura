---
phase: 43-operator-break-glass-recovery-and-forgot-password-e2e
plan: 03
subsystem: cli
tags: [break-glass, recover-operator, cli, authula, argon2, x-term, secret-sourcing, offline-recovery]

# Dependency graph
requires:
  - phase: 43-01
    provides: "breakglass.Sourcer + Secrets + selectSoleOperator — the env/--generate/hidden-prompt sourcing engine + operator-resolution guard this glue drives"
  - phase: 43-02
    provides: "breakglass.NewAuthula + Deps + RecoverOperator — the offline Authula provider + guard→setter→re-seed→audit orchestrator this glue calls"
  - phase: 36-multi-user-identity-isolation-authula-cutover
    provides: "webauth.New/Provider (offline Authula CoreServices), config.LoadDB() Authula fields (AuthulaSecret + AuthulaDatabaseURL), cmd/aura/recovery.go identityRecover — the thin resolve→call→print→exit + stdout-only-secret analog"
provides:
  - "cmd/aura/recover_operator.go — identityRecoverOperator: the operator-facing CLI glue (flags → Sourcer → offline Authula CoreServices → breakglass.RecoverOperator → ok/exit)"
  - "aura identity recover-operator — the runnable R1 exit path (offline operator password reset + session-kill + recovery re-seed), R3 sourcing, R4 --no-recovery"
  - "cmd/aura/identity.go — D-05 sibling dispatch (case recover-operator) + identityUsage disambiguation"
  - "go.mod — golang.org/x/term v0.44.0 promoted indirect→direct"
affects: [gsd-verify-work, operator-recovery-runbook, R1-R3-R4-coverage]

# Tech tracking
tech-stack:
  added:
    - "golang.org/x/term v0.44.0 — promoted indirect→direct (hidden-prompt seams term.IsTerminal / term.ReadPassword); already pinned in go.sum, no new supply-chain surface"
  patterns:
    - "Thin CLI glue over owned logic: cmd/aura wires real seams (os.Getenv, x/term, os.Stdout/Stderr, config.LoadDB pool, offline Authula provider) into the tested internal/breakglass primitives — no business logic in cmd/aura (excluded from the 85% floor, behaviourally covered)"
    - "Hand-rolled flag.FlagSet dispatch (flag.ContinueOnError + SetOutput(io.Discard)), NOT cobra — mirrors runIdentity/runDB switch-tree (go.mod has no spf13/cobra)"
    - "Secret discipline (T-43-01): the password is never an argv element; the ok-line + every error name no secret; the ONLY secret emission is the single --generate stdout line written by Sourcer.Source (mirror recovery.go:53-55)"
    - "Fail-safe provider lifecycle: breakglass.NewAuthula provider is always defer Close()d so Authula's expiry workers stop (goleak-clean, T-43-09), even on the post-construction error paths"

key-files:
  created:
    - "cmd/aura/recover_operator.go — identityRecoverOperator + parseRecoverOperatorFlags + readHiddenFromStdin + recoverOperatorUsage (142 LOC)"
  modified:
    - "cmd/aura/identity.go — case recover-operator (D-05) calling identityRecoverOperator(ctx, pool, cfg, args[1:]); identityUsage extended with both recover variants + one-line disambiguation"
    - "go.mod — golang.org/x/term v0.44.0 moved to the direct require block"

key-decisions:
  - "D-05: aura identity recover-operator ships as a hand-rolled (flag.FlagSet, NOT cobra) SIBLING of recover <name>; identityUsage disambiguates token-mint vs offline operator reset + recovery re-seed; the recover <name> token path (recovery.go) is untouched"
  - "The recover_operator.go glue + its identity.go dispatch caller landed in ONE commit (8f799d1a) rather than the literal Task-1-alone boundary: the pre-commit `unused` lint gate rejects the glue until a caller exists — a green-commit necessity, not a scope change. The go.mod x/term promotion is the separate second commit (d2960ade)."
  - "go.sum kept byte-unchanged per the plan's explicit acceptance criterion: `go mod tidy` additionally pruned 81 unrelated stale entries (e.g. sarama v1.50.3); that out-of-scope dependency churn was reverted (`git checkout -- go.sum`), keeping only the surgical go.mod x/term indirect→direct move. go mod verify stays clean (extra go.sum entries never break build/verify)."
  - "An empty AURA_AUTHULA_SECRET fails fast with a clear, secret-free stderr error naming the var (A3/T-43-10) instead of constructing a broken provider or panicking"

patterns-established:
  - "CLI glue verifies via build/vet + a non-destructive usage smoke; the real R1/R3/R4 reset (mutates live auth state, needs a TTY/deploy host) is Manual-Only, with the package-level logic already proven by the 43-01/43-02 unit + db_integration tests"
  - "Positional-argument rejection (fs.NArg()>0 → error) reinforces the T-43-01 argv prohibition: a fat-fingered password can never be silently accepted as a CLI argument"

requirements-completed: []   # R1/R3/R4 are phase-local SPEC labels, not global REQUIREMENTS.md IDs (which key by domain prefix MUSR-/PROF-/…) — mark deliberately NOT run, matching the 43-01/43-02/43-04 precedent; see Issues Encountered

coverage:
  - id: D1
    description: "aura identity recover-operator exists as a D-05 sibling subcommand: dispatches to identityRecoverOperator(ctx, pool, cfg, args[1:]); parses --generate/--no-recovery without cobra; identityUsage lists both recover variants with the disambiguation; the existing recover <name> path is unchanged"
    requirement: "R1"
    verification:
      - kind: automated
        ref: "command: cd D:/Repo/Aura && go build ./... && go vet ./cmd/aura/ (exit 0)"
        status: pass
      - kind: other
        ref: "command: go run ./cmd/aura identity → stderr usage lists 'recover-operator [--generate] [--no-recovery]' + the D-05 disambiguation lines, exit 1, NO DB connection"
        status: pass
    human_judgment: false
  - id: D2
    description: "R1/R3/R4 live exit path: on a single-operator deployment, `aura identity recover-operator` (AURA_RECOVERY_* env / --generate / hidden prompt) resets the operator password, kills sessions, re-seeds identity_recovery, prints an ok: line (no secret), exits 0; --generate prints one secret line; --no-recovery skips the re-seed and warns; empty AURA_AUTHULA_SECRET → clear stderr error"
    requirement: "R1"
    verification:
      - kind: manual_procedural
        ref: "deploy host: aura identity recover-operator against the live single-operator aura DB (Manual-Only per plan — do NOT run automated against live aura)"
        status: unknown
    human_judgment: true
    rationale: "The real reset mutates live Authula auth state and requires a TTY / deployment host; it is the manual operator action, explicitly out of scope for automated verify. The composed logic (guard/setter/re-seed/audit/sourcing) is already proven at the package level by the 43-01 unit tests + 43-02 db_integration throwaway-DB proof (R1/R2/R4/R6/D-06, -race green); this plan is thin glue over those tested primitives."
  - id: D3
    description: "golang.org/x/term v0.44.0 is a DIRECT require (no longer // indirect); go.sum unchanged; module graph verifies"
    verification:
      - kind: automated
        ref: "command: grep 'golang.org/x/term' go.mod (line in the direct golang.org/x/* block, no // indirect) + git diff --stat go.sum (empty) + go mod verify (all modules verified)"
        status: pass
    human_judgment: false

# Metrics
duration: 20min
completed: 2026-07-11
status: complete
---

# Phase 43 Plan 03: recover-operator CLI glue Summary

**`aura identity recover-operator` — the runnable, offline break-glass command: hand-rolled flag.FlagSet glue that sources the new password/answer via os.Getenv + golang.org/x/term seams (breakglass.Sourcer), builds the offline Authula CoreServices, and calls breakglass.RecoverOperator — the last wave turning the tested 43-01/43-02 primitives into an operator-facing command.**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-07-11T12:02:21Z
- **Completed:** 2026-07-11T12:22Z
- **Tasks:** 2
- **Files modified:** 3 (1 created, 2 modified; go.sum touched-then-restored, net byte-unchanged)

## Accomplishments
- `cmd/aura/recover_operator.go` — `identityRecoverOperator` wires the real seams (os.Getenv, `term.IsTerminal`, `term.ReadPassword`, os.Stdout/Stderr) into `breakglass.Sourcer`, derives the Authula DSN (`cfg.AuthulaDatabaseURL` or the aura DB URL), fails fast on an empty `AURA_AUTHULA_SECRET`, builds `breakglass.NewAuthula` (always `defer Close()`), calls `breakglass.RecoverOperator`, warns on `--no-recovery`, and prints a secret-free `ok:` line.
- `aura identity recover-operator` dispatches as a D-05 sibling of `recover <name>`; `identityUsage` disambiguates the two mechanisms (token-mint vs offline operator reset + recovery re-seed); the `recover <name>` path is untouched.
- `golang.org/x/term v0.44.0` promoted indirect→direct (the hidden-prompt seams' import); go.sum byte-unchanged, `go mod verify` clean.
- Secret discipline honored end-to-end: the password is never an argv element, the ok-line + all errors name no secret, and the only secret emission is the single `--generate` stdout line (Sourcer.Source).

## Task Commits

Each task was committed atomically (2 tasks → 2 commits; the file→commit mapping shifted for a green pre-commit lint gate — see Deviations):

1. **Task 1 + Task 2 dispatch: recover_operator.go glue + identity.go D-05 sibling case/usage** - `8f799d1a` (feat)
2. **Task 2 build: golang.org/x/term indirect→direct promotion** - `d2960ade` (build)

**Plan metadata:** this docs commit (SUMMARY + STATE + ROADMAP)

## Files Created/Modified
- `cmd/aura/recover_operator.go` (created, 142 LOC) - `identityRecoverOperator` + `parseRecoverOperatorFlags` (flag.FlagSet, NOT cobra) + `readHiddenFromStdin` (x/term hidden read, prompt to stderr) + `recoverOperatorUsage`.
- `cmd/aura/identity.go` (modified) - `case "recover-operator": identityRecoverOperator(ctx, pool, cfg, args[1:])` (cfg already in scope from config.LoadDB); `identityUsage` extended with both recover variants + the one-line disambiguation. `recover <name>` branch unchanged.
- `go.mod` (modified) - `golang.org/x/term v0.44.0` moved into the direct `golang.org/x/*` require block.

## Decisions Made
- **D-05 sibling, not cobra:** `recover-operator` is a hand-rolled `flag.FlagSet` case beside `recover <name>`; go.mod has no spf13/cobra and CLAUDE.md mandates the existing switch-tree. The two are distinct mechanisms (recovery.go mints a hand-off token; recover-operator does the offline password reset + recovery re-seed).
- **Authula DSN fallback:** `cfg.AuthulaDatabaseURL` when set, else `cfg.DB.URL` (webauth forces `?search_path=authula`) — mirrors `scripts/authula_seed_e2e.go`.
- **Fail-fast on missing secret:** an empty `cfg.AuthulaSecret` prints a clear, secret-free stderr error naming `AURA_AUTHULA_SECRET` before any provider construction (A3/T-43-10).
- **Provider always closed:** `defer provider.Close()` on every post-construction path (T-43-09 goleak-clean).

## Deviations from Plan

The code was implemented exactly as the plan's `<action>` specified — no bug/missing-critical/blocking code fixes were needed (it built clean on the first `go vet`/`go build`). Two mechanical, process-level adaptations:

**1. [Rule 3 - Blocking] recover_operator.go + identity.go committed together, not Task-1-alone**
- **Found during:** Task 1 commit (first attempt)
- **Issue:** The pre-commit `golangci-lint` gate flags `identityRecoverOperator`/`parseRecoverOperatorFlags`/`readHiddenFromStdin`/`recoverOperatorUsage` as `unused` when `cmd/aura/recover_operator.go` is committed alone — the glue has no caller until Task 2 wires the dispatch. The two files are mutually dependent for a green build+lint (identity.go calling a not-yet-defined function wouldn't even compile the other way).
- **Fix:** Landed the glue (Task 1) + its `identity.go` dispatch/usage (Task 2's identity.go portion) in one atomic `feat` commit `8f799d1a`; the `go.mod`/`go.sum` x/term promotion (Task 2's remainder) is the separate `build` commit `d2960ade`. Still 2 commits for 2 tasks, atomic-by-concern, each green through the full hook (gofmt/vet/lint/file-size).
- **Verification:** both commits passed the pre-commit hook with no `--no-verify`.
- **Committed in:** `8f799d1a`, `d2960ade`

**2. [Rule 2 - Correctness] Positional-argument rejection in the flag parser**
- **Found during:** Task 1 (recover_operator.go)
- **Issue:** The plan specified rejecting unknown flags; a stray positional argument (e.g. a fat-fingered password) would otherwise be silently ignored — a soft violation of the T-43-01 "password is never a CLI argument" prohibition.
- **Fix:** `parseRecoverOperatorFlags` returns an error (→ usage + non-zero exit) when `fs.NArg() > 0`, so no bare positional is ever accepted.
- **Verification:** `go vet`/`go build` green; covered by the usage/dispatch path.
- **Committed in:** `8f799d1a`

---

**Total deviations:** 2 (1 blocking commit-structure adaptation, 1 correctness hardening). No code-logic deviation from the plan's `<action>`; no scope creep.
**Impact on plan:** Both preserve the plan's intent (green atomic commits; secret-free argv). All acceptance criteria met.

## Issues Encountered
- **`go mod tidy` over-pruned go.sum:** promoting x/term via `go get golang.org/x/term@v0.44.0` needs zero go.sum change (already pinned), but the follow-up `go mod tidy` additionally removed 81 unrelated stale entries (e.g. `github.com/IBM/sarama v1.50.3`, not in the build graph). The plan's acceptance criterion is explicit ("go.sum is unchanged in content"), and no CI/Makefile enforces `go mod tidy`. **Resolved** by restoring go.sum (`git checkout -- go.sum`) and keeping only the surgical go.mod x/term promotion; `go build ./...`, `go vet`, and `go mod verify` all stay clean (extra go.sum entries never break build/verify). The stale-entry cleanup is pre-existing tech debt, out of scope for this phase.
- **`requirements mark-complete` NOT run (deliberate):** the SPEC's `R1..R6` are phase-local labels; `.planning/REQUIREMENTS.md` keys global IDs by domain prefix (MUSR-/PROF-/LOOP-/…) with no `R1`/`R3`/`R4` checkbox to tick. This mirrors the 43-01/43-02/43-04 precedent (each `requirements-completed: []` for the same reason). R1/R3/R4's user-observable deliverable — the runnable command — is complete regardless; the deep R1/R3/R4 runtime proof is the Manual-Only D2 line + the already-green 43-01/43-02 package tests.

## User Setup Required
None - no external service configuration required. Runtime note (documented in the command's `--help`/usage and error text, A3): `aura identity recover-operator` requires `AURA_AUTHULA_SECRET` (64 hex) plus `AURA_AUTHULA_DATABASE_URL` or `AURA_DB_URL` for the offline Authula reset.

## Next Phase Readiness
- **Phase 43 all 4 plans executed** (01 guard+sourcing, 02 setter+orchestrator+db_integration, 03 CLI glue [this], 04 forgot-password E2E) — ready for `/gsd-verify-work`.
- The runnable command closes the operator lockout exit path: on the live single-operator deployment (`dvdmarchetto@gmail.com`, missing `identity_recovery` row), `aura identity recover-operator` resets login + restores forgot-password. This is the Manual-Only operator action (D2) the verify step should walk the operator through.
- No blockers. NO new deps (x/term promotion only), NO migrations, NO new persistent env (the three `AURA_RECOVERY_*` are ephemeral command inputs, not stored config).

## Self-Check: PASSED
- Files: `cmd/aura/recover_operator.go` FOUND · `cmd/aura/identity.go` FOUND · `go.mod` FOUND (x/term in direct block).
- Commits: `8f799d1a` FOUND · `d2960ade` FOUND.
- Build/vet: `go build ./...` OK · `go vet ./cmd/aura/` OK · `go mod verify` all modules verified.
- Smoke: `go run ./cmd/aura identity` prints the D-05 usage disambiguation (recover-operator sibling), exit 1, no DB connection.

---
*Phase: 43-operator-break-glass-recovery-and-forgot-password-e2e*
*Completed: 2026-07-11*
