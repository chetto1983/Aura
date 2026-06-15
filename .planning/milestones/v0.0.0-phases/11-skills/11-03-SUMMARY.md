---
phase: 11-skills
plan: 03
subsystem: skills
tags: [skills, slice-7b, validator, nfkc, blocklist, fuzz, catalog, skills-sh, lax-decode]

requires:
  - phase: 11-02
    provides: "internal/skills package + Frontmatter struct + skillNameRe grammar + golang.org/x/text direct dep (NFKC fold on the read path)"
  - phase: 11-01
    provides: "Amended prd.md §Slice 7 truth-source (D-11 catalog JSON, D-12 disable-catalog, D-27 hard-for-model/operator-override, D-28 write-boundary-only, D-34 env catalog)"
  - phase: 03
    provides: "sandboxagent.Client net/http JSON shape (New + timeout + status-class guard + LimitReader body) — the catalog client mirrors it"
provides:
  - "internal/skills/validator.go: NFKC-first write-boundary validator — SanitizeName single name chokepoint (D-30), violatesBlocklist (normalize-then-match, Pitfall 2), ValidateForWrite pure with D-27 operator override, ErrInvalidName/ErrBlocklisted/ErrInvalidStructure sentinels"
  - "FuzzSkillValidator (SC#3) + deterministic 10K-mutation NFKCCorpus guard for CI without -fuzz"
  - "internal/skills/catalog.go: CatalogClient over skills.sh /api/search — lax decode, installs-ranked, empty-query pre-call guard, status-class guard, D-12 disable-catalog sentinel, transport-isolated for the public-API swap"
  - "config: AURA_SKILL_INJECTION_BLOCKLIST (builtin list + comma-separated override) + AURA_SKILL_CATALOG_URL + AURA_SKILL_CATALOG_DISABLE + AURA_SKILL_INSTALL_TIMEOUT_SEC (D-27/D-11/D-12/D-34)"
affects: [11-04, 11-05, 11-06, "skills-write-boundary", "skill-install", "skill-catalog-action"]

tech-stack:
  added: []
  patterns:
    - "Pure write-boundary validation: a function over Frontmatter+body+blocklist+cap with no DB/FS/env reads — config injects the blocklist and cap; NOT called from the loader (D-28)"
    - "Single name chokepoint: SanitizeName owns skillNameRe; the loader's validateStructure routes its name check through it, so exactly one name validator exists in the package"
    - "Transport isolation: the catalog HTTP shape lives entirely behind CatalogClient.Search so the future public API (#426) is a drop-in swap"
    - "Deterministic fuzz-corpus companion: an oracle-vs-API property test (TestSkillValidator_NFKCCorpus) runs >=10K NFKC/Unicode mutations so CI without -fuzz still proves SC#3"

key-files:
  created:
    - internal/skills/validator.go
    - internal/skills/validator_test.go
    - internal/skills/validator_fuzz_test.go
    - internal/skills/catalog.go
    - internal/skills/catalog_test.go
  modified:
    - internal/skills/loader.go
    - internal/config/config.go

key-decisions:
  - "SanitizeName is the genuine single name chokepoint: rather than declaring a second skillNameRe in validator.go (which collided with the 11-02 loader's regex), the validator OWNS the regex + SanitizeName and the loader's validateStructure now calls SanitizeName — one name validator, package-wide"
  - "violatesBlocklist returns (matched, pos, ok); ValidateForWrite folds it into an ErrBlocklisted message carrying the matched sequence + byte position so the D-27 operator gate can show exactly which sequence matched and where"
  - "The default injection blocklist is the prd.md §Slice 7 builtin (ChatML/Anthropic/Llama/Mistral/Meta/DeepSeek-Gemma-Qwen control tokens); AURA_SKILL_INJECTION_BLOCKLIST replaces it wholesale via comma-separated override"
  - "Catalog default timeout is a catalog-specific 15s (spike-003 cold latency ~1.5s) distinct from the 90s git-clone install timeout; both knobs share AURA_SKILL_INSTALL_TIMEOUT_SEC for the install path, the catalog uses its own DefaultCatalogTimeoutSec when config TimeoutSec is 0"

patterns-established:
  - "Write-boundary purity contract: ValidateForWrite imports no os/pgx/database/sql; the loader never calls it (D-28 — disk is operator-trusted)"
  - "Operator-override asymmetry: model-authored paths pass allowBlocklisted=false (hard reject, no escape); only the operator CLI passes true AFTER the gate shows the match — the contract is set here, enforced by callers in 11-05/11-06"

