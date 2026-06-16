---
phase: 23-frontend-infrastructure-industrial-foundation
fixed_at: 2026-06-16T00:00:00Z
review_path: .planning/phases/23-frontend-infrastructure-industrial-foundation/23-REVIEW.md
iteration: 1
findings_in_scope: 6
fixed: 6
skipped: 0
status: all_fixed
---

# Phase 23: Code Review Fix Report

**Fixed at:** 2026-06-16
**Source review:** `.planning/phases/23-frontend-infrastructure-industrial-foundation/23-REVIEW.md`
**Iteration:** 1
**Scope:** Critical + Warning (the 6 `WR-*` warnings). The 5 `IN-*` INFO findings were
explicitly out of scope this pass and were **not touched**.

**Summary:**

- Findings in scope: 6
- Fixed: 6
- Skipped: 0
- Status: `all_fixed`

All 6 warnings were fixed. Every fix kept the Phase-23 foundation green: after each
commit the relevant gate (Go build/vet/test + `agui_boundary_check.sh` +
`check-file-size.sh`, or web `lint`/`format:check`/`typecheck`/`test`, or `ci.yml`
YAML-parse) was re-run and stayed green. The byte-locked test contracts
(`AppShell.test.tsx`, `shell.spec.ts`) and the inline+synchronous no-flash theme
script in `web/index.html` were preserved unchanged.

## Per-finding result

| ID | Status | Commit | What changed | Gate re-verified green |
|----|--------|--------|--------------|------------------------|
| WR-01 | fixed | `34db9662` | Collapsed the theme-snippet to a single source: stopped the generator emitting the unconsumed `head-snippet.generated.html`, deleted the dead file (+ `.prettierignore`/README refs), and added a generator **drift assertion** that fails `npm run build` if `index.html`'s inline script no longer matches `tokens.json $meta`. Inline script stays INLINE+SYNCHRONOUS; `index.html` byte-unchanged; dist unaffected (snippet was never bundled). | web `lint`+`typecheck`+`test`; generator pass + negative drift test (exit 1 on divergence, 0 restored); `npm run build` → dist unchanged |
| WR-02 | fixed | `e48a109c` | `getTheme()` now reads+validates `localStorage['aura.theme']` against a known-theme set (`THEMES`) with a `DEFAULT_THEME` fallback, mirroring `getDensity()`, so the inline boot script and the runtime path resolve theme identically. Rebuilt the committed embed dist (new bundle hash); inline pre-paint theme script preserved. | web `lint`+`typecheck`+`test`; dist `index.html` still carries the inline theme script before the main module |
| WR-03 | fixed | `29765185` | Added a real dependency-closure gate to `scripts/agui_boundary_check.sh`: fails if `go list -deps ./internal/webui/...` contains any `internal/*` package but `webui` itself (leaf invariant, FND-02). Corrected the `doc.go`/`serve_webui.go` comments to reference the gate that now actually enforces it. | `go build`/`vet`/webui+cmd tests; `agui_boundary_check.sh` exit 0; `check-file-size.sh` |
| WR-04 | fixed | `5f65c991` | Appended `*.woff *.woff2 *.ttf *.otf *.eot *.webp *.avif *.wasm` to the `.gitattributes` Binary-assets block (after the `text eol=lf` globs so they override), so a future Vite/Workbox font/wasm/modern-image asset cannot be LF-corrupted or forge a stale-dist diff. Verified `git check-attr text -- web/anything.woff2` → `text: unset` (-text), also under `internal/webui/dist/`. | `git check-attr` for all 8 new types under `web/` + dist |
| WR-05 | fixed | `661ffa21` | Added `scripts/web_filter_backstop.sh` and wired it as a guard step (`if web!=true`) into all 3 path-filtered web jobs (web-lint, web-test, web-dist-freshness): it cross-checks `git diff` for the event range and **fails** if a `web/**` or `internal/webui/dist/**` path actually changed while the filter said false (a misfired filter merging gates-skipped). Fail-OPEN on an uncomputable range (shallow clone / first-push all-zero BEFORE), fail-CLOSED only on a verified web-path diff. | `ci.yml` YAML-parse; script tested: fail-open on empty/zero base, clean for a non-web range (WR-04 commit), fails for a web range (WR-02 commit) |
| WR-06 | fixed | `cc4ae739` | Introduced a single `SERVE_ORIGIN` constant in `web/playwright.config.ts` with a comment tracing `:9080` to `config.go`'s `AGUIBind` default (asserted in `config_test.go`), used for both `baseURL` and the `/healthz` readiness url; added a matching note in the web-e2e job. The independent pre-Playwright `/healthz` curl was deliberately **not** added — see remainder below. | web `lint`+`format:check`+`typecheck`+`test`; `ci.yml` YAML-parse |

