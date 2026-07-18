# Deferred Items — Phase 37F

Out-of-scope discoveries logged per the executor's SCOPE BOUNDARY rule (fix only what
the current task's changes directly caused; log everything else here instead of fixing it).

## 37F-04 — `internal/objectstore` package-wide coverage is below 85% (pre-existing, unrelated to this plan)

**Found during:** Task 2 (`ShareSnapshotKey`/`ShareArtifactKey`/`ShareKeyPrefix` GREEN commit),
while verifying the plan's own acceptance criterion `go test ./internal/objectstore/ -cover
-count=1 reports >= 85%`.

**What was found:** Plain `go test ./internal/objectstore/ -cover -count=1` reports **63.9%**
for the whole package — below the plan's literal acceptance-criteria threshold. Measured the
baseline at the pre-37F-04 commit (`34a87f589`, `git show 34a87f589:internal/objectstore/types.go`
swapped in temporarily, `share_key_test.go` removed, then both restored via `git checkout HEAD --`):
baseline was **63.6%**. This plan's 3 new functions are **100.0% covered** individually
(`go tool cover -func` confirms `ShareSnapshotKey`/`ShareArtifactKey`/`ShareKeyPrefix` all at
100.0%, matching `AssetKey`'s existing 100.0%) — the delta from 63.6% to 63.9% is a net
*improvement*, not a regression.

**Root cause (pre-existing, not introduced by 37F-04):** the package's remaining uncovered
functions are all live-infrastructure-gated:
- `internal/objectstore/s3.go`: `Put`/`Head`/`Get`/`List`/`Delete`/`ConfigureBrowserUploadCORS`
  (all 0.0%) — need a live Garage/S3 endpoint, tested under a different tag/tier.
- `internal/objectstore/identity_store.go`: `Put`/`Delete` (0.0%), `Resolve` (40.0%) — need a
  live Postgres connection, tested under `db_integration`.
- `internal/objectstore/filesystem.go`: `Delete` (0.0%).

A plain untagged `go test -cover` on this package will never clear 85% without either (a)
running the full `db_integration neo4j_integration` tag matrix per CLAUDE.md's coverage-gate
discipline, or (b) a dedicated task to add unit coverage for the S3/identity-store paths —
neither of which is this plan's scope (37F-04 is pure primitives, explicitly no DB/Garage per
its own `<objective>` and `project_toolchain` constraints).

**Disposition:** NOT fixed here — out of scope for 37F-04 (SCOPE BOUNDARY: only auto-fix
issues directly caused by the current task's changes). Logged for whichever future
phase/plan owns a package-wide `internal/objectstore` coverage push, or for the next
full-matrix (`db_integration neo4j_integration`) measurement to confirm whether the tagged
tiers already clear the floor (CLAUDE.md's owned-surface floor is measured across that full
matrix, not a plain untagged snapshot — this package may already clear 85% once db_integration-
tagged tests are included, which this plan did not re-run to confirm).

**Verification of this plan's own deliverable:** `internal/share` (this plan's other package)
measures 97.4% via plain `go test -cover`, comfortably above the floor. The 3 new
`internal/objectstore` functions are proven 100.0% individually via `go tool cover -func`.

## 37F-07 — `TestHandleCheckTelegramAvailabilityBranches` fails under `db_integration` (pre-existing, unrelated to this plan)

**Found during:** Task 2/3 final verification — running the plan's own
`go test -tags db_integration -race -p 1 -count=1 ./internal/share/ ./internal/agui/` gate.

**What was found:** `TestHandleCheckTelegramAvailabilityBranches/no_token_configured_reports_not-configured`
(`internal/agui/settings_api_branches_test.go:204`) fails with `probe was called with no token
configured`. This test asserts the telegram-availability probe is NOT called when no bot token
is configured; it failed both BEFORE and AFTER re-seeding the wiped `local` identity (see
below), and touches none of this plan's files (`internal/share/*`, `internal/agui/audit_store.go`,
`internal/agui/share_audit_union_test.go`) — it is a telegram/settings concern with zero code path
overlap with `shared_links`/`share_audit`.

**Root cause (pre-existing, environmental):** almost certainly a `TELEGRAM_BOT_TOKEN`-shaped env
var leaking from the parallel session's `.env`/shell state into this test run (matching the
project's documented `.env` cross-contamination class of issue), which makes the probe's
"no token configured" branch observe a token where the test expects none. Not caused by any
Task 1/2/3 change in this plan.

**Also found + already fixed (not deferred):** the SAME full-package run initially showed ~15
unrelated failures, ALL `FK 23503` on `conversations_identity_id_fkey` (e.g.
`TestAgentRunCapability`, `TestConversationsAPI_*`, `TestServer_Integration_*`) — traced to the
seeded `local` identity (`...0001`) being ABSENT from the live `aura` database (a parallel
session's coverage/reset run wiped it — the documented "Re-seed local identity for
db_integration" gotcha). Re-seeded via the exact idempotent statements from migration 0004
(`INSERT ... ON CONFLICT DO NOTHING` for both `aura.identities` and the `*` wildcard grant in
`aura.capability_grants`), then re-ran the full gate: every one of those ~15 failures cleared,
leaving only the telegram probe test above. This was an environmental data-repair, not a code
change, and is NOT logged as a deviation against this plan's deliverable.

**Disposition:** NOT fixed here — out of scope for 37F-07 (SCOPE BOUNDARY). Logged for whoever
owns `internal/agui/settings_api_branches_test.go` / the telegram settings surface, or for a
future `.env`-isolation cleanup of the shared dev Postgres/session.

## 37F-09 — the wiped `local` identity (FK 23503) recurred; scoped export tests were unaffected

**Found during:** Task 2 final verification — running a full-package
`go test -tags db_integration -count=1 -coverprofile=... ./internal/agui/` to double-check the
package-wide coverage floor (beyond the plan's own `-run 'TestShareExport'` scope).

**What was found:** the SAME class of failure 37F-07 already documented above recurred: ~15
pre-existing tests unrelated to this plan (`TestAgentRunCapability`,
`TestConversationsAPI_*`, `TestServer_Integration_SSERoundTrip`, `TestBranchList_*`, etc.) fail
with `insert or update on table "conversations" violates foreign key constraint
"conversations_identity_id_fkey"` — the seeded `local` identity (`...0001`) is again absent from
the live `aura` database (evidently re-wiped by other work against the same shared dev Postgres
since 37F-07's own re-seed). None of the failing tests touch `share_export.go`,
`conversations_api.go`, or `share_export_test.go`.

**This plan's own deliverable was unaffected:** `go test -tags db_integration -race -p 1
-count=1 -run 'TestShareExport' ./internal/agui/` passed 6/6 cleanly (0.11s-0.33s each, real
execution) in the SAME broken DB state, precisely because `share_export_test.go` follows the R-13
discipline (`seedShareExportIdentity` mints a fresh, non-wildcard identity per test — it never
depends on `local`). Total package coverage under the tag still measured **85.8%** (≥ 85% floor)
despite the ~15 unrelated failures; `share_export.go` itself: `handleConversationExport` 75.6%,
`exportFilenameStem` 92.3% (the uncovered handler branches are the nil-asset-service 503 and the
post-owner-gate 500s on `LoadHistory`/`ListForThread`/marshal failure — none required by this
plan's 6 named acceptance tests).

**Disposition:** out of scope for 37F-09's code deliverable (SCOPE BOUNDARY) — no plan file was
touched to work around it. Mirroring 37F-07's own precedent (an "environmental data-repair, not
a code change"), re-applied migration 0004's idempotent seed directly against the live `aura` DB
(`docker exec aura-postgres psql -U aura -d aura`, local-socket trust auth — no `.env` password
needed): `INSERT INTO aura.identities (id, name, kind) VALUES ('...0001', 'local', 'system') ON
CONFLICT DO NOTHING;` + the matching `*` wildcard row in `aura.capability_grants`, both verified
absent beforehand (0 rows) and present after (1 row each). Re-ran the plan's own full
plan-level verification command, `go test -tags db_integration -race -p 1 -count=1
./internal/agui/`: all ~15 FK-23503 failures cleared. The ONLY remaining failure is
`TestHandleCheckTelegramAvailabilityBranches/no_token_configured_reports_not-configured` —
already fully documented above under 37F-07 (a `TELEGRAM_BOT_TOKEN`-shaped env leak, zero overlap
with this plan's files) and confirmed here to be genuinely independent of the identity-seed
issue: it is the one failure that persists even with `local` correctly seeded. Recurrence of the
identity wipe itself points at a still-unresolved cause upstream of any single plan (likely a
parallel session's coverage/reset run against the same shared Postgres, consistent with the
project's documented "coverage-gate-nukes-live-db" history) — logged for whoever picks up the
shared dev-Postgres isolation problem.

## 37F-12 — `TestProductionContainerArtifactsMatchFatImageContract` fails against `caddy/Caddyfile` (pre-existing, unrelated to this plan)

**Found during:** post-task full-suite regression check — `go test ./cmd/aura/... -count=1`,
run beyond the plan's own scoped verification (`-run "TestPublicShareRoute|TestSharePublicCapabilityName"`)
to confirm the mux edits to `serve_webui.go` introduced no collateral breakage.

**What was found:** `TestProductionContainerArtifactsMatchFatImageContract`
(`cmd/aura/container_artifacts_test.go:11`) fails: `caddy/Caddyfile missing "@authed"`. This test
asserts string-literal contracts across `docker/aura/Dockerfile`, `compose.yaml`,
`compose.gvisor.yaml`, `caddy/Caddyfile`, `.dockerignore`, and `scripts/garage_bootstrap.sh` — none
of which this plan's declared files (`cmd/aura/serve_webui_share.go`, `cmd/aura/serve_webui.go`,
`cmd/aura/share_public_route_test.go`, `internal/agui/share_public_route_test.go`) touch or
reference. `git status --short` at the time of this run showed `caddy/Caddyfile` completely
untracked-as-modified (clean) — the failure is against the file's content already committed at
HEAD, not against anything this plan wrote.

**Root cause (pre-existing, environmental):** this most likely traces to the same known drift my
orchestrating instructions flagged at run start — a separate branch `fix/ci-red-37f-drift` plus a
leaked `749a2c54` `ci.yml` commit exist on/around `master`, explicitly called out as "the human
owns ALL of that reconciliation." A stale `caddy/Caddyfile` missing an `@authed` matcher some
other concurrent change expects is consistent with that description. Not caused by, and not
touching, any file this plan modifies.

**Disposition:** NOT fixed here — out of scope for 37F-12 (SCOPE BOUNDARY: only fix issues
directly caused by the current task's changes; `git_discipline_this_run` additionally scoped this
run to commit ONLY this plan's declared files). Logged for whoever reconciles
`fix/ci-red-37f-drift` back onto `master`.

**Verification this plan's own deliverable is unaffected:** `go build ./...`, `go vet ./cmd/aura/
./internal/agui/`, and the full `internal/agui` untagged suite (`go test ./internal/agui/... -count=1`)
are all green. The ONLY failure in a whole-package `cmd/aura` run is this one pre-existing,
unrelated test.

## 37F-13 — `bash scripts/coverage_docker.sh` cannot exit 0: pre-existing `internal/db` migration
## round-trip failures block the run before it computes a percentage (pre-existing, human-reserved)

**Found during:** Task 2's required verification, `bash scripts/coverage_docker.sh` (disposable
`aura_cov` DB, WSL, real Docker-backed mcp-neo4j-cypher).

**What was found:** the full `go test -tags "db_integration neo4j_integration" -p 1 ./internal/...`
run inside `coverage_gate.sh` fails two tests in `internal/db`, unrelated to anything `internal/db`
itself normally does wrong:

```
--- FAIL: TestMigration0039RoundTrip (1.06s)
    migrate_0039_integration_test.go:57: version=41, want 39
--- FAIL: TestMigration0040RoundTrip (0.94s)
    migrate_0040_integration_test.go:57: version=41, want 40
```

Both tests run `Migrate()` (full up-migrate to the latest file on disk) and assert the reported
schema version equals a HARDCODED literal (39, 40) that was correct only until the NEXT migration
landed. Migration `0041_shared_links_rls` (37F-20, commit `d158bc97`, already an ancestor of this
plan's starting HEAD `439c82da` — verified via `git merge-base --is-ancestor`) moved the on-disk
floor to 41, so both hardcoded assertions are now stale. This is a pre-existing regression on
`master`, not something introduced by 37F-13 — `git status` at the start of this plan showed zero
uncommitted changes to `internal/db/`.

**Root cause, and why NOT fixed here:** 37F-20's own SUMMARY (`37F-20-SUMMARY.md` Deviations /
Known Follow-ups) already found and named this exact gap: it explicitly states migration 0041
shipped with NO `migrate_0041_integration_test.go` companion (unlike 0040's precedent) because
"this run operated under an explicit file-scope lock to the plan's 5 declared files (**a separate
branch and an unrelated leaked commit exist on master that are the human's to reconcile**)." This
plan's own orchestrating instructions repeat the identical scoping boundary verbatim: "A separate
branch `fix/ci-red-37f-drift` and a leaked commit `749a2c54` exist on/around master — the human
owns ALL of that reconciliation... If HEAD is not on master or the tree has unexpected foreign
changes, STOP." Editing `migrate_0039_integration_test.go`/`migrate_0040_integration_test.go` —
even though the fix is textually trivial (update two hardcoded literals, or make the assertion
relative) — would mean reaching into exactly the area two independent plans have now flagged as
reserved for that human-owned branch. Per SCOPE BOUNDARY (only fix issues the current task's own
changes directly caused) this is out of scope for 37F-13 a second time.

**Practical impact on this plan's own verification:** `coverage_gate.sh` aborts
(`if ! go test ...; then exit 1; fi`) BEFORE it ever reaches the `go tool cover -func` aggregation
step, so `bash scripts/coverage_docker.sh` cannot literally exit 0 while this persists — even
though `go test` does not abort early package-by-package (Go's test runner completes every
package regardless of an earlier one's failure) and the coverprofile (`cover_gate.out`, 1.3 MB,
fresh) is fully populated for every package that ran, INCLUDING `internal/db` itself (which still
reported `coverage: 82.0%` despite its two assertion failures — a test-logic bug, not a compile
failure, so its instrumentation is unaffected). This plan worked around the abort by applying
`coverage_gate.sh`'s own filter (`grep -vE '/internal/db/sqlc/|/internal/agent/agenttest/|
/internal/llm/client\.go:'`) directly to that fresh profile and running
`go tool cover -func` by hand — see 37F-13-SUMMARY.md for the resulting real aggregate and
per-package numbers. This is a legitimate reconstruction of what the script would have printed,
not a fabricated number, but it is NOT the same as the script itself exiting 0, and is disclosed
as such.

**Disposition:** NOT fixed here — out of scope for 37F-13 (SCOPE BOUNDARY; also explicitly
in-territory of the human-owned `fix/ci-red-37f-drift` reconciliation per both this plan's own
orchestrating instructions and 37F-20's independent prior finding). Logged for whoever reconciles
that branch: the fix is either (a) update the two hardcoded version literals to the current floor
each time a migration lands (fragile), or (b) change both tests to assert against
`len(os.ReadDir("internal/db/migrations"))`-derived expectations or the highest `.up.sql` file's
numeric prefix instead of a hardcoded literal (matches this file's own project-wide "the directory
is the only source of truth, not a hardcoded number" discipline already applied to migration
*authoring* in CLAUDE.md's Persistence section), or (c) add the missing
`migrate_0041_integration_test.go` companion 37F-20 already flagged as a known follow-up, retiring
0039/0040's role as the "latest floor" proof entirely.

## 37F-13 — `internal/objectstore` measures 69.6% under the REAL two-tag gate (pre-existing,
## structural, already deferred once by 37F-04 — not closed by this plan)

**Found during:** Task 2's per-package coverage assertion (the 2026-06-13 campaign floor is
per-package, not just aggregate) against the same fresh, real `db_integration neo4j_integration`
coverprofile the aggregate number above is drawn from.

**What was found:** `internal/objectstore` (top-level package only, excluding the sibling
`internal/objectstore/garageadmin` package which is separately at 76.6%) measures **69.6%** —
below the 85% floor. Per-function breakdown (`go tool cover -func` against the isolated
package slice) shows the shortfall is overwhelmingly concentrated in `s3.go`'s live-S3-API
methods, every one of them at literal 0%: `ConfigureBrowserUploadCORS`, `Put`, `Head`, `Get`,
`List`, `Delete`. Every OTHER file in the package is reasonably covered under this real tag
combo: `identity_store.go` 76.9–100% (its `Resolve` alone is 93.3%, confirming it IS exercised
under `db_integration` as expected), `fake.go` 71.4–100%, `types.go` (37F-04's own 3 functions)
100% each, and `filesystem.go` mostly 66.7–100% (one exception: `Delete` at 0.0%, a small,
non-decisive function).

**Root cause, and why NOT fixed here:** `s3.go`'s six 0%-covered methods each drive a real
AWS-SDK S3 client call against a live S3-compatible endpoint (Garage) — none of them contain
meaningfully-unit-testable pure logic without first introducing a mockable client seam, which
would be a structural refactor of pre-existing, working infrastructure that predates 37F entirely
(37F never touched `s3.go`; the whole package's ONLY 37F-authored code is `types.go`'s three key
constructors, each independently at 100%, confirmed by 37F-04's own SUMMARY / this phase's
`deferred-items.md` 37F-04 entry above). This is the SAME class of gap CLAUDE.md documents for
`internal/sandbox/usersandbox`'s `DockerBackend` (Docker-daemon-gated code with no
`docker_integration` CI tier) — a live-endpoint dependency the coverage gate's tag set
(`db_integration neo4j_integration` only, no `garage_integration` job) structurally cannot reach.
37F-04 already discovered and deferred this EXACT package (then measuring 63.6–63.9% under a
plain untagged run) with the identical disposition: "Logged for whichever future phase/plan owns
a package-wide `internal/objectstore` coverage push... neither of which is this plan's scope."
37F-13/WEBSHARE-04's own scope is the SC4 cross-identity deny test and the share.public capability
gate — introducing an AWS-SDK S3 client mock seam is an unrelated, non-trivial engineering task
several times the size of this plan's actual deliverable, and would not move any WEBSHARE-04
security property.

**Disposition:** NOT closed here — out of scope for 37F-13 (SCOPE BOUNDARY; a second, independent
confirmation of 37F-04's own prior deferral, now measured against the REAL two-tag gate instead of
a plain untagged run). The aggregate coverage gate (the actual enforced CI mechanism —
`coverage_gate.sh` has no per-package assertion of its own; the per-package floor is a
documentation-level discipline from the 2026-06-13 campaign) clears 85% comfortably (85.7%,
computed on the same profile — see 37F-13-SUMMARY.md) specifically BECAUSE `internal/objectstore`
is a small package relative to the owned-surface total; this shortfall does not currently drag the
enforced gate below the floor. Logged for whoever next owns either (a) an `s3.go` client-seam
refactor + mock-backed unit tests, or (b) a live-Garage `garage_integration` CI tier, whichever
this project adopts first.

## 37F-18 — `web/src/chat/share/*` Stryker mutation score is 69.85% (3 files below 70%,
## project-aggregate gate still passes — not closed here)

**Found during:** Task 1's web mutation gate, after fixing two real scoping bugs (both already
committed by this plan): `stryker.config.json`'s `mutate` array and `vitest.stryker.config.ts`'s
`mutationTests` array never grew to cover the share module, so every prior Stryker run had
measured 0% "no coverage" for these files — not a survivor score, an invisibility bug. With both
lists corrected, the REAL score is now visible for the first time.

**What was found:** `npx stryker run`'s project-wide aggregate is 75.34% (>= the `break: 70`
threshold in `stryker.config.json` — the actual CI-enforced gate, `web-mutation` in `ci.yml`, PASSES).
The `chat/share` subdirectory's OWN aggregate is **69.85%** — 0.15 percentage points under this
plan's stated "≥70% on `web/src/chat/share/*`" target, driven almost entirely by three
presentational components: `ShareModal.tsx` 62.50% (91 survived / 261 valid mutants — by far the
largest contributor), `SharedSection.tsx` 57.14% (14/34), `ShareLinkRow.tsx` 57.14% (17/41). The
other four files clear 70% individually: `shareViewModel.ts` 91.78%, `useThreadShares.ts` 83.33%,
`shareApi.ts` 82.14%, `RevokeConfirmDialog.tsx` 80.00%. `web/src/shell/ShareShell.tsx` is 100.00%.

**Root cause (not investigated further here):** the three low files are React/JSX-heavy
presentational components (a modal's tier/expiry radio state, a list section's empty/error
branches, a row's expiry-badge conditionals) — the class of code where Stryker's mutators
routinely generate large numbers of "equivalent" or extremely hard-to-kill mutants (conditional
className toggles, JSX attribute permutations, string-literal ARIA labels) that behavioral tests
using Testing Library's role/text queries don't exhaustively assert. No individual survivor was
triaged against the JSON/HTML mutant detail (this plan's `stryker.config.json` only wires the
`clear-text`/`progress` reporters, so no structured per-mutant report exists to triage from without
a further ~20-minute re-run with `reporters: ["html"]` added).

**Disposition:** NOT closed here — the CI-enforced gate (Stryker's own project-aggregate `break`
threshold) already passes, and triaging 91+ survivors in `ShareModal.tsx` down to real-gap-vs-
equivalent classifications is a genuine test-design task, not a wiring/verification one; doing it
hastily under this plan's already-large scope would risk exactly the CLAUDE.md "NO TEST BABY
SITTING" box-checking this project explicitly rejects. Logged for whoever next owns either (a) an
HTML/JSON Stryker report re-run + per-mutant triage of `ShareModal.tsx`/`SharedSection.tsx`/
`ShareLinkRow.tsx`, or (b) new behavioral test cases targeting the specific state transitions
(tier selection, expiry option, stale-snapshot banner) these three components own.
