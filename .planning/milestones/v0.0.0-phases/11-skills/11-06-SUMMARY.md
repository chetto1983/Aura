---
phase: 11-skills
plan: 06
subsystem: skills
tags: [skills, slice-7d, installer, native-git-clone, canonical-hash, tofu-pin, symlink-strip, always-strip, red-flags, catalog-default-on, discovery-install-loop, cap-07-complete]

requires:
  - phase: 11-02
    provides: "skill tool ActionRouter (reserved install/catalog keys) + skillLoader consumer seam + the cycle-free tools↔skills boundary"
  - phase: 11-03
    provides: "ValidateForWrite (allowBlocklisted override) + SanitizeName + ValidateNameAgainstDir + CatalogClient.Search (installs-ranked, D-12 disable) + AURA_SKILL_* config"
  - phase: 11-04
    provides: "Writer (pending+audit, db.WithTx) + HashSkillDir canonical (relPath,bytes) sha256 + copyTreeNoSymlinks/Materialize symlink-strip + migration 0010 append-only audit (D-29 install tuple)"
  - phase: 11-05
    provides: "skillWriter/skillAlerter tool seams + ask_user *ErrAwaitingUserInput pause idiom + ResumeHandler activation channel + cmd/aura/skills.go runSkills switch"
  - phase: 08
    provides: "scoring.ComputeSkillTier (install=Risky) + the backup.go LookPath fixed-argv exec discipline (G204 rationale precedent)"
provides:
  - "internal/skills/hash.go: CanonicalHash(dir) — Aura's OWN TOFU pin (byte-sorted (relPath,bytes) sha256, D-15), formalizing HashSkillDir; never interops the upstream computedHash"
  - "internal/skills/installer.go: Installer.Install — native git clone (LookPath fixed-argv, --depth 1 --single-branch -c core.autocrlf=false, GIT_TERMINAL_PROMPT=0, -- repoURL guard), symlink-strip stage, unconditional always:true strip (D-10), red-flag detection (metadata.install[]/tool wildcard/bundled exec, D-13), validate, skills-lock.json canonical-hash pin, pending+audit via writer — NEVER self-activates"
  - "internal/skills/writer.go WriteInstallPending: lands the staged tree (multi-file) in pending/ + the D-29 install pending audit tuple"
  - "internal/skills/writer_activate.go SetAlways: operator re-enable/disable of the stripped always flag on an active skill (D-10)"
  - "internal/skills/catalog.go CatalogClient.Disabled(): D-12 escape-hatch read (no dial)"
  - "internal/agent/tools/skill_install.go: actionCatalog (default-ON discovery, D-12) + actionInstall (clone+stage pending + red-flag-surfacing ask_user gate, D-13; model never self-activates D-03) + skillCatalog/skillInstaller consumer seams"
  - "cmd/aura adapters (skillCatalogAdapter/skillInstallerAdapter) + aura skills install/catalog/always CLI"
affects: [11-07, "skill-snippets", "skill-restore-archive", "north-star-xlsx-e2e"]

tech-stack:
  added: []
  patterns:
    - "Native git-clone-as-transport: LookPath-resolved git + FIXED argv (clone flags constants) + the '--' separator + a validateRepoURL value guard (no leading '-', no NUL, URL/scp shape) + GIT_TERMINAL_PROMPT=0 + core.autocrlf=false — no npm/pip/cargo, no postinstall surface (D-14)"
    - "Aura-owned canonical hash as the TOFU pin: byte-sorted (relPath,bytes) sha256 over the symlink-stripped staged tree, written into skills-lock.json AFTER hashing (so the lockfile excludes itself); deliberately NOT the locale/platform-sensitive upstream computedHash (spike 004b)"
    - "Always-strip persisted on disk: the installer rewrites SKILL.md from the parsed always:false frontmatter via the shared skillFileBytes renderer BEFORE the hash + pending write, so a create and an install of identical content hash identically AND the on-disk pending skill carries no always:true (D-10)"
    - "Red flags as advisory gate inputs: metadata.install[]/tool-wildcard/bundled-executable detection feeds the ask_user gate question; nothing is auto-run (bundled scripts only ever execute later via sandbox_exec)"
    - "Discovery→install loop via two consumer seams: skillCatalog (Search+Disabled) + skillInstaller (Install→summary) keep tools cycle-free of internal/skills; the live CatalogClient/Installer adapt at the composition root"