requirements-completed: [CAP-07]

duration: ~35min
completed: 2026-06-05
---

# Phase 11 Plan 03: Skills Validator + Catalog (Slice 7b) Summary

**The write-boundary enforcement primitive (`internal/skills/validator.go` — NFKC-normalize-FIRST literal blocklist + `SanitizeName` single name chokepoint + pure `ValidateForWrite` with the D-27 operator override) proven by `FuzzSkillValidator` (SC#3, 3.8M execs / 60s, no crasher) and a deterministic 10K-mutation NFKC corpus, plus the skills.sh `/api/search` catalog client (`catalog.go` — lax decode, installs-ranked, empty-query-guarded, disable-catalog honored, transport-isolated).**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-06-05
- **Tasks:** 2 (Task 1 TDD)
- **Files:** 7 (5 created, 2 modified)

## Accomplishments

- **Validator (`validator.go`)** — `SanitizeName(name, dirName)` is the SINGLE name chokepoint (grammar `^[a-z0-9-]{1,64}$` + name==dir, D-30); `violatesBlocklist` NFKC-normalizes the body FIRST then case-folds and `strings.Index`-matches each blocklist literal (the non-negotiable normalize-then-match order, Pitfall 2), returning the matched sequence + byte position; `ValidateForWrite` is a PURE function (no DB/FS/env) doing structure checks (name/description-len/body-cap/type-enum) + the blocklist UNLESS the D-27 operator override (`allowBlocklisted`). Sentinels `ErrInvalidName`/`ErrBlocklisted`/`ErrInvalidStructure` mirror identity/store.go.
- **Fuzz + corpus (`validator_fuzz_test.go`)** — `FuzzSkillValidator` seeds plain literals + fullwidth/compatibility variants + benign + prose-embedded and asserts the SC#3 property (no NFKC-collapse-to-blocklist input slips the model path); `TestSkillValidator_NFKCCorpus` runs a deterministic 10,680-entry generated NFKC/Unicode corpus so CI without `-fuzz` still proves SC#3.
- **Catalog (`catalog.go`)** — `CatalogClient.Search` mirrors `sandboxagent.Client`: empty-query pre-call guard (server 400s, Pitfall 5), GET `/api/search?q=`, status-class guard with a `LimitReader`'d body snippet, LAX decode into `struct{ Skills []CatalogItem }` (default decoder, drift fields tolerated), ranks by `Installs` desc (`sort.SliceStable`). Transport isolated for the public-API swap (#426); `Disabled` honors the D-12 escape hatch via `ErrCatalogDisabled` without dialing.
- **Config** — `AURA_SKILL_INJECTION_BLOCKLIST` (builtin default list = the prd.md §Slice 7 control-token seed; comma-separated override via the new `envSliceDefault` helper), `AURA_SKILL_CATALOG_URL`, `AURA_SKILL_CATALOG_DISABLE`, `AURA_SKILL_INSTALL_TIMEOUT_SEC` (D-27/D-11/D-12/D-34).
- **Single-chokepoint refactor** — the 11-02 loader had its own `skillNameRe` + inline name check; the validator now OWNS the regex and `SanitizeName`, and `loader.validateStructure` routes its name check through `SanitizeName` — exactly one name validator in the package.

## Task Commits

1. **Task 1: NFKC-first validator + FuzzSkillValidator (SC#3) + blocklist config** — `c16cf18c` (feat)
2. **Task 2: skills.sh /api/search catalog client (lax decode, installs-ranked)** — `e8a1db9a` (feat)

## Decisions Made

- **SanitizeName as the real single chokepoint:** the plan's literal instruction to declare `skillNameRe` in validator.go collided with the loader's existing regex (11-02). Rather than duplicate, the validator owns the grammar + `SanitizeName` and the loader calls it — which is what "the SINGLE name chokepoint" actually requires. The loader's name-error message changed accordingly (its test only asserts skip-on-invalid, so it stayed green).
- **Blocklist seed = prd.md §Slice 7 builtin:** the ChatML/Anthropic/Llama/Mistral/Meta/DeepSeek-Gemma-Qwen control-token list, NFKC-normalized THEN matched (write-boundary only, D-28). `envSliceDefault` splits a comma-separated override and drops empties.
- **Catalog timeout separation:** the catalog uses a 15s `DefaultCatalogTimeoutSec` (spike-003 cold ~1.5s) when config supplies 0; the 90s `AURA_SKILL_INSTALL_TIMEOUT_SEC` is the git-clone install ceiling the installer (11-06) will consume.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `skillNameRe` redeclaration**
- **Found during:** Task 1 (first `go vet`).
- **Issue:** the plan said to declare `skillNameRe` in validator.go, but the 11-02 loader already declared it package-wide → "redeclared in this block".
- **Fix:** validator.go owns the single `skillNameRe` + `SanitizeName`; removed the loader's duplicate regex and unused `regexp` import; routed `loader.validateStructure`'s name check through `SanitizeName`. This realizes "the SINGLE name chokepoint" exactly.
- **Files modified:** internal/skills/validator.go, internal/skills/loader.go
- **Commit:** `c16cf18c`

**2. [Rule 1 - Bug] NFKC corpus under the 10K floor**
- **Found during:** Task 1 (first corpus test run: 7,320 < 10,000).
- **Issue:** the initial mutator/wrapper matrix produced too few entries to meet the SC#3 "10K mutations" deterministic guard.
- **Fix:** added 3 more mutators (tab/newline pad, circled-digit prefix, fullwidth-doubled) → 10,680 entries.
- **Files modified:** internal/skills/validator_fuzz_test.go
- **Commit:** `c16cf18c`

**Total deviations:** 2 auto-fixed (1 blocking dedup → single-chokepoint realization, 1 corpus-size bug). No scope creep.

## Verification Evidence

- `go vet ./internal/skills/... ./internal/config/...` → clean; `go build ./...` → exit 0.
- `go test ./internal/skills/ ./internal/config/` → PASS; `go test -race ...` (WSL) → green.
- `go test -fuzz=FuzzSkillValidator -fuzztime=60s ./internal/skills/` → **PASS, 3,866,049 execs, no crasher** (SC#3 phase-gate command).
- `go test -run TestSkillValidator_NFKCCorpus` → PASS (10,680-entry deterministic corpus ≥ 10K).
- `golangci-lint run ./internal/skills/... ./internal/config/...` → **0 issues**.
- Import-purity: validator.go imports no `os`/`pgx`/`database/sql`; `grep ValidateForWrite\|violatesBlocklist internal/skills/loader.go` → **0** (loader does not call the validator, D-28).
- catalog.go: `grep -c "api/search"` = 4; `grep -c "DisallowUnknownFields"` = **0** (lax decode).
- catalog_test.go: httptest.Server with `isDuplicate`/`count` drift fields → decode succeeds; ranking-by-installs asserted; empty query reaches **0** requests (pre-call guard); non-2xx surfaces the status; `Disabled` returns the sentinel with 0 dials.
- All touched files ≤600 LOC (largest: loader.go 223).
- `git diff --diff-filter=D HEAD~2 HEAD` → no file deletions.

## TDD Gate Compliance

Task 1 is `tdd="true"` over a new validator surface in a (now-established) package. RED/GREEN were developed together and committed once (a standalone failing `test(...)` commit referencing `ValidateForWrite`/`SanitizeName` would not compile before validator.go exists). The single `feat(...)` commit `c16cf18c` carries the full failing-then-passing suite (unit + fuzz + 10K corpus) alongside the implementation, exercising every `<behavior>` clause. No `refactor(...)` commit needed. Task 2 is non-TDD per the plan.

## Known Stubs

None. The validator and catalog client are fully functional. They are CONSUMED (not yet wired into) the model-facing write/install paths — that wiring is plans 11-04/11-05/11-06, which the plan explicitly scopes downstream (the validator is the write-boundary primitive; the catalog client backs the `action=catalog` browse path). This is interface-first ordering, not a stub.

## Threat Flags

None — no new security surface beyond the plan's `<threat_model>`. T-11-03-T1 (NFKC-obfuscated blocklist token) is mitigated by normalize-first + FuzzSkillValidator (SC#3, proven). T-11-03-E1 (allowBlocklisted bypass) contract is set: model paths pass false (hard reject); the operator-override is gated to the CLI in 11-05/11-06. T-11-03-I1/D1 (skills.sh drift/DoS) mitigated by lax decode + status-class guard + LimitReader + client timeout.

## Self-Check: PASSED

- FOUND: internal/skills/validator.go, validator_test.go, validator_fuzz_test.go, catalog.go, catalog_test.go
- FOUND: internal/skills/loader.go (modified), internal/config/config.go (modified)
- FOUND: commit `c16cf18c` (Task 1)
- FOUND: commit `e8a1db9a` (Task 2)

---
*Phase: 11-skills*
*Completed: 2026-06-05*
