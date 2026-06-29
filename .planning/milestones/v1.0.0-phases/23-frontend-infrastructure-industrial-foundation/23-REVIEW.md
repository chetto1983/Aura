---
phase: 23-frontend-infrastructure-industrial-foundation
reviewed: 2026-06-16T00:00:00Z
depth: standard
files_reviewed: 21
files_reviewed_list:
  - .github/workflows/ci.yml
  - Makefile
  - cmd/aura/serve.go
  - cmd/aura/serve_webui.go
  - cmd/aura/serve_webui_test.go
  - docker/aura/Dockerfile
  - internal/webui/doc.go
  - internal/webui/embed.go
  - internal/webui/embed_test.go
  - scripts/web_dist_freshness.sh
  - web/e2e/shell.spec.ts
  - web/eslint.config.js
  - web/index.html
  - web/playwright.config.ts
  - web/src/AppShell.tsx
  - web/src/ErrorBoundary.tsx
  - web/src/main.tsx
  - web/src/theme/applyTheme.ts
  - web/src/theme/density.ts
  - web/tokens/generate-theme.mjs
  - web/vite.config.ts
findings:
  critical: 0
  warning: 6
  info: 5
  total: 11
status: issues_found
---

# Phase 23: Code Review Report

**Reviewed:** 2026-06-16T00:00:00Z
**Depth:** standard
**Files Reviewed:** 21
**Status:** issues_found

## Summary

Reviewed the hand-written Phase-23 surface: the Go serve mount + embed + tests, the
`web_dist_freshness.sh` gate, the CI YAML (web jobs + e2e provisioning), the Dockerfile
`webbuild` stage, and the TS/React source. I traced the additive `/` mount through the
two-mux precedence interaction, the `aura serve` boot path under the PG-only e2e stack,
and the theme-before-paint dual-source script.

No BLOCKER-class defects. The mount precedence is correct (AG-UI prefixes stay
authoritative; wrong-method AG-UI requests 405 rather than leaking the SPA; the static
tree is path-traversal-safe via `fs.Sub`). The boot path holds: `bootServeChatEnv`
hard-requires only Postgres, so the Neo4j-less e2e job boots, and `/healthz` (not
`/readyz`) is correctly chosen as the Playwright readiness URL.

The defects are robustness/maintainability traps rather than incorrect behavior today:
a generated theme-snippet that is never consumed (dual source-of-truth drift), a theme
path that reads localStorage in one place and ignores it in the other, a documented
leaf-boundary invariant that has no enforcing CI gate, a latent `.gitattributes`
binary-corruption hazard, and a no-skip-as-green gap in the new web CI jobs that the rest
of the pipeline is otherwise rigorous about.

## Warnings

### WR-01: Generated `head-snippet.generated.html` is never consumed — dual source of truth for the theme-before-paint script

**File:** `web/tokens/generate-theme.mjs:43-56`, `web/index.html:10-21`, `web/src/styles/head-snippet.generated.html:1-13`, `web/README.md:30-41`
**Issue:** `generate-theme.mjs` emits `src/styles/head-snippet.generated.html` containing
the exact theme-before-paint IIFE, and the README states the generator "emits ... the
inline head snippet." But nothing injects that generated file into `index.html` — the
build is `generate-theme.mjs && tsc -b && vite build`, with no copy/transform step that
splices the snippet in. `index.html` carries its own hand-maintained duplicate of the
same script. The generated file's own header comment ("keep localStorage keys in sync
with src/theme/applyTheme.ts") admits the manual-sync hazard. Result: three independent
copies of the keys/defaults (`aura.theme`/`aura.density`, `dark`/`operator`) — the inline
`index.html` script, the generated (dead) snippet, and `applyTheme.ts`/`density.ts` — with
no gate keeping them aligned. A token change to `$meta.defaultDensity` regenerates the dead
snippet but silently leaves `index.html` stale, breaking the no-flash contract without any
test or lint catching it. The freshness gate only diffs `internal/webui/dist/`, not these
source files.
**Fix:** Either (a) make the generated snippet load-bearing — have a Vite `transformIndexHtml`
hook (or a small build step) inject `head-snippet.generated.html` into `index.html` so the
generator is the single source — or (b) delete the dead `head-snippet.generated.html` output
+ its README/prettierignore/eslint-ignore references and add a CI assertion that the
`index.html` inline keys/defaults equal `tokens.json $meta` (e.g. grep the four literals).
Do not keep an unconsumed generated artifact that the README advertises as authoritative.