key-files:
  created:
    - internal/skills/hash.go
    - internal/skills/hash_test.go
    - internal/skills/installer.go
    - internal/skills/installer_test.go
    - internal/skills/installer_integration_test.go
    - internal/agent/tools/skill_install.go
    - internal/agent/tools/skill_install_test.go
  modified:
    - internal/skills/writer.go
    - internal/skills/writer_activate.go
    - internal/skills/writer_test.go
    - internal/skills/catalog.go
    - internal/agent/tools/skill.go
    - internal/agent/tools/skill_test.go
    - cmd/aura/serve_adapters.go
    - cmd/aura/skills.go

key-decisions:
  - "CanonicalHash is a thin formalization over the established HashSkillDir (11-04), NOT a second algorithm — the writer (single SKILL.md) and the installer (cloned tree) share ONE sha256 helper so a create-then-reinstall of identical content matches. hash.go satisfies the plan's 'hash.go contains sha256' acceptance by naming the algorithm in CanonicalHash's doc + the canonicalHashAlgo constant the lockfile writer single-sources."
  - "The installer writes its OWN skills-lock.json (a key literally named computedHash carrying AURA's hash) but NEVER reads/parses an upstream one — the acceptance 'no upstream computedHash consume path' means no PARSE of the locale-sensitive upstream value, which is honored (only a write of our own pin)."
  - "always:true is stripped by rewriting SKILL.md from the always:false frontmatter into the staged tree (persisted on disk), not merely zeroing the in-memory struct — so the on-disk pending skill + its canonical hash both reflect the strip (D-10), and TestInstallClonesAndStrips_AlwaysStrip asserts the staged file no longer contains `always: true`."
  - "WriteInstallPending (new Writer method) promotes the WHOLE staged tree into pending/ (copyTreeNoSymlinks), not just SKILL.md — an installed skill may bundle reference files; the single-file writePending stays the authored-skill path."
  - "Catalog is wired pool-FREE (default-ON, D-12) so the model can discover skills even on a read-only manifest path; the Installer is pool-gated (it needs the audit tx). CatalogClient.Disabled() reads the D-12 flag directly (no probe dial)."
  - "The 11-02 'not yet available' dispatch test switched its example from install→restore (install is now wired); restore/archive remain the genuinely-reserved keys."

patterns-established:
  - "Supply-chain content gate without a package manager: clone (content-gated) → symlink-strip → always-strip → red-flag surface → validator → canonical-hash TOFU pin → pending+audit → human-only activation; the only host tool is operator-provisioned git, the cloned content is never trusted"
  - "Operator override after the gate: --allow-blocklisted passes ValidateForWrite's allowBlocklisted=true ONLY at the CLI AFTER the operator has seen the match; the model install path always passes false (hard reject)"

requirements-completed: [CAP-07]

duration: ~45min
completed: 2026-06-05
---

# Phase 11 Plan 06: Skills Installer + Catalog/Install Actions (Slice 7d) Summary

**Closes CAP-07: `aura skills install <repo>` and the model-facing `skill action=install` clone a third-party skill natively (LookPath fixed-argv `git clone --depth 1 --single-branch -c core.autocrlf=false`, `GIT_TERMINAL_PROMPT=0`, `--`-guarded validated repoURL — no npm/pip, no postinstall surface, D-14), strip symlinks at the copy boundary (T-11-06-T2), strip `always:true` unconditionally and persist it on disk (D-10), detect supply-chain red flags (metadata.install[] / tool wildcards / bundled executables, D-13), validate at the write boundary, pin Aura's OWN canonical byte-sorted (relPath,bytes) sha256 into `skills-lock.json` (TOFU, NOT the locale-sensitive upstream computedHash, D-15), and land the skill in `pending/` + the append-only audit ledger — NEVER self-activating (D-03/T-11-06-E2). `skill action=catalog` browses skills.sh `/api/search` default-ON (D-12). The discovery→install loop the North-Star xlsx E2E needs is live; install pauses via ask_user surfacing the red flags, and only a human resume or `aura skills approve` activates.**

