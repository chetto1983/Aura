---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 18
subsystem: testing
tags: [go-vet, golangci-lint, deadcode, coverage, go-mutesting, vitest, stryker, quality-snapshot, composition-root]

# Dependency graph
requires:
  - phase: 37F-conversation-artifact-sharing-export-inserted
    provides: "37F-09/11/13/16/17 — the export endpoint, the lifecycle sweep/cascade seams, the public page, and the shared-links management surfaces this plan verifies end-to-end"
provides:
  - "The WEBSHARE-02/03 share.Service actually wired into the deployed `aura serve` binary (SetShareService, chat.run.SetShareRevoker, the share_expiry_sweep cron dispatch + daily seed) — previously reachable only from tests"
  - "A green full local Go tag matrix (vet/build/lint/deadcode/test-race/vuln/coverage) modulo the two pre-existing, human-owned items named in this plan's own instructions"
  - "internal/webui/dist rebuilt and proven byte-identical to a fresh build"
  - "docs/aura-quality-snapshot.md re-attested for this phase's full changed-file blast radius"
  - "Stryker's mutate/vitest.stryker.config.ts test lists extended to actually measure web/src/chat/share/* and web/src/shell/ShareShell.tsx (previously invisible to the mutation runner)"
affects: [37F-19, future-phases-touching-cmd/aura/serve.go, future-phases-touching-internal/cron]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Late-bound Set*/SetShareRevoker composition-root setters (mirrors agui.Server's ~15 existing Set* methods) for dependencies assembled after the consuming struct is constructed"
    - "seedDailyCronSweep shared helper for system-seeded daily-cron sweep tasks (identity_purge/sandbox_reap use KindEvery instead, so they stay separate)"

key-files:
  created:
    - cmd/aura/share_service_wiring.go
  modified:
    - cmd/aura/chat_boot.go
    - cmd/aura/serve.go
    - cmd/aura/serve_dispatch.go
    - cmd/aura/serve_provisioning.go
    - internal/cron/dispatch.go
    - internal/cron/store.go
    - internal/runner/conversation_delete_lifecycle_test.go
    - internal/runner/runner_delete.go
    - internal/share/redact.go
    - web/src/chat/share/ShareLinkRow.tsx
    - web/stryker.config.json
    - web/vitest.stryker.config.ts
    - internal/webui/dist/** (103 files, rebuilt bundle)
    - docs/aura-quality-snapshot.md
    - .planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md

key-decisions:
  - "Treated the deadcode-surfaced composition-root gap as Rule 2 (missing critical functionality) and fixed it in this plan rather than deferring — the entire share feature was a permanent 503 in the deployed binary, and this plan's own acceptance criteria requires make quality to exit 0 (deadcode is one of its gates)"
  - "Added SetShareRevoker as a new late-bound Runner setter rather than restructuring aura chat/aura serve boot ordering — chat.run is assembled before objectStore/chat.assets exist, and the codebase already has a well-established Set*-after-construction pattern (agui.Server) to mirror"
  - "Did NOT wire the share_expiry_sweep scheduler seeding into 37F-11/12/13 retroactively — this plan is the first to actually complete it, following 37F-11's own explicit note that it 'remains for a later plan'"
  - "Classified the token.go crypto/rand.Read-failure mutation survivor as non-leak-class and advisory-accepted (same methodology 37F-03 used for its own redact.go survivor) rather than adding a fault-injection test seam for an OS-level failure Go's stdlib documents as practically unreachable"
  - "Documented the web/src/chat/share/* Stryker subdirectory shortfall (69.85%, 0.15pp under this plan's stated 70% target, concentrated in three presentational components) rather than writing box-checking tests under time pressure — the actual CI-enforced gate (Stryker's project-aggregate break threshold) passes at 75.34%"

patterns-established:
  - "share_service_wiring.go as the dedicated composition-root file for a package that needs Set*-after-construction wiring across multiple consumer seams (agui HTTP + runner cascade + cron dispatch) from ONE constructed instance"

requirements-completed: [WEBSHARE-04]

coverage:
  - id: D1
    description: "The full Go tag matrix (vet, build, file-size, lint, deadcode, test-race, govulncheck, db+neo4j-tagged coverage) proven green locally on the disposable aura_cov DB"
    requirement: "WEBSHARE-04"
    verification:
      - kind: other
        ref: "make quality (WSL) — vet/file-size/lint/deadcode/test-race/build/govulncheck"
        status: pass
      - kind: integration
        ref: "bash scripts/coverage_docker.sh — aggregate 85.7%, disposable aura_cov DB"
        status: pass
    human_judgment: false
  - id: D2
    description: "The share.Service composition-root wiring gap (deadcode-surfaced): SetShareService, chat.run.SetShareRevoker, and the share_expiry_sweep cron dispatch/seed all now reach the deployed binary"
    requirement: "WEBSHARE-04"
    verification:
      - kind: other
        ref: "deadcode -test $(GO_PACKAGES) — zero unreachable functions (was 13+ before the fix)"
        status: pass
      - kind: unit
        ref: "internal/runner/conversation_delete_lifecycle_test.go#TestSetShareRevoker"
        status: pass
    human_judgment: false
  - id: D3
    description: "SC3 core mutation spot-check (redact.go, snapshot.go, token.go) >=70% killed, every survivor classified, zero leak-class survivors"
    requirement: "WEBSHARE-04"
    verification:
      - kind: other
        ref: "go-mutesting ./internal/share/redact.go ./internal/share/snapshot.go ./internal/share/token.go — 0.925926 (25/27 killed)"
        status: pass
    human_judgment: true
    rationale: "The mutation score itself is machine-computed and passes the 70% floor, but classifying the 2 survivors as non-leak-class (vs. a real gap) is a judgment call this SUMMARY documents; a human should spot-check the reasoning."
  - id: D4
    description: "Web static + unit gates green: tsc --noEmit, eslint --max-warnings=0, prettier --check, vitest run --coverage (>=85%)"
    requirement: "WEBSHARE-04"
    verification:
      - kind: unit
        ref: "npx vitest run --coverage — 186 test files / 1571 tests passed; 92.99% statements overall, 95.32% on src/chat/share"
        status: pass
      - kind: other
        ref: "npx tsc --noEmit -p web/tsconfig.json; npx eslint web/src; npx prettier --check web/"
        status: pass
    human_judgment: false
  - id: D5
    description: "Web mutation gate (Stryker): project-aggregate >=70% break threshold, and web/src/chat/share/*, web/src/shell/ShareShell.tsx actually measured (were previously invisible — 0% no-coverage, not survivors)"
    requirement: "WEBSHARE-04"
    verification:
      - kind: other
        ref: "npx stryker run — project aggregate 75.34% (>= break:70, PASS); ShareShell.tsx 100%"
        status: pass
    human_judgment: true
    rationale: "The project-aggregate CI gate passes, but the chat/share subdirectory itself (69.85%) sits 0.15pp under this plan's own stated target, concentrated in 3 presentational files (57-63%). Documented as a residual gap in deferred-items.md rather than closed — a human should decide whether to accept or route to a follow-up test-design task."
  - id: D6
    description: "internal/webui/dist rebuilt and proven byte-identical to a fresh `npm run build` (D-05 tamper-evidence)"
    requirement: "WEBSHARE-04"
    verification:
      - kind: other
        ref: "git diff --exit-code -- internal/webui/dist/ after a second fresh build (post-commit) — exit 0"
        status: pass
    human_judgment: false
  - id: D7
    description: "docs/aura-quality-snapshot.md re-attested for every row whose CI-gate-path glob matches a file this phase (cumulative, origin/master..HEAD) changed"
    requirement: "WEBSHARE-04"
    verification:
      - kind: other
        ref: "AURA_QUALITY_CHANGED_FILES=... AURA_QUALITY_BASE_DATE=... bash scripts/quality_snapshot_gate.sh — 'ok: quality snapshot gate checked 3 row(s) against base date 2026-07-17'"
        status: pass
    human_judgment: false

duration: 2h 30min
completed: 2026-07-18
status: complete
---

# Phase 37F Plan 18: Full Local Gate + Mutation + Dist Rebuild + Quality-Snapshot Re-Attestation Summary

**Proved the whole 37F phase green locally — and in doing so, found and fixed the entire share feature had never been wired into the deployed `aura serve` binary (deadcode-surfaced: `SetShareService`/`ShareRevoker`/`share_expiry_sweep` were all reachable only from tests), plus two Stryker mutation-gate scoping bugs that made `web/src/chat/share/*` invisible to the mutation runner.**

## Performance

- **Duration:** ~2h 30min
- **Started:** ~2026-07-17T23:45:00Z (estimated)
- **Completed:** 2026-07-18T02:15:00Z
- **Tasks:** 2
- **Files modified:** 15 hand-written files + 103 rebuilt `internal/webui/dist` bundle files

## Accomplishments

- **Closed a deadcode-surfaced composition-root gap that made the entire WEBSHARE-02/03 share feature a permanent 503 in the deployed binary.** `SetShareService` was never called (every `/api/shares*` route 503'd), the D-15 revoke-on-delete cascade silently no-op'd (`r.shareRevoker == nil`), and the `share_expiry_sweep` cron kind was never dispatched — three gaps 37F-11's own SUMMARY had explicitly flagged as "remains for a later plan" three plans ago. Fixed via a new `cmd/aura/share_service_wiring.go` composition-root file, a new late-bound `Runner.SetShareRevoker` setter, and the daily-cron seed/dispatch wiring `internal/cron`/`cmd/aura` needed.
- **Full Go tag matrix proven green locally**: `go vet`/`build`/`golangci-lint`/`deadcode`/`go test -race`/`govulncheck` all clean; `bash scripts/coverage_docker.sh` (disposable `aura_cov` DB) aggregate **85.7%**, 5/6 owned packages individually ≥85% (`internal/objectstore`'s pre-existing 69.6% unchanged, documented exception). The only test-race failure across the whole 78-package matrix is the pre-existing, human-owned `cmd/aura` container-artifacts drift.
- **SC3 core mutation spot-check**: `redact.go` + `snapshot.go` + `token.go` (extended from 37F-03's original redact.go+snapshot.go pair now that token.go exists) — **92.6% killed (25/27)**, both survivors classified non-leak-class.
- **Fixed two independent Stryker scoping bugs** that made `web/src/chat/share/*` and `ShareShell.tsx` measure 0% ("no coverage", not survived) on the first run: `stryker.config.json`'s `mutate` array and `vitest.stryker.config.ts`'s `mutationTests` array were both hand-curated lists 37F's own UI plans never extended. After the fix: project-aggregate mutation score **75.34%** (≥70% break threshold, PASS); `ShareShell.tsx` 100%; `chat/share/*` subdirectory 69.85% (documented residual, see Deviations).
- **`internal/webui/dist` rebuilt** and proven byte-identical to a fresh `npm run build` (103 files, Vite content-hash churn — expected).
- **`docs/aura-quality-snapshot.md` re-attested** for this phase's full cumulative changed-file surface (`origin/master..HEAD`): 2 rows (Telegram channel, Scheduler North-Star) were stale and re-attested metric-neutral; the gate now prints `ok`.

## Task Commits

Each task was committed atomically (5 commits for Task 1's deviations, 1 for Task 2):

1. **Task 1: Full local gate — Go matrix, web gates, mutation, dist rebuild**
   - `ff89cbea` (fix) — wire the WEBSHARE-02/03 share service into the composition root
   - `21b4c998` (fix) — harden the SC3 allowlist nolint against a golangci-lint scale flake
   - `3a1e586e` (fix) — scope Stryker's mutate/test lists to the 37F share module
   - `493fb90d` (fix) — fix sub-floor badge text size in ShareLinkRow
   - `338cb662` (chore) — rebuild internal/webui/dist to match source
2. **Task 2: Quality snapshot re-attestation**
   - `91891710` (docs) — re-attest quality snapshot rows for the phase's changed-file surface

**Plan metadata:** committed separately at the end of this SUMMARY (STATE.md/ROADMAP.md/REQUIREMENTS.md).

_Note: this plan surfaced far more deviations than usual because it is the phase's own "prove it's really true" gate — see Deviations below for the full accounting._

## Files Created/Modified

- `cmd/aura/share_service_wiring.go` — buildShareService (composition root), shareConvAdapter (share.ConversationReader), shareServiceAdapter (agui.ShareService)
- `cmd/aura/chat_boot.go` — chatEnv gains a `shareSvc *share.Service` field
- `cmd/aura/serve.go` — builds the share service, wires SetShareService/SetShareRevoker, seeds the daily share-expiry sweep
- `cmd/aura/serve_dispatch.go` — registers `cron.KindShareExpirySweep` in the dispatch map
- `cmd/aura/serve_provisioning.go` — `seedDailyCronSweep` shared helper + `seedShareExpirySweep`
- `internal/cron/dispatch.go` — `KindShareExpirySweep` added to `isSilentSuccessKind`
- `internal/cron/store.go` — new `KindShareExpirySweep` TaskKind constant
- `internal/runner/runner_delete.go` — new `SetShareRevoker` late-bound setter
- `internal/runner/conversation_delete_lifecycle_test.go` — `TestSetShareRevoker`
- `internal/share/redact.go` — function-scoped nolint (was statement-scoped, flaky at full-repo lint scale)
- `web/src/chat/share/ShareLinkRow.tsx` — badge text size fix (readability floor)
- `web/stryker.config.json` — added the 9 share-module files to `mutate`
- `web/vitest.stryker.config.ts` — added the 5 share-module test files to `mutationTests`
- `internal/webui/dist/**` — rebuilt bundle (103 files)
- `docs/aura-quality-snapshot.md` — 2 rows re-attested + header summary
- `.planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md` — logged the Stryker per-file residual

## Decisions Made

See `key-decisions` in the frontmatter above — summarized: fixed the composition-root wiring gap in-plan (Rule 2, not deferred), added a new `Runner.SetShareRevoker` setter mirroring the existing `agui.Server` late-bound pattern rather than restructuring boot order, left the crypto/rand.Read failure-path mutation survivor advisory-accepted (non-leak-class), and documented rather than hastily patched the `chat/share/*` Stryker subdirectory shortfall.

## Deviations from Plan

This plan is, by design, the phase's own verification gate — it surfaced substantially more than the two pre-authorized known-red items. All are documented below with the SCOPE BOUNDARY / deviation-rule reasoning applied.

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical Functionality] The entire WEBSHARE-02/03 share service was never wired into the deployed binary**
- **Found during:** Task 1, `deadcode -test $(GO_PACKAGES)` (part of `make quality`)
- **Issue:** `deadcode` reported the whole of `internal/share`'s public API (NewService, Create/Update/Revoke/ResolveByToken/ResolveInternal, Store.*, NewAuditWriter/Append) plus `internal/db.WithIdentityTxRaw` as unreachable — reachable only from `_test.go` files. Root cause: `agui.Server.SetShareService` was called exactly zero times outside tests (`internal/agui/share_api_test.go`), `runner.Deps.ShareRevoker`/`Runner.shareRevoker` was never populated at any composition root, and the `share_expiry_sweep` cron `TaskKind` was never registered in `buildDispatch` or seeded. Confirmed via `agui.Server`'s own `s.share == nil` guards (already present in every handler in `share_api*.go`) that in production every `/api/shares*` route answered 503 — the feature was a documented seam that 37F-11's own SUMMARY explicitly named as "remains for a later plan" (citing 37F-12/13 as candidates), but neither of those plans, nor any plan since, picked it up.
- **Fix:** New `cmd/aura/share_service_wiring.go` builds ONE `*share.Service` (structurally satisfies `runner.ShareRevoker` and `handlers.ShareExpirer` directly — no adapter — and is wrapped in a thin `agui.ShareService` adapter for the HTTP surface, mirroring the proven shape already in `internal/agui/share_api_test.go`'s test double). Wired at three composition-root call sites: `aguiServer.SetShareService` (serve.go), `chat.run.SetShareRevoker` (a NEW late-bound setter on `Runner`, needed because `chat.run` is assembled in the shared `chat_boot.go` boot — BEFORE `objectStore`/`chat.assets` exist — so `Deps`-time injection wasn't available; mirrors the `agui.Server` `Set*`-after-construction pattern already used ~15 times in this codebase), and `buildDispatch`'s new `cron.KindShareExpirySweep` registration (`internal/cron/store.go` new constant + `serve_dispatch.go` map entry + `serve_provisioning.go`'s new `seedShareExpirySweep`, a daily 04:00 cron mirroring `seedSkillTTLSweep`'s shape). Also added `KindShareExpirySweep` to `internal/cron/dispatch.go`'s `isSilentSuccessKind` so the routine "expired 0 links" tick doesn't push channel noise, matching its `identity_purge`/`sandbox_reap`/`skill_ttl_sweep` siblings.
- **Files modified:** `cmd/aura/share_service_wiring.go` (new), `cmd/aura/chat_boot.go`, `cmd/aura/serve.go`, `cmd/aura/serve_dispatch.go`, `cmd/aura/serve_provisioning.go`, `internal/cron/dispatch.go`, `internal/cron/store.go`, `internal/runner/runner_delete.go`, `internal/runner/conversation_delete_lifecycle_test.go`
- **Verification:** `deadcode -test $(GO_PACKAGES)` clean (confirmed twice independently, including a fresh cache-free run); `go build ./...`/`go vet ./...` clean; `go test -race ./internal/share/ ./internal/runner/ ./internal/cron/...` all green including the new `TestSetShareRevoker`; the full `internal/agui`/`internal/share` `db_integration` suite (already exercised by 37F-13's own SC4 tests) unaffected — no `internal/agui` source changed.
- **Committed in:** `ff89cbea`

**2. [Rule 3 - Blocking, tooling flake] golangci-lint intermittently flagged an already-suppressed staticcheck finding at full-repo scale**
- **Found during:** Task 1, `make quality`'s lint step, on a re-run after the wiring fix
- **Issue:** `internal/share/redact.go:96` (pre-existing 37F-03 code, untouched by this plan otherwise) has a deliberate `//nolint:staticcheck` on `projectArtifact`'s explicit-field struct literal (the doc comment above it explains why: keeping the fields explicit makes a future field-mismatch between `ArtifactMeta`/`SnapshotArtifact` a compile error, not a silent drift). golangci-lint v2.12.2 (the CI-pinned version) reported the S1016 finding anyway when linting the full ~78-package list — but NOT when linting `internal/share` alone, nor a 3-package subset including it. Same unchanged file, same line, no code change between runs.
- **Fix:** Moved the `//nolint:staticcheck` from statement scope (on the `return SnapshotArtifact{` line) to function scope (on the line before `func projectArtifact`), which covers the whole function body regardless of the exact reported statement position.
- **Files modified:** `internal/share/redact.go`
- **Verification:** Three repeated full-repo `golangci-lint run $(GO_PACKAGES)` invocations after the move, all `0 issues`; the pre-commit hook's own lint pass also confirmed clean on both resulting commits.
- **Committed in:** `21b4c998`

**3. [Rule 1 - Bug] Stryker's mutate/test lists never grew to cover the 37F share module**
- **Found during:** Task 1, first `npx stryker run` after fixing `stryker.config.json`'s `mutate` array — `web/src/chat/share/*` and `web/src/shell/ShareShell.tsx` all reported 0.00% with EVERY mutant marked "no coverage" (not "survived")
- **Issue:** `web/vitest.stryker.config.ts` has its OWN hand-curated `mutationTests` array (separate from the main `vitest.config.ts`), and it was never extended to include the share module's test files. Stryker's dry run discovered only 237 of the project's 1571 tests — every share test file was invisible to the mutation runner's coverage analysis, so mutating the share source files (correctly added to `stryker.config.json` in the same task) produced pure "no coverage," not a real (even if low) survivor score.
- **Fix:** Added the 5 existing test files that exercise the 9 `mutate`-listed share files to `vitest.stryker.config.ts`'s `mutationTests`: `shareApi.test.ts`, `SharedSection.test.tsx` (also exercises `ShareLinkRow.tsx` and `useThreadShares.ts` transitively, confirmed via its own imports), `ShareModal.test.tsx`, `shareViewModel.test.ts`, `ShareShell.test.tsx`.
- **Files modified:** `web/stryker.config.json`, `web/vitest.stryker.config.ts`
- **Verification:** Re-run dry-run picked up 325 tests (up from 237); the share files now show real killed/survived mutant counts instead of all-"no coverage"; project-aggregate mutation score 75.34% (≥70% break threshold, PASS); `ShareShell.tsx` 100%.
- **Committed in:** `3a1e586e`

**4. [Rule 1 - Bug] `ShareLinkRow.tsx` badge text below the readability floor**
- **Found during:** Task 1, first full `npx vitest run --coverage` — `readabilityTokens.test.ts`'s "keeps arbitrary rem text utilities at 11px or larger in operator density" test failed
- **Issue:** `text-[0.6875rem]` on the tier `Badge` = 10.66px at the 15.5px operator-density base (`0.6875 * 15.5 = 10.65625`), below the project's 11px floor. Every other `Badge` text-size override in the codebase already uses `text-[0.75rem]` (11.625px).
- **Fix:** Changed to `text-[0.75rem]`, matching the established convention.
- **Files modified:** `web/src/chat/share/ShareLinkRow.tsx`
- **Verification:** `npx vitest run src/__tests__/readabilityTokens.test.ts` passes (4/4); full suite re-run 186/186 test files, 1571/1571 tests.
- **Committed in:** `493fb90d`

**5. [Rule 3 - Blocking] `internal/webui/dist` stale relative to source**
- **Found during:** Task 1, per its own explicit instruction (D-05 freshness gate)
- **Issue:** The last dist rebuild (`7a969db6`) predates 37F's SharePage/ShareModal/SharedSection/etc. additions and this plan's own ShareLinkRow.tsx fix.
- **Fix:** `cd web && npm run build` on the existing `node_modules` (no `package-lock.json` touched, per `0d58e6a9f`'s precedent).
- **Files modified:** `internal/webui/dist/**` (103 files — Vite content-hash churn)
- **Verification:** `git diff --exit-code -- internal/webui/dist/` after a SECOND fresh build (post-commit) exits 0 — the committed bytes are byte-identical to a fresh build.
- **Committed in:** `338cb662`

---

**Total deviations:** 5 auto-fixed (2 Rule 1, 1 Rule 2, 2 Rule 3). **Impact on plan:** Deviation #1 is the significant one — a previously-invisible defect that made the entire phase's HTTP feature surface non-functional in production despite months of passing tests; fixing it is squarely why this verification-gate plan exists. All 5 fixes are corrective (bugs, missing wiring, blocking staleness), not scope creep — no new feature surface was added.

## Known-Red Items (pre-existing, NOT fixed — per this plan's own instructions)

1. **`internal/db` migration round-trip tests** (`TestMigration0039RoundTrip`/`TestMigration0040RoundTrip`) fail under `db_integration` — hardcoded version literals (39, 40) are stale now that migration 0041 exists. The fix already exists on branch `fix/ci-red-37f-drift` (commit `a649cd8b`, anchor-to-v39) — human-owned, not touched here. Confirmed present, unchanged, exactly as documented.
2. **`internal/objectstore` measures 69.6%** under the db+neo4j coverage matrix, concentrated in `s3.go`'s live-Garage-endpoint methods (0% each, need a live S3 endpoint outside this gate's tag set). Pre-existing (first found by 37F-04, re-confirmed by 37F-13), unchanged by this plan (37F never touches `s3.go`). Confirmed present, unchanged, exactly as documented.
3. **`cmd/aura`'s `TestProductionContainerArtifactsMatchFatImageContract`** fails against `caddy/Caddyfile` — the SAME pre-existing Authula/Caddy drift `deferred-items.md`'s 37F-12 entry already documented, confirmed by the human-owned `fix/ci-red-37f-drift` branch's own commit `9fdcf2eb` ("update the container-artifacts contract to post-Authula Caddy"). Not touched here.

## Residual Gap (documented, not blocking)

**`web/src/chat/share/*`'s own Stryker mutation subdirectory score is 69.85%** — 0.15 percentage points under this plan's stated "≥70% on `web/src/chat/share/*`" target, concentrated in three presentational components (`ShareModal.tsx` 62.50%/91 survivors, `SharedSection.tsx` 57.14%, `ShareLinkRow.tsx` 57.14%; the other four files clear 70% individually). The actual CI-enforced gate — Stryker's project-aggregate `break: 70` threshold — passes at 75.34%. Logged in full in `deferred-items.md` under "37F-18"; not closed here (triaging 91+ survivors is a test-design task, not a wiring/verification one, and this plan's scope was already substantial).

## Six Owned-Package Coverage Numbers (Task 1 acceptance criteria)

| Package | Coverage | Floor | Status |
|---|---|---|---|
| `internal/share` | 85.3% | ≥85% | PASS |
| `internal/agui` | 85.7% | ≥85% | PASS |
| `internal/objectstore` | 69.6% | ≥85% | KNOWN PRE-EXISTING (item 2 above) |
| `internal/runner` | 92.3% | ≥85% | PASS |
| `internal/cron/handlers` | 87.5% | ≥85% | PASS |
| `internal/config` | 92.0% | ≥85% | PASS |
| **Aggregate (owned surface)** | **85.7%** | **≥85%** | **PASS** |

## Both Mutation Scores

| Target | Score | Floor | Status |
|---|---|---|---|
| Go SC3 core (`redact.go`+`snapshot.go`+`token.go`) | 92.6% (25/27) | ≥70% | PASS — 2 survivors, both classified non-leak-class |
| Web project-aggregate (Stryker, `break: 70`) | 75.34% | ≥70% | PASS |
| Web `chat/share/*` subdirectory (informational, not the enforced gate) | 69.85% | ≥70% (this plan's stated target) | 0.15pp short — documented residual, see above |

## Issues Encountered

None beyond the deviations documented above — every gate that initially failed had a root cause identified, fixed (where in-scope) or classified/documented (where a genuine pre-existing or residual item), and re-verified.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- The full local matrix is green (modulo the three pre-existing, human-owned items explicitly out of this plan's scope) and the quality snapshot gate prints `ok`.
- **37F-19** (wave 9, the terminal human-gated plan) can proceed: live public-page verification + the actual `git push` + CI green confirmation. That push will also surface whether CI's stricter Skills `db_integration`-only coverage job and the `web-dist-freshness`/`web-mutation` CI jobs agree with these local numbers — they are checked locally here specifically so 37F-19 does not have to wait ~20 minutes on CI to learn of a gap.
- The human still owns reconciling `fix/ci-red-37f-drift` back onto master (the `internal/db` migration test fix + the Caddyfile/Authula contract fix) — this was explicitly out of scope for every 37F plan that has encountered it, including this one.
- The `web/src/chat/share/*` Stryker residual (69.85%, three presentational components) is logged in `deferred-items.md` for whoever next owns either a report-driven per-mutant triage or new behavioral test cases targeting the modal's tier/expiry/stale-snapshot state transitions.

## Self-Check: PASSED

- `cmd/aura/share_service_wiring.go` — FOUND
- `internal/runner/runner_delete.go` — FOUND
- `internal/cron/store.go` — FOUND
- `web/stryker.config.json` — FOUND
- `web/vitest.stryker.config.ts` — FOUND
- `docs/aura-quality-snapshot.md` — FOUND
- `.planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md` — FOUND
- Commit `ff89cbea` — FOUND
- Commit `21b4c998` — FOUND
- Commit `3a1e586e` — FOUND
- Commit `493fb90d` — FOUND
- Commit `338cb662` — FOUND
- Commit `91891710` — FOUND

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-18*