### WR-02: `applyTheme()`/`getTheme()` ignores `localStorage['aura.theme']` while the inline boot script reads it — divergent theme resolution

**File:** `web/src/theme/applyTheme.ts:22-30`, `web/index.html:14`
**Issue:** The inline boot script sets `data-theme` from
`localStorage.getItem('aura.theme') || 'dark'`. After hydration, `main.tsx` calls
`applyTheme()`, which calls `getTheme()` — and `getTheme()` hard-returns `DEFAULT_THEME`
('dark'), ignoring localStorage entirely. So the two theme paths use different resolution
rules: the boot script honors a stored `aura.theme`, the runtime path overwrites it back to
'dark'. Today this is masked because only 'dark' exists (`type Theme = 'dark'`), but the
asymmetry is a latent bug: the moment a second theme is added, a stored value would paint
correctly pre-hydration and then flip back to dark on mount (a guaranteed flash + a broken
preference). The density path does NOT have this bug — `getDensity()` correctly reads
storage and validates via `isDensity`. The theme path should mirror it.
**Fix:** Make `getTheme()` read+validate storage symmetrically with `getDensity()` (read
`THEME_STORAGE_KEY`, validate against the known theme set, fall back to `DEFAULT_THEME`),
even while only 'dark' exists — so the inline script and the runtime path resolve identically.
Until a real second theme lands, this keeps the two code paths from silently disagreeing.

### WR-03: `doc.go`/`serve_webui.go` claim `agui_boundary_check.sh` keeps `internal/webui` leaf — but the script never checks `internal/webui`

**File:** `internal/webui/doc.go:13-15`, `cmd/aura/serve_webui.go:16-17`, `scripts/agui_boundary_check.sh:18-24`
**Issue:** Both the package doc and the serve-mount header assert that `internal/webui`
stays leaf-level "so `scripts/agui_boundary_check.sh` stays green." That script only checks
one closure: `go list -deps ./internal/agent/...` must not contain `internal/agui`. It does
not inspect `internal/webui` at all. `internal/webui` IS leaf today (verified:
`go list -deps ./internal/webui/...` yields only itself among internal packages), but that
is upheld by convention, not by any gate. A future edit adding `import
".../internal/agui"` (or `internal/agent`) to `webui` would compile, pass CI, and silently
violate the stated D-17 leaf invariant. The phase context lists "imports only stdlib" as a
MUST invariant — a MUST invariant with no automated enforcement is a regression waiting to
happen.
**Fix:** Add a closure assertion to `agui_boundary_check.sh` (or a sibling gate) that fails
if `go list -deps ./internal/webui/...` contains any `github.com/chetto1983/aura/internal/`
package, then correct the doc comments to reference the gate that actually enforces it. One
extra `grep -q` line wired into the existing CI step.

### WR-04: Broad `web/** text eol=lf` + `internal/webui/dist/** text eol=lf` will corrupt any non-png binary asset the Vite build emits

**File:** `.gitattributes:16-31`
**Issue:** `web/** text eol=lf` and `internal/webui/dist/** text eol=lf` force EOL
normalization on every matched path. The only binary-exclusion overrides are an explicit
extension list (`*.png`, `*.jpg`, `*.gif`, `*.ico`, `*.pdf`, `*.gz`, `*.zip`, ...). The
committed dist today contains only `.png`/`.svg`/text, so it is safe right now. But the
SPA is greenfield and growing: the first time Vite emits a `.woff2`/`.woff` font, a
`.wasm` chunk, a `.webp`/`.avif` image, or a `.mp3`/`.ogg` asset — none of which are in the
binary override list — Git will LF-normalize it, corrupting the bytes on checkout AND
producing a perpetual phantom diff that reds the byte-exact `web-dist-freshness` gate
(`git diff --exit-code -- internal/webui/dist/`). This is exactly the "CRLF round-trip
forges a stale-dist diff" failure the comment on line 13-15 says it is guarding against,
but the guard is incomplete.
**Fix:** Replace the over-broad `text eol=lf` globs with a binary-default posture for the
asset directories, or extend the binary override list to the full set Vite can emit
(`*.woff`, `*.woff2`, `*.ttf`, `*.eot`, `*.wasm`, `*.webp`, `*.avif`, `*.mp3`, `*.ogg`,
`*.wav`, `*.mp4`, `*.webm`). Safest: add `internal/webui/dist/**/*.{woff,woff2,ttf,wasm,webp,avif} binary`
(and the web/public equivalents) so a new asset type cannot silently corrupt or red the gate.