## Performance

- **Duration:** ~45 min
- **Completed:** 2026-06-05
- **Tasks:** 2 (both autonomous)
- **Files:** 15 (7 created, 8 modified)

## Accomplishments

### Task 1 — Native clone installer + canonical hash + red-flag detection + always-strip (`b52fdc5f`)

- **`hash.go`** — `CanonicalHash(dir)` formalizes the byte-sorted (relPath, content) sha256 TOFU pin (D-15) over a symlink-stripped tree, delegating to the established `HashSkillDir` (11-04) so the writer and installer share ONE sha256 helper. The `"sha256:"` prefix is part of the pin; `canonicalHashAlgo` single-sources the algorithm name for the lockfile writer.
- **`installer.go`** — `Installer.Install`: (1) `validateRepoURL` rejects option-injection / NUL / non-URL shapes BEFORE the clone (the repoURL is the only interpolated value); (2) `exec.LookPath("git")` + a FIXED argv (`clone --depth 1 --single-branch -c core.autocrlf=false -- <repoURL> <scratch>`) with `GIT_TERMINAL_PROMPT=0` (never hang on auth, T-11-06-T3) and `//nolint:gosec` carrying the G204 rationale; (3) `locateSkillDir` finds the SKILL.md (repo root or one/two levels down, the anthropics/skills layout); (4) `always:true` stripped UNCONDITIONALLY (D-10), persisted by rewriting SKILL.md from the always:false frontmatter into the staged tree; (5) `detectRedFlags` collects metadata.install[] / tool-wildcard / bundled-executable flags for the gate (nothing auto-runs); (6) `ValidateNameAgainstDir` + `ValidateForWrite` (operator-overridable via `AllowBlocklisted`, model=false hard-reject); (7) `CanonicalHash` → the TOFU pin written into `skills-lock.json`; (8) `WriteInstallPending` lands the tree in `pending/` + the D-29 install pending audit tuple. It NEVER activates.
- **`writer.go` WriteInstallPending** — promotes the whole staged (symlink-stripped) tree into `pending/<name>/` atomically (temp-dir + rename) then records the `(NULL, NULL, true, false)` install pending audit row inside `db.WithTx`.
- **Tests** — `installer_test.go` builds LOCAL `file://` git-repo fixtures (no network) so the clone path RUNS deterministically: always-strip (the staged SKILL.md no longer carries `always: true`), symlink-strip (a host-pointing symlink is dropped, Linux), red-flag detection (all three kinds), canonical-hash stability across two clone runs + independent recomputation + `sha256:` prefix, ErrNoSkillFound, and the repoURL guard. `hash_test.go` proves order-independence + symlink-exclusion + content-sensitivity. `installer_integration_test.go` (db_integration) runs the FULL `Install` → pending + exactly one install audit row (the pending tuple, canonical hash) + NO active dir (no self-activation).

### Task 2 — install + catalog actions (model discovery→install) + CLI (`e2eb3c72`)

- **`skill_install.go`** — `actionCatalog` (default-ON, D-12): ranks by installs (the live client ranks), guards an empty query, returns enable-guidance when disabled WITHOUT dialing, formats via `NewResult`. `actionInstall`: calls the `skillInstaller` seam (clone+stage pending), surfaces the red flags + the always-strip note in the ask_user gate question (D-13), and PAUSES via `*ErrAwaitingUserInput` — the model cannot self-approve (D-03); an installer error (blocklist hit, missing SKILL.md, unreachable repo) is a tool error (self-correct), NOT a pause. New consumer seams `skillCatalog` (Search+Disabled) + `skillInstaller` (Install→summary) keep `internal/agent/tools` free of `internal/skills`.
- **`skill.go`** — the router fills the reserved `install`/`catalog` keys (the 11-02 `notYetWired` placeholders are replaced); the schema `action` description marks them available (restore/archive stay reserved); a new `repo` property is documented.
- **`catalog.go`** — `CatalogClient.Disabled()` exposes the D-12 flag directly (no probe dial).
- **`writer_activate.go` SetAlways** — the operator-only `aura skills always <name> {on|off}` path that re-enables (or disables) the always flag the installer stripped (D-10): name-guarded chokepoint, rewrites the active SKILL.md, re-materializes, records a cli update audit row.
- **`serve_adapters.go`** — `skillCatalogAdapter` (pool-free, default-ON) + `skillInstallerAdapter` (pool-gated, model actor) wired in `newSkillTool`; the live `CatalogClient`/`Installer` adapt the consumer seams.
- **`cmd/aura/skills.go`** — `install <repo> [--allow-blocklisted]` (operator install → pending + red-flag print + approve hint), `catalog {search <query>|disable-catalog|enable-catalog}`, `always <name> {on|off}`.