## Fixed Issues (detail)

### WR-01: Generated `head-snippet.generated.html` never consumed — dual source of truth

**Files modified:** `web/tokens/generate-theme.mjs`, `web/.prettierignore`, `web/README.md`, deleted `web/src/styles/head-snippet.generated.html`
**Commit:** `34db9662`
**Applied fix:** Took the safe variant from the guidance (in-place `index.html`
rewriting was rejected to keep the no-flash script byte-stable). `tokens.json` is now
the single source: the generator no longer emits the unconsumed snippet, and instead
asserts the hand-maintained inline script in `index.html` still contains the tokens'
default theme/density literals — the build fails on drift. The inline script remains
INLINE+SYNCHRONOUS in `index.html` (it cannot be bundled), and `index.html` is
byte-unchanged. The committed dist is unaffected because the snippet was never bundled.
Verified the negative path: editing the inline default makes the generator exit 1.

### WR-02: `getTheme()` ignored `localStorage['aura.theme']`

**Files modified:** `web/src/theme/applyTheme.ts`, `internal/webui/dist/` (rebuilt)
**Commit:** `e48a109c`
**Applied fix:** Mirrored `getDensity()` — `getTheme()` reads `THEME_STORAGE_KEY`,
validates against a `THEMES` set, falls back to `DEFAULT_THEME`. This keeps the
pre-paint inline script and the runtime path resolving identically even while only
`'dark'` exists. Rebuilt the committed embed dist (the bundle hash changed); confirmed
the rebuilt `dist/index.html` still carries the inline `data-theme`/`data-density`
script before the main module.

### WR-03: Documented leaf invariant had no enforcing gate

**Files modified:** `scripts/agui_boundary_check.sh`, `internal/webui/doc.go`, `cmd/aura/serve_webui.go`
**Commit:** `29765185`
**Applied fix:** Added a second closure assertion to the boundary script (the webui
closure must contain no `internal/*` package but itself), mirroring the existing
exit-code/echo contract. Corrected the two doc comments to reference the gate that now
actually enforces the invariant. Script still exits 0 today.

### WR-04: `.gitattributes` binary guards incomplete

**Files modified:** `.gitattributes`
**Commit:** `5f65c991`
**Applied fix:** Extended the Binary-assets block with the 8 asset types Vite/Workbox
may emit. Verified via `git check-attr` that all 8 now resolve to `-text` both under
`web/` and `internal/webui/dist/`, overriding the broad `text eol=lf` globs.

### WR-05: Web CI jobs lacked the no-skip-as-green backstop

**Files modified:** `scripts/web_filter_backstop.sh` (new), `.github/workflows/ci.yml`
**Commit:** `661ffa21`
**Applied fix:** New backstop script wired as a guard step (`if web!=true`) into all
three filtered web jobs. It is deliberately fail-open on an uncomputable range so it
can never red-false the pipeline, fail-closed only on a confirmed web-path change in a
trustworthy range. Tested locally against real commits: clean for a `.gitattributes`-only
range, fails for a `web/`-touching range, fail-open on empty/all-zero base.

### WR-06: web-e2e `:9080` untied to the AG-UI default

**Files modified:** `web/playwright.config.ts`, `.github/workflows/ci.yml`
**Commit:** `cc4ae739`
**Applied fix:** Applied only the safe part of the guidance. A single `SERVE_ORIGIN`
constant now backs both `baseURL` and the `/healthz` readiness url, with a comment
tracing `:9080` to `config.go`'s `AGUIBind` default (asserted in `config_test.go`); a
matching note was added to the web-e2e job. The independent pre-Playwright `/healthz`
curl was **not** added — see remainder.

## Non-actionable remainder (documented, not forced)

- **WR-06 — independent `/healthz` curl before Playwright:** Not added. `aura serve` is
  booted *by* Playwright's `webServer` (which already gates on `url: .../healthz` with a
  60s timeout), so the daemon does not exist before Playwright starts it — a separate
  pre-Playwright curl is not cleanly possible from the job. The webServer's existing
  `/healthz` url gate remains the boot-readiness signal. If a future change makes
  `buildRegistryWithMCP` hard-require a sidecar at boot, the right follow-up is to split
  the daemon boot out of the `webServer` command and add a dedicated `curl --fail --retry`
  step — tracked here as a Phase-24+ follow-up rather than forced now.

## Notes

- The 5 INFO findings (IN-01 .. IN-05) were intentionally left untouched (out of scope).
- During WR-06 a parallel session interleaved a commit (`eabd5954`, the calendar-PIM-MCP
  design spec) onto the branch; the pre-commit hook initially swept its untracked file
  into the deletion set. The WR-06 commit was amended to restore that file, so the
  parallel session's work was preserved (verified present in the HEAD tree).

---

_Fixed: 2026-06-16_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
