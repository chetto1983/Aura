---
phase: 32-quality-cleanup-dead-code-shared-helpers
plan: 04
subsystem: settings
tags: [settings-allowlist, env-overlay, react-i18next, vite-dist, compose, sidecar, dead-code, refactor]

# Dependency graph
requires:
  - phase: 32-01
    provides: "QUAL-02 dead-code triage baseline (sidecar-owned vs in-process verdict for AURA_MEMORY_EMBED_*)"
  - phase: 15-memory
    provides: "agent-memory sidecar that consumes AURA_MEMORY_EMBED_* from compose/.env at container start"
provides:
  - "settings.AllowedKeys without the two sidecar-owned AURA_MEMORY_EMBED_* keys; OverlayEnv silently ignores stale rows (no migration)"
  - "Frontend SettingsKey union + BACKEND_SETTINGS array + en/it i18n trimmed of the two keys (parity test green)"
  - "compose.yaml + .env.example document AURA_MEMORY_EMBED_* as sidecar-owned compose/.env vars (not cockpit-overridable)"
  - "internal/webui/dist rebuilt + committed (i18n-chunk hash cascade) so web-dist-freshness stays green"
affects: [32-08, settings, web, compose]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Allowlist-guarded env overlay: removing a key from settings.AllowedKeys auto-retires any stale aura.settings DB row (the `if _, ok := AllowedKeys[r.Key]; !ok { continue }` guard) — no migration"
    - "Symmetric en+it i18n key removal to keep the resources.parity.test.ts gate green (Pitfall 6)"
    - "Vite content-hash cascade: editing a shared i18n resources chunk renames every translated component chunk; vendor chunks (shiki/assistant-ui/utils) stay byte-identical (toolchain-parity proof)"

key-files:
  created: []
  modified:
    - internal/settings/settings.go
    - internal/settings/settings_test.go
    - web/src/settings/ModelSettingsPanel.tsx
    - web/src/i18n/resources.settings.ts
    - compose.yaml
    - .env.example
    - internal/webui/dist
  deleted: []

key-decisions:
  - "D-05 confirmed remove-from-Go + document: the two keys were a silent no-op cockpit overlay (the daemon never reads them; compose maps them to the sidecar at container START, unreachable by OverlayEnv os.Setenv). Removed in-process, kept + documented as sidecar-owned compose/.env vars."
  - "No DB migration: OverlayEnv's allowlist guard already ignores unlisted rows, so a stale aura.settings row is a silent no-op. Migration would be cosmetic-only and was prohibited."
  - "i18n keys live in the modular split web/src/i18n/resources.settings.ts (imported by resources.ts), not resources.ts directly — edited the real source file (see Deviations)."

patterns-established:
  - "Removal regression test: assert both keys absent from AllowedKeys AND that OverlayEnv silently skips a stale row for a now-unlisted key (proves the no-migration retirement path)."
  - "Secret-guard invariant test: secret.IsSecretEnvKey stays true for the removed *_API_KEY/*_BASE_URL names and other *_KEY/*_SECRET keys — redaction is independent of the settings allowlist (B-09 superset)."

requirements-completed: []  # QUAL-02 intentionally NOT marked complete here — left to the orchestrator/verifier. See Next Phase Readiness: 32-04 is the LAST QUAL-02 dead-code item.

coverage:
  - id: T1
    description: "Both AURA_MEMORY_EMBED_* keys removed from settings.AllowedKeys; OverlayEnv silently skips a stale row for a now-unlisted key; secret.IsSecretEnvKey redaction intact; no migration; no value logged."
    requirement: "QUAL-02"
    verification:
      - kind: unit
        ref: "internal/settings/settings_test.go#TestMemoryEmbedKeysRemovedFromAllowlist"
        status: pass
      - kind: unit
        ref: "internal/settings/settings_test.go#TestOverlayEnvSkipsStaleRemovedKey"
        status: pass
      - kind: unit
        ref: "internal/settings/settings_test.go#TestSecretRedactionPredicateUnaffected"
        status: pass
      - kind: unit
        ref: "go test -race ./internal/settings/ (green)"
        status: pass
      - kind: other
        ref: "grep -n 'AURA_MEMORY_EMBED' internal/settings/settings.go (empty)"
        status: pass
    human_judgment: false
  - id: T2
    description: "Keys removed from SettingsKey union + BACKEND_SETTINGS + en/it i18n (parity green); compose.yaml + .env.example document sidecar ownership; internal/webui/dist rebuilt + committed with no residual drift."
    requirement: "QUAL-02"
    verification:
      - kind: unit
        ref: "web/src/i18n/__tests__/resources.parity.test.ts (en ↔ it parity, green)"
        status: pass
      - kind: integration
        ref: "cd web && npm test (108 files / 914 tests passed) && npm run lint (0 warnings) && npm run build (exit 0)"
        status: pass
      - kind: other
        ref: "grep -rn 'AURA_MEMORY_EMBED|memoryEmbed' web/src (empty)"
        status: pass
      - kind: other
        ref: "git status --porcelain internal/webui/dist after commit (empty — no residual drift; second build byte-identical)"
        status: pass
    human_judgment: false