## Task Commits

1. **Task 1: native git-clone installer + canonical hash + red-flag detection + always-strip** — `b52fdc5f` (feat)
2. **Task 2: skill install + catalog actions + aura skills install/catalog/always CLI** — `e2eb3c72` (feat)

## Decisions Made

- **CanonicalHash formalizes HashSkillDir (one algorithm)** — rather than introduce a second hash, `hash.go` exposes `CanonicalHash` delegating to the 11-04 `HashSkillDir`, so the writer (single SKILL.md) and installer (cloned tree) pin identically. The acceptance's "hash.go contains sha256" is met by the documented algorithm + the `canonicalHashAlgo` constant.
- **We write our own `computedHash` key, never parse the upstream one** — the lockfile carries a key literally named `computedHash` holding AURA's hash; the installer has NO path that reads/parses an upstream `skills-lock.json computedHash` (the locale/platform-sensitive value spike 004b warned against). The acceptance "no upstream computedHash consume path" is honored.
- **always:true strip is persisted on disk** — the installer rewrites SKILL.md from the always:false frontmatter into the staged tree before hashing + the pending write, so the on-disk pending skill AND its canonical hash both reflect the strip; re-enable is the explicit `aura skills always <name> on` operator path (D-10).
- **WriteInstallPending promotes the whole tree** — an installed skill may bundle reference files, so the install pending write copies the entire staged (symlink-stripped) tree, unlike the single-file authored `writePending`.
- **Catalog default-ON pool-free, installer pool-gated** — the model can discover skills on any path (D-12); the installer needs the audit tx so it is wired only when a pool exists.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] gosec G703 path-traversal taint on SetAlways WriteFile**
- **Found during:** Task 2 (golangci-lint Gate-2 pass).
- **Issue:** `SetAlways` writes `activeDir/SKILL.md` where `activeDir = activeRoot/<name>`; gosec flagged the WriteFile as a tainted-path traversal because `name` reaches the join unguarded.
- **Fix:** added a `SanitizeName(name, name)` chokepoint at the top of `SetAlways` (a name matching `^[a-z0-9-]{1,64}$` cannot contain a separator or `..`) + a justified `#nosec G703` citing the validated name + operator-controlled root. This is the package's established name-chokepoint idiom (11-03 SanitizeName).
- **Files modified:** internal/skills/writer_activate.go.
- **Committed in:** `e2eb3c72` (Task 2).

**2. [Rule 1 - Bug] stale "not yet available" dispatch test pointed at the now-wired install action**
- **Found during:** Task 2 (first `go test ./internal/agent/tools/`).
- **Issue:** `TestSkillDispatchErrors` asserted `action=install` returns "not yet available"; install is now wired, so it returned the install handler's "repo required" error.
- **Fix:** switched the test's reserved-action example to `restore` (still genuinely reserved); install/catalog are now live. The test still proves a reserved-but-unwired action errors with "not yet available".
- **Files modified:** internal/agent/tools/skill_test.go.
- **Committed in:** `e2eb3c72` (Task 2).

**Total deviations:** 2 auto-fixed (1 blocking gosec, 1 stale-test correction reflecting the now-wired action). No scope creep — the installer + catalog/install behavior is exactly as planned.

## Verification Evidence