### WR-05: New web CI jobs lack the no-skip-as-green guard the rest of the pipeline enforces — a `dorny/paths-filter` misfire goes green having tested nothing

**File:** `.github/workflows/ci.yml:664-752` (web-lint, web-test, web-dist-freshness)
**Issue:** CLAUDE.md's no-skip-as-green rule is enforced everywhere else (db/web/knowledge/
telegram tiers `t.Fatal` under `$CI`; the cache gate runs a negative test). The three
path-filtered web jobs (`web-lint`, `web-test`, `web-dist-freshness`) instead gate every
real step on `if: steps.changes.outputs.web == 'true'`. When the filter evaluates false,
the job reports SUCCESS having installed nothing and run nothing — there is no positive
assertion that a web-touching PR actually exercised lint/test/freshness. This is the precise
falsely-green shape the project guards against: if `dorny/paths-filter` ever misfires (a
glob edit, a base-ref quirk, a rename that dodges `web/**`), a web change merges with its
quality gates silently skipped and the job still green. The Go side avoids this by making the
skip a hard failure under CI; the web jobs have no equivalent backstop.
**Fix:** Add a cheap always-green floor to each web job that runs even when the filter is
false (mirroring the Go "compile floor" pattern) — e.g. an unconditional final step that
asserts the filter ran and, when web==true, that the gated steps actually executed (a sentinel
file / step-output check). At minimum, document why these three jobs are exempt from
no-skip-as-green and add a periodic full run (e.g. on `push` to master ignoring the filter)
so the gates cannot rot undetected.

### WR-06: `web-e2e` boots `aura serve` against a Neo4j-less stack with a degraded LLM key but has no assertion that the daemon actually came up healthy before Playwright

**File:** `.github/workflows/ci.yml:753-833`, `web/playwright.config.ts:40-46`
**Issue:** The e2e job provisions PG only (no `neo4j-up`) and a dummy
`OPENROUTER_API_KEY`. Boot tolerates this (verified: `bootServeChatEnv` hard-requires only
`db.Open`; Neo4j is opened solely behind the default-off `AURA_LLM_REASONING_LEARNING`), and
`/healthz` is correctly chosen as the webServer readiness URL (PG-only liveness, not the
Neo4j-touching `/readyz`). The risk is the failure mode: the ONLY thing standing between
"daemon failed to boot" and a green job is Playwright's 60s `webServer.timeout`. If
`buildRegistryWithMCP` ever starts hard-requiring a sidecar at boot (memory MCP /
mcp-neo4j-cypher), the webServer would hang and the job would fail with an opaque Playwright
timeout rather than a named boot error, and there is no step that curls `/healthz` /
`/readyz` independently to surface the real cause. The job also relies on the daemon
defaulting to `127.0.0.1:9080` (it never exports `AURA_AGUI_BIND`); a future change to the
`AGUIBind` default silently breaks the hardcoded `baseURL`/`url` in `playwright.config.ts`
with no shared constant tying them.
**Fix:** Add an explicit `curl --fail --retry` against `127.0.0.1:9080/healthz` as a CI step
before `npm run test:e2e` so a boot failure reports a named, fast error instead of an opaque
60s Playwright timeout; and either export `AURA_AGUI_BIND` in the job env (single source) or
add a comment tying `playwright.config.ts`'s `:9080` to the `config.go` default so the
coupling is visible.

## Info

### IN-01: `aguiRoutePrefixes` duplicates the AG-UI route list with no compile-time link to `agui.Server.Mux`

**File:** `cmd/aura/serve_webui.go:32-39`, `internal/agui/server.go:92-97`
**Issue:** The owned-route prefix list (`/healthz`, `/readyz`, `/debug/vars`, `/metrics`,
`/agent/run`, `/threads/`) is a hand-maintained copy of the routes `agui.Server` registers on
its own mux. The two lists are not linked: if the AG-UI server adds a route (say `/agent/cancel`
in Phase 24/25), the operator must remember to add it to `aguiRoutePrefixes` or that route falls
through to the SPA catch-all and 404s/renders the shell instead of reaching AG-UI. The
`serve_webui_test.go` precedence test only covers the currently-listed prefixes, so a missed
prefix would not be caught.
**Fix:** Have `agui.Server` expose its top-level route prefixes (e.g. an exported
`RoutePrefixes()` or a package var) and have `newServeHandler` register from that single source,
so adding an AG-UI route automatically keeps it authoritative over `/`. Low priority for the
static-placeholder phase; revisit when WEB-01 adds the SPA fallback that makes a missed prefix
actively harmful.