# Metrics
duration: ~32min
completed: 2026-06-30
status: complete
---

# Phase 32 Plan 04: AURA_MEMORY_EMBED_* Full-Stack Removal Summary

**Removed the two sidecar-owned `AURA_MEMORY_EMBED_*` cockpit settings keys — a silent no-op overlay the daemon never read — across Go (AllowedKeys), the React settings panel + en/it i18n, and the compose/.env docs, then rebuilt `internal/webui/dist`; the real sidecar compose/.env variables and the Secret-redaction guard are preserved.**

## Performance

- **Duration:** ~32 min (including a dist-determinism + toolchain-parity investigation)
- **Completed:** 2026-06-30
- **Tasks:** 2 (both `tdd="true"`)
- **Files modified:** 7 (Go settings.go + settings_test.go; web ModelSettingsPanel.tsx + resources.settings.ts; compose.yaml; .env.example; internal/webui/dist bundle)

## Accomplishments

- **Go (Task 1):** Deleted the `AURA_MEMORY_EMBED_BASE_URL` and `AURA_MEMORY_EMBED_API_KEY` entries from `settings.AllowedKeys`. `OverlayEnv`'s allowlist guard (`if _, ok := AllowedKeys[r.Key]; !ok { continue }`) means any stale `aura.settings` row for the retired keys is silently skipped — **no migration**. The API key value (`Secret:true`) is never logged on removal, and `secret.IsSecretEnvKey` redaction is untouched.
- **Tests (Task 1):** Added three assertions — both keys absent from `AllowedKeys`/`Allowed()`; `OverlayEnv` is a no-op for a stale row whose key is no longer allowlisted (proves the no-migration retirement); and `secret.IsSecretEnvKey` stays `true` for the removed names + other `*_KEY`/`*_SECRET` keys (redaction is independent of the allowlist). RED confirmed before removal, GREEN after.
- **Frontend (Task 2):** Removed the two keys from the `SettingsKey` union and the `BACKEND_SETTINGS` array in `ModelSettingsPanel.tsx`, and removed `memoryEmbedBaseUrl`/`memoryEmbedKey` from **both** `en` and `it` in `resources.settings.ts` (symmetric — keeps `resources.parity.test.ts` green). `npm test` = 914/914 pass, `npm run lint` = 0 warnings.
- **Docs (Task 2):** Documented in `compose.yaml` (the env block that maps these to the sidecar's `OPENAI_BASE_URL`/`OPENAI_API_KEY` at container start) and added a commented section in `.env.example` that `AURA_MEMORY_EMBED_*` remain valid sidecar-owned compose/.env variables, **not** runtime-overridable via the cockpit (the daemon's `os.Setenv` overlay cannot reach an already-running sidecar).
- **Dist (Task 2):** Rebuilt `internal/webui/dist` and committed it (Pitfall 3) so `web-dist-freshness` stays green.

## Task Commits

Each task committed atomically (D-11 split), via direct `git commit` with explicit pathspec (the `gsd` wrapper times out on the file-size hook; a concurrent Codex session was committing to master throughout):

1. **Task 1: Go settings allowlist removal + tests** — `71b64a97` (refactor)
2. **Task 2: web + i18n + compose/.env docs + dist rebuild** — `03587cd0` (refactor)

## Files Created/Modified

- `internal/settings/settings.go` — two `AURA_MEMORY_EMBED_*` entries removed from `AllowedKeys`.
- `internal/settings/settings_test.go` — `TestMemoryEmbedKeysRemovedFromAllowlist`, `TestOverlayEnvSkipsStaleRemovedKey`, `TestSecretRedactionPredicateUnaffected` added (imports `internal/secret`).
- `web/src/settings/ModelSettingsPanel.tsx` — keys removed from `SettingsKey` union and `BACKEND_SETTINGS`.
- `web/src/i18n/resources.settings.ts` — `memoryEmbedBaseUrl`/`memoryEmbedKey` removed from `en` and `it`.
- `compose.yaml` — sidecar-ownership comment added to the agent-memory env block.
- `.env.example` — commented sidecar-owned section documenting the two vars (not cockpit-overridable).
- `internal/webui/dist` — rebuilt bundle (1 ModelSettingsPanel chunk + 28 i18n-cascade chunk renames + index.html + sw.js).

## Decisions Made

- **D-05 (remove + document):** confirmed the keys are sidecar-owned no-ops in-process and removed them from Go while preserving + documenting the compose/.env variables. The sidecar continues to read them at container start.
- **No migration:** prohibited and unnecessary — the allowlist guard auto-ignores stale rows.
- **Real i18n source file:** the keys live in the modular `resources.settings.ts` (imported by `resources.ts`), so that is the file edited (see Deviations).

## Deviations from Plan

### Deviations