- **Unit (race, WSL):** `go test -race ./internal/agent/tools/ ./internal/skills/ ./cmd/aura/` → **PASS** (all three packages, race-clean). The clone path RUNS via local `file://` fixtures (NOT skipped); the symlink tests run on Linux (Windows skips them — privilege).
- **Task 1 phase-gate command:** `go test -race -run 'TestInstall|TestCanonicalHash' ./internal/skills/` → **PASS** (race-clean, WSL).
- **`go vet ./...` → 0; `go build ./...` → exit 0; full `go test` (touched packages) → 0 failures.**
- **`golangci-lint run ./internal/agent/tools/... ./internal/skills/... ./cmd/aura/...` → 0 issues.**
- **Installer grep-clean:** `installer.go` contains `LookPath` (×5), `core.autocrlf=false` (×3), `GIT_TERMINAL_PROMPT=0` (×3); NO upstream computedHash CONSUME path (the only `computedHash` occurrences are a comment + the WRITE of our own lockfile key).
- **Boundary held:** `go list -deps ./internal/agent/tools | grep -c internal/skills` → **0** (cycle-free; the catalog + installer + writer + loader all adapt at the composition root).
- **Router wired:** `skill.go` registers `t.actionInstall` + `t.actionCatalog` (the 11-02 placeholders replaced); CLI has `case "install"`, `case "catalog"`, `case "always"`.
- **db_integration compiles + runs** (`TestInstallFullPipeline` under the tag): the full Install lands pending + ONE install audit row (pending tuple + canonical hash) + NO active dir.
- **All touched files ≤600 LOC** (largest: serve_adapters.go 413; installer.go 453).
- **No file deletions** in either commit.

## Known Stubs

None goal-blocking for 7d. `restore`/`archive` skill actions remain `notYetWired` (a future library-management plan). The Runner's auto-dispatch of a resolved install-approval resume to `skills.ResumeHandler` is the same integration 11-05 documented (the handler + the `aura skills approve` CLI both activate an installed pending skill — interface-first ordering, not a stub: the install→pending→approve→active contract is shipped + tested). The live `file://`-fixture clone is the no-network determinism floor; a real skills.sh-repo clone is available as the integration/E2E path.

## Threat Flags

None — no new security surface beyond the plan's `<threat_model>`. T-11-06-T1 (malicious third-party skill) is mitigated by the no-postinstall native clone (D-14) + the canonical-hash TOFU pin + the surfaced red flags + the always-strip; T-11-06-T2 (symlink escape) by the copy-boundary symlink-strip (tested, Linux); T-11-06-E1 (third-party always:true steers every turn) by the unconditional always-strip (tested); T-11-06-E2 (model self-installs+activates) by the pending+ask_user gate, no model approve (tested: install pauses, never activates); T-11-06-T3 (clone hangs on auth / runs shell) by LookPath fixed-argv + `--` + validateRepoURL + GIT_TERMINAL_PROMPT=0 + the install timeout. T-11-06-SC (git is the transport) is accepted — git is operator-provisioned, the cloned content IS gated (no [ASSUMED]/[SUS] package checkpoint, no npm/pip/cargo install in this plan).

## Next Phase Readiness

- The `Installer` + `CanonicalHash` + the catalog/install actions complete the discovery→install loop the North-Star xlsx E2E depends on (catalog → ask_user → install → approve → sandbox_exec).
- 11-07 (snippets) reuses the installer's materialized executable-under-/skills path + the `skill_ttl_sweep` cron TaskKind (admitted by the 0009 kind CHECK in 11-04).
- **CAP-07 is COMPLETE** — read (7a/11-02), validator (7b/11-03), write/edit (7c/11-04+11-05), install (7d/11-06) are all shipped; catalog default-ON, native clone (node dropped), audit-immutable trigger + D-29 matrix (migration 0010), NFKC validator + 10K fuzz are all live and verified. The traceability row is updated.

## Self-Check: PASSED

- FOUND: internal/skills/hash.go, hash_test.go, installer.go, installer_test.go, installer_integration_test.go
- FOUND: internal/agent/tools/skill_install.go, skill_install_test.go
- FOUND: commit `b52fdc5f` (Task 1)
- FOUND: commit `e2eb3c72` (Task 2)

---
*Phase: 11-skills*
*Completed: 2026-06-05*