### IN-02: `web-dist-freshness.sh` diff scope cannot catch a stale generated theme source

**File:** `scripts/web_dist_freshness.sh:24-27`
**Issue:** The script runs the full `npm run build` (which regenerates `src/styles/theme.css`
and `src/styles/head-snippet.generated.html`) but then `git diff --exit-code` only on
`internal/webui/dist/`. That is correct for the dist tamper-evidence goal, but it means a
hand-edited or stale committed `src/styles/theme.css` (which `vite build` consumes) is invisible
to this gate — the gate would pass as long as the final dist bytes match, masking a divergence
between `tokens.json` and the committed generated CSS until someone reads the source.
**Fix:** Either broaden the diff to also cover `web/src/styles/theme.css` (+
`head-snippet.generated.html` if it is kept per WR-01), or accept the scope and note in the
script header that generated *source* drift is intentionally out of scope (only dist bytes are
gated). A one-line `git diff --exit-code -- web/src/styles/theme.css` addition closes it.

### IN-03: `ErrorBoundary` discards the caught error entirely — no `componentDidCatch`, no console trace

**File:** `web/src/ErrorBoundary.tsx:14-20`
**Issue:** `getDerivedStateFromError` swaps in the themed fallback but the boundary never
implements `componentDidCatch`, so the actual error object is dropped on the floor — not even
`console.error`'d. The comment justifies skipping *telemetry* (no backend sink yet, D-13), which
is reasonable, but losing the error from the devtools console too makes a production
white-screen→fallback transition undiagnosable for an operator. React itself logs uncaught render
errors in dev, but in the production embed build that re-throw logging is stripped.
**Fix:** Add a minimal `componentDidCatch(error, info) { console.error('Aura render error', error, info); }`
so the operator's browser console retains the trace, without adding any backend dependency. Keep
the no-telemetry posture; just stop discarding the error locally.

### IN-04: `web-test` Vitest job runs `--coverage` but no coverage threshold is enforced (FND-06 floor not gated)

**File:** `.github/workflows/ci.yml:717-719`, `web/vitest.config.ts:10-15`, `web/package.json:14`
**Issue:** `npm run test` is `vitest run --coverage`, and the vitest config sets a v8 provider
with include/exclude — but no `coverage.thresholds` are configured, so coverage is *measured and
discarded*. The Go side enforces an 85% owned-surface floor; the web unit lane collects a v8
report that nothing asserts on, so web coverage can silently rot to near-zero while the job stays
green. Given CLAUDE.md's explicit ≥85% floor philosophy, an unenforced web coverage number is a
gap, not a gate.
**Fix:** Add `coverage.thresholds` to `vitest.config.ts` (e.g. lines/functions/statements at the
project floor) so the web-test job fails below the bar, bringing the web lane to parity with the
Go coverage gate. Choose a floor consistent with the project's 85% posture or document the
deliberate web-specific number.

### IN-05: `AppShell` brand `<img>` points at `/logo.png` with no decoding/loading hints and a single accessibility-name source

**File:** `web/src/AppShell.tsx:7`, `web/e2e/shell.spec.ts:29-30`
**Issue:** The e2e brand assertion (`getByRole('img', { name: /aura/i })`) passes on the `alt`
attribute regardless of whether `/logo.png` actually loads, so a 404 on the embedded `logo.png`
(e.g. a future dist path change) would not fail the brand test — the visual brand could silently
break while CI stays green. Minor: the `<img>` also lacks `decoding="async"`/`loading` hints,
trivial polish for an above-the-fold brand mark.
**Fix:** Strengthen the brand contract with a network-level assertion that `/logo.png` returns 200
(Playwright `page.waitForResponse` or a `request.get`), so a broken embedded asset reds the gate
rather than passing on `alt` text alone. Optionally add `decoding="async"`.

---

_Reviewed: 2026-06-16T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