**1. [Rule 3 — file-reference correction] i18n keys edited in `resources.settings.ts`, not `resources.ts`**
- **Found during:** Task 2 (frontend i18n removal)
- **Issue:** The plan's `files_modified` and Removal Map name `web/src/i18n/resources.ts`, but the `settings.fields.memoryEmbed*` keys actually live in the modular split `web/src/i18n/resources.settings.ts` (which `resources.ts` imports and composes). `resources.ts` itself contains no settings field keys.
- **Fix:** Edited `web/src/i18n/resources.settings.ts` (the real source of the keys), removing both keys symmetrically from `en` and `it`. `resources.ts` needed no change.
- **Verification:** `resources.parity.test.ts` green; `grep -rn 'memoryEmbed' web/src` empty; full `npm test` 914/914.
- **Committed in:** `03587cd0` (Task 2).

---

**Total deviations:** 1 (file-reference correction — the modular i18n split moved the keys to `resources.settings.ts`).
**Impact on plan:** None on scope or behavior; the keys are removed symmetrically from both locales exactly as required, in the project's actual i18n source file.

## TDD Gate Compliance

Both tasks are `tdd="true"`. Task 1 followed RED→GREEN explicitly: the three new assertions failed before the `AllowedKeys` edit (`go test ./internal/settings/` FAIL on the two removal tests; the secret-guard test passed as a true invariant), then passed after removal (`go test -race ./internal/settings/` GREEN). Both tasks are behaviour-preserving cleanups (removing a no-op control), committed as `refactor(...)` commits pairing the change with its regression guard. There is no observable runtime behaviour change to capture beyond "the no-op overlay is gone and stale rows are ignored", which the OverlayEnv-skip test pins.

## Issues Encountered

- **Dist hash churn (~29 chunks) investigated before committing.** A 2-key i18n removal renamed nearly all app-chunk hashes, which looked like possible cross-platform (node-version) divergence. Verified it is the legitimate, deterministic **Vite content-hash cascade**: editing the shared i18n `resources.settings.ts` chunk changes the chunk every `useTranslation` component imports, rippling new hashes — while the pure-vendor chunks (`shiki-*`, `assistant-ui-*`, `utils-*`) stayed **byte-identical** to the committed bundle, proving toolchain parity with whatever built the committed dist (and thus with CI). Two consecutive builds produced byte-identical output (deterministic), and `git status --porcelain internal/webui/dist` is empty after commit (no residual drift). The local WSL node is v26 vs CI's pinned `24.x`, but the byte-identical vendor chunks confirm the bundler output matches — `web-dist-freshness` will rebuild to the same hashes.
- **Parallel Codex session active on master throughout.** Both commits used explicit pathspec `git commit -m … -- <files>` and were verified with `git show --stat HEAD` to contain only declared files; the concurrent session's staged `cmd/aura/*`, `internal/assets/*`, `internal/documents/*`, `internal/db/*`, `internal/agui/document*` work was never staged or touched. The whole-module `go vet ./...` pre-commit hook was polled for a green window before each commit (the session's `internal/documents/embedding_jobs_test.go` was transiently red between bursts — not fixed, not bypassed).
- **`.env.example` access:** initially permission-blocked for the Read/Grep tools; edited after the user lifted the restriction mid-run. No `--no-verify`, no value of the secret key logged anywhere.

## User Setup Required

None — no external service configuration required. Operators who previously set the (no-op) cockpit fields are unaffected: stale `aura.settings` rows are silently ignored, and the sidecar still reads `AURA_MEMORY_EMBED_*` from compose/.env.

## Next Phase Readiness

- **QUAL-02 is now fully resolved across its dead-code items** for the verifier: `assets.Status` (32-01), `agui.indexByte`/`stringList` + RequestID re-stamp (32-02), telebot blank import + discarded `Build()` (32-03), and the sidecar-only `AURA_MEMORY_EMBED_*` keys (this plan, 32-04). The `requirements.mark-complete` call is intentionally left to the orchestrator/verifier.
- Wave 1 of Phase 32 is complete (32-01..04). **32-05** (Wave 2 — QUAL-03 leaf extractions: `internal/neostore` + `internal/db` numeric + `internal/envutil`) is next.

## Self-Check: PASSED

- FOUND: internal/settings/settings.go (AllowedKeys minus 2 keys)
- FOUND: internal/settings/settings_test.go (3 new tests)
- FOUND: web/src/settings/ModelSettingsPanel.tsx (union + array trimmed)
- FOUND: web/src/i18n/resources.settings.ts (en+it trimmed)
- FOUND: compose.yaml (sidecar-ownership comment)
- FOUND: .env.example (sidecar-owned section)
- FOUND: internal/webui/dist (rebuilt, no residual drift)
- FOUND: .planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-04-SUMMARY.md
- FOUND commit: 71b64a97 (Task 1 — Go settings allowlist removal)
- FOUND commit: 03587cd0 (Task 2 — web + i18n + docs + dist)

---
*Phase: 32-quality-cleanup-dead-code-shared-helpers*
*Completed: 2026-06-30*
