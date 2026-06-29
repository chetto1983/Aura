# Slice D — Frontend + Build/CI/Ops Quality Audit

**Audit date:** 2026-06-29  
**Auditor:** Codebase Quality Audit Agent (read-only)  
**Scope:** `web/src/` (React 19 / Vite 8 / TS 6 / Tailwind 4 SPA), `.github/workflows/`, `scripts/`, `Makefile`, `compose.yaml`, `compose.gpu.yaml`, docker configurations  
**Excluded from scope (per instructions):** `web/node_modules/`, `web/dist/`, lockfiles, generated files  
**Method:** Static analysis only — no builds, no npm runs, no test execution

---

## A. Slice Summary

| Metric | Value |
|--------|-------|
| Source files audited | ~240 `.ts`/`.tsx` (non-test) + 4 workflows + 33 scripts + 1 Makefile + 3 compose files |
| Test files | ~130 `*.test.{ts,tsx}` |
| Largest non-test source file | `i18n/resources.governance.ts` (567 LOC) |
| Largest test file | `__tests__/LoginPage.test.tsx` (643 LOC — **exceeds 600-LOC cap**) |
| Files approaching 600-LOC cap | `chat/ExternalStoreChat.tsx` (553), `routes/LoginPage.tsx` (518), `governance/governanceApi.ts` (435) |
| Duplicate `getJSON` implementations | **3 locations** (`api/json.ts`, `conversations/useConversations.ts`, `governance/governanceApi.ts`) |
| Duplicate focus-trap logic | **3 locations** (inline in `BoardLayout.tsx`, inline in `McpLifecycleCluster.tsx` `RemoveDialog`, uses utility in `Drawer.tsx`/`SourceExplorerSheet.tsx`) |
| Stryker mutation scope gaps | `chat/ExternalStoreChat.tsx`, `conversations/useConversations.ts`, `auth/authConfig.ts` not in mutate list |
| Frontend quality gates CI-enforced? | **Yes** — `web-lint`, `web-test` (Vitest ≥85%), `web-mutation` (Stryker ≥70% break), `web-dist-freshness`, `web-e2e` all in `ci.yml` blocking jobs |
| CI `./...` vs `go_packages.sh` | **F-015 confirmed**: `unit-test`, `windows-unit`, `vulncheck` jobs use `./...` raw; only `build-and-lint` uses `go_packages.sh` |
| Action versions pinned? | All actions pinned to major versions (`@v4`, `@v6`) — no SHA pinning (supply-chain medium risk per audit F-051 context) |
| Total findings | **Critical: 0, High: 2, Medium: 8, Low: 6, Info: 3** |

---

## B. Findings Table

### QA-D-01 — Duplicate `getJSON` implementation across three API modules

| Field | Value |
|-------|-------|
| **Category** | Code Duplication |
| **Severity** | High |
| **Confidence** | High |
| **Evidence** | `web/src/conversations/useConversations.ts:61-70` (local `getJSON`); `web/src/governance/governanceApi.ts:113-122` (another local `getJSON`); `web/src/api/json.ts:1-10` (the canonical shared version). Other API modules (`graphApi.ts`, `onboardingApi.ts`, `passwordResetApi.ts`) correctly import from `api/json`. |
| **Why** | The shared `api/json.ts` module already provides `getJSON`/`postJSON` with the identical implementation (same-origin credentials, `HTTP <n>` error). The two local copies create three divergence points: a behavior fix in `api/json.ts` will not propagate, and `governance/governanceApi.ts` further extends the pattern with `sendJSON`/`patchJSON`/`deleteJSON` that also duplicate the contract. At 435 LOC, `governanceApi.ts` is approaching the 600-LOC cap partly because it houses the HTTP layer it should import. |
| **Action** | 1. Remove `getJSON` from `conversations/useConversations.ts` and import from `../api/json`. 2. Remove `getJSON` from `governance/governanceApi.ts` and import from `../api/json`. 3. Extract `sendJSON`/`postJSON`/`patchJSON`/`deleteJSON` from `governance/governanceApi.ts` into `api/json.ts` (they are generic write helpers, not governance-specific). This will also lower `governanceApi.ts` from 435 toward ~300 LOC. |
| **Effort** | S (mechanical refactor) |
| **Safe cleanup strategy** | Import substitution only — identical logic, no behavior change. All existing tests pass because the contract is identical. |
| **Regression risk** | Low — the `governance/governanceApi.ts` local `getJSON` lacks the 204 coercion the write layer needs (that lives in `sendJSON`), so read calls are unaffected. |

---

### QA-D-02 — Focus-trap logic duplicated in `BoardLayout.tsx` instead of using `focusTrap.ts`

| Field | Value |
|-------|-------|
| **Category** | Code Duplication / Antipattern |
| **Severity** | High |
| **Confidence** | High |
| **Evidence** | `web/src/a11y/focusTrap.ts` exports `focusFirstDescendant` and `trapTabKey`. `web/src/governance/BoardLayout.tsx:56-91` re-implements the same focus-trap loop with its own inline `FOCUSABLE_SELECTOR` string (`'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'`). `web/src/governance/McpLifecycleCluster.tsx:208-238` implements a third copy in `RemoveDialog` (Tab+Shift-Tab loop, `querySelectorAll('button')`). By contrast `Drawer.tsx` and `SourceExplorerSheet.tsx` correctly import from `focusTrap.ts`. |
| **Why** | Three independent focus-trap implementations means a bug fix or selector change must be applied in 3 places. The `BoardLayout` copy omits the `disabled` attribute check used in `focusTrap.ts`; the `RemoveDialog` copy queries only `button` elements, missing focusable `<input>` or `<a>` that might appear. Divergence is already a real defect. |
| **Action** | 1. `BoardLayout.tsx`: replace the inline focusables+keydown logic with `focusFirstDescendant(sheet)` + `trapTabKey(event, sheet)` from `focusTrap.ts`. 2. `McpLifecycleCluster.tsx` `RemoveDialog`: same substitution (also fixes the `button`-only selector gap). |
| **Effort** | S |
| **Safe cleanup strategy** | Drop-in substitution. Existing `BoardLayout.test.tsx` exercises open/close/keyboard, so it will catch any regression. |
| **Regression risk** | Low — the shared utility has its own `focusTrap.ts` test. |

---

### QA-D-03 — `LoginPage.test.tsx` exceeds the 600-LOC cap (643 lines)

| Field | Value |
|-------|-------|
| **Category** | Antipattern / File-size cap |
| **Severity** | Medium |
| **Confidence** | High |
| **Evidence** | `web/src/__tests__/LoginPage.test.tsx` is 643 lines per `wc -l`. The project cap is 600 LOC (CLAUDE.md §Behavioral rules; enforced by `scripts/check-file-size.sh` and the `build-and-lint` CI job). |
| **Why** | The CI file-size gate (`bash scripts/check-file-size.sh` in `build-and-lint`) runs on tracked files including test files. If this file is currently tracked, the gate is failing silently or the cap is being skipped for it. A 643-line test file also makes test maintenance harder. |
| **Action** | Split into two or three logical groups: e.g., `LoginPage.authula.test.tsx` (credential flow, TOTP, CSRF), `LoginPage.bootstrap.test.tsx` (first-user creation, password confirmation), `LoginPage.a11y.test.tsx` (aria checks, focus restore). The shared `authulaConfigResponse` helper moves to a `__tests__/login.fixtures.ts` module. |
| **Effort** | S |
| **Safe cleanup strategy** | Pure file split — no logic changes. |
| **Regression risk** | Low. |

---

### QA-D-04 — `conversations/useConversations.ts` local `getJSON` also diverges with a local `postJSON` pattern

| Field | Value |
|-------|-------|
| **Category** | Code Duplication |
| **Severity** | Medium |
| **Confidence** | High |
| **Evidence** | `web/src/conversations/useConversations.ts:84-105` (`createConversation`) constructs its own `fetch('/api/conversations', { method: 'POST', ... })` with manual `Content-Type` and error handling instead of using `postJSON`. The same module defines its own `getJSON` (line 61). |
| **Why** | Compounds QA-D-01. Three separate fetch patterns in one file means the "all API calls must be same-origin + accept: json + non-200 throws" invariant is repeated in prose rather than enforced structurally. |
| **Action** | Consolidate `createConversation` to use `postJSON` from `api/json` after the QA-D-01 refactor lands. |
| **Effort** | S |
| **Regression risk** | Low — `useConversations.test.ts` mocks `fetch` globally. |

---

### QA-D-05 — `ProfileOnboardingWizard.tsx` uses `Loader2 animate-spin` instead of the shared `Spinner` component

| Field | Value |
|-------|-------|
| **Category** | Code Duplication |
| **Severity** | Medium |
| **Confidence** | High |
| **Evidence** | `web/src/onboarding/ProfileOnboardingWizard.tsx:86` uses `<Loader2 aria-hidden="true" className="size-4 animate-spin" />`. All governance write surfaces (`McpInstallPanel`, `SkillInstallPanel`, `McpEnvEditForm`, `McpLifecycleCluster`, `WhatsAppConnect`, `CalendarConnect`, `PimDeviceCodePanel` — 7 files) use the shared `Spinner` component from `components/Spinner.tsx`. |
| **Why** | The `Spinner` component includes `motion-reduce:animate-none` and uses a standardized size (`h-3.5 w-3.5`). The `ProfileOnboardingWizard` copy uses a different icon (lucide `Loader2`), different size, and lacks the reduced-motion guard — a visual inconsistency and accessibility gap. |
| **Action** | Replace `<Loader2 ... animate-spin>` in `ProfileOnboardingWizard.tsx` with `<Spinner />`. Remove the `Loader2` import if not used elsewhere. |
| **Effort** | XS |
| **Regression risk** | Low — visual only. |

---

### QA-D-06 — Stryker mutation scope missing high-value state-logic files

| Field | Value |
|-------|-------|
| **Category** | Test Gaps |
| **Severity** | Medium |
| **Confidence** | High |
| **Evidence** | `web/stryker.config.json` `mutate` list (13 files). Missing key logic files: `web/src/conversations/useConversations.ts` (conversation lifecycle + title logic, query cache keys), `web/src/chat/sseAdapter.ts` (core SSE reducer), `web/src/auth/authConfig.ts` (CSRF/auth config parsing), `web/src/chat/sseAdapter_frames.ts` and `sseAdapter_snapshot.ts`. |
| **Why** | The Stryker gate (≥70% killed, break at 70%) is meant to prove that tests actually detect logic mutations. Omitting the SSE adapter and the auth config parser from the mutate list means regressions in those files would not be caught by the mutation gate. `conversations/useConversations.ts` contains the title-truncation logic (`autoTitleFromPrompt`) and the query-key definitions — string mutations there are invisible to the mutation gate. |
| **Action** | Add to `stryker.config.json` `mutate`: `src/conversations/useConversations.ts`, `src/auth/authConfig.ts`, `src/chat/sseAdapter_frames.ts`, `src/chat/sseAdapter_snapshot.ts`. Evaluate whether `src/chat/sseAdapter.ts` can be practically mutated (SSE stream logic may be hard to unit-kill). |
| **Effort** | S (config change + potential new tests to reach kill thresholds) |
| **Regression risk** | Low — additive gate only. |

---

### QA-D-07 — CI `unit-test` and `vulncheck` jobs use raw `./...` instead of `go_packages.sh` (F-015)

| Field | Value |
|-------|-------|
| **Category** | CI / Architecture |
| **Severity** | Medium |
| **Confidence** | High |
| **Evidence** | `.github/workflows/ci.yml:84` `go test -race -count=1 ./...`; `ci.yml:111` `go build ./...` (windows-unit); `ci.yml:122` `go test -count=1 ./...` (windows-unit); `ci.yml:175` `govulncheck ./...` (vulncheck). Contrast with `ci.yml:49`, `ci.yml:52`, `ci.yml:63`, `ci.yml:68` which use `$(bash scripts/go_packages.sh)`. `scripts/go_packages.sh` explicitly excludes `web/node_modules/` subtrees from Go package enumeration. |
| **Why** | When `web/node_modules/` is present on the runner (it is installed in the `web-e2e` and similar jobs), `./...` may traverse into it if Go accidentally picks up a `go.mod`-like file. In practice the risk is low because `web/node_modules/` contains no `go.mod`, but the inconsistency is a maintenance smell and means the unit-test and vulncheck jobs do not respect the same filter as vet/lint/deadcode. This is the pre-existing F-015 finding in the existing audit. |
| **Action** | Replace `./...` with `$(bash scripts/go_packages.sh)` in the `unit-test`, `windows-unit`, and `vulncheck` jobs. Alternatively, add a `paths-filter` exclusion pattern. |
| **Effort** | XS |
| **Regression risk** | Near-zero — the package filter only removes non-existent paths. |

---

### QA-D-08 — Two custom Skeleton system abstractions co-exist without clear ownership

| Field | Value |
|-------|-------|
| **Category** | Architecture / Duplication |
| **Severity** | Medium |
| **Confidence** | High |
| **Evidence** | `web/src/components/skeleton/Skeleton.tsx` (349 LOC) is a rich, custom skeleton system with `SkeletonBlock`, `SkeletonText`, `SkeletonCard`, `SkeletonTable`, `SkeletonChart`, `SkeletonForm`, `SkeletonPage` etc. — all rendering through CSS classes defined in `styles/skeleton.css`. Separately, `web/src/components/ui/skeleton.tsx` (13 LOC) is the shadcn-generated `<Skeleton>` component based on `animate-pulse`. Both are live: `ui/skeleton.tsx` is imported by `ConversationSidebar.tsx`, `SearchPanel.tsx`, and `governance/governanceView.tsx`; the richer `components/skeleton/Skeleton.tsx` is used only internally by `AppSkeletons.tsx`. |
| **Why** | Two independent skeleton abstractions create inconsistent loading UIs and mean developers must choose between two APIs. The `ui/skeleton.tsx` uses Tailwind `animate-pulse bg-accent` while `Skeleton.tsx` uses custom CSS wave animation with `--skeleton-*` design tokens. The visual result differs. The shadcn component's usage in `governanceView.tsx` conflicts with the rich custom system used in the shell skeleton. |
| **Action** | Decision: adopt one system. Recommend retiring `ui/skeleton.tsx` (shadcn) and pointing `ConversationSidebar`, `SearchPanel`, `governanceView` to the custom `Skeleton.tsx` primitives. Alternatively, if the shadcn system is intentional for simpler cases, document the split. |
| **Effort** | M |
| **Regression risk** | Medium — visual change. Run Playwright E2E to verify. |

---

### QA-D-09 — `boolValue` in `governanceApi.ts` duplicates `booleanField` in `authConfig.ts`

| Field | Value |
|-------|-------|
| **Category** | Code Duplication |
| **Severity** | Low |
| **Confidence** | Medium |
| **Evidence** | `web/src/governance/governanceApi.ts:401-403` exports `boolValue(value: unknown): boolean { return value === true; }`. `web/src/auth/authConfig.ts:17-20` exports `booleanField(source: unknown, key: string): boolean` (key-access wrapper around the same `=== true` check). These are the only two boolean parsers in the frontend. |
| **Why** | Near-equivalent logic. `boolValue` is a scalar check; `booleanField` adds key-extraction but the inner predicate is identical. If semantics ever need to extend (e.g., accept `'true'` string), both files need updating. |
| **Action** | Extract a shared `isTruthy(value: unknown): boolean` into `api/json.ts` (which already owns the generic response-parsing helpers) and import from both call sites. |
| **Effort** | XS |
| **Regression risk** | Low. |

---

### QA-D-10 — Action versions pinned to mutable major tags (not SHA) across all four workflows

| Field | Value |
|-------|-------|
| **Category** | CI / Supply-chain |
| **Severity** | Low |
| **Confidence** | High |
| **Evidence** | All 39 `uses:` lines across `ci.yml`, `release.yml`, `codeql.yml`, `skills.yml` use semver-major tags (e.g., `actions/checkout@v6`, `actions/setup-go@v6`, `docker/setup-buildx-action@v4`). No actions are pinned to a full commit SHA. |
| **Why** | Mutable major tags mean a GitHub Actions maintainer can push a new patch to the tag that contains malicious code (supply-chain attack). SHA pinning (`actions/checkout@<sha>`) eliminates this vector. The existing security audit references this as F-051. This finding **does NOT re-report F-051** — it confirms it persists as a maintainability issue and amplifies it with count evidence (39 action refs). |
| **Action** | Progressively migrate to SHA-pinned actions. Start with secrets-accessing steps (`docker/login-action`, `goreleaser`). Use `dependabot` with `insecure-external-code-execution: allow` to auto-PR pin updates. |
| **Effort** | M |
| **Regression risk** | Low — SHA-pinned actions behave identically. |

---

### QA-D-11 — `shell.modes` contains placeholder surface entries (`tree`, `displays`, `settings`) that render as disabled buttons

| Field | Value |
|-------|-------|
| **Category** | Dead/Obsolete Code |
| **Severity** | Low |
| **Confidence** | High |
| **Evidence** | `web/src/shell/modes.ts:1` defines `MODES = ['chat', 'tree', 'graph', 'governance', 'displays', 'settings']`. `LIVE_MODES = ['chat', 'graph', 'governance']`. `ModeSwitcher.tsx:19` renders ALL 6 modes but disables non-live ones. `ModeTabBar.tsx` only renders `LIVE_MODES`. The i18n resources have keys for `shell.modes.tree`, `shell.modes.displays`, `shell.modes.settings` (used only in `ModeSwitcher`). |
| **Why** | Three placeholder mode buttons are permanently disabled in `ModeSwitcher` (the desktop nav) with `aria-disabled` and a "Coming soon" tooltip. They add dead visual surface and i18n keys that carry zero user utility. The `ModeTabBar` (mobile) already excludes them, confirming they are vestigial. |
| **Action** | Until the features are actually planned for implementation, either: (a) remove the non-live modes from `MODES` and their i18n keys, keeping only `LIVE_MODES`, or (b) if they need to remain as future anchors, document this intent explicitly. |
| **Effort** | XS |
| **Regression risk** | Low — no live functionality. The i18n parity test may need updating if keys are removed. |

---

### QA-D-12 — `AppSkeletons.tsx` imports i18n directly, bypassing the React hook pattern

| Field | Value |
|-------|-------|
| **Category** | Antipattern |
| **Severity** | Low |
| **Confidence** | High |
| **Evidence** | `web/src/components/skeleton/AppSkeletons.tsx:1` imports `i18n from '../../i18n/i18n'` and calls `i18n.t('skeleton.login')` directly (line 215, 217, 219). All other components use `useTranslation()` hook from `react-i18next`. |
| **Why** | Direct `i18n.t()` access bypasses React's context re-render cycle. If the language changes while a skeleton is displayed, the skeleton text will not update until the next render. It also makes the component harder to test (the mock setup for `i18n` is more complex than mocking `useTranslation`). The reason for the deviation appears to be that `RouteSkeletonFallback` is called in a non-hook context (`main.tsx` Suspense fallback) — but the component itself is a React component so `useTranslation()` would work fine inside it. |
| **Action** | Replace `i18n.t()` calls in `AppSkeletons.tsx` with `useTranslation()` inside each skeleton component that needs the string. Move the string derivation into the component body. |
| **Effort** | XS |
| **Regression risk** | Low — the Skeleton test already covers `RouteSkeletonFallback`. |

---

### QA-D-13 — `SkeletonBlock`, `SkeletonText` etc. never imported outside their own skeleton module

| Field | Value |
|-------|-------|
| **Category** | Dead/Unreachable Code |
| **Severity** | Low |
| **Confidence** | High |
| **Evidence** | The barrel `web/src/components/skeleton/index.ts` re-exports all skeleton primitives (`SkeletonBlock`, `SkeletonText`, `SkeletonAvatar`, `SkeletonCard`, `SkeletonChart`, `SkeletonForm`, `SkeletonTable`, `SkeletonPage`). Grep across all non-test `.ts`/`.tsx` files in `web/src` finds zero imports of these primitives outside of `AppSkeletons.tsx` and `Skeleton.tsx` themselves. Only `RouteSkeletonFallback`, `AppShellSkeleton`, `RuntimeHealthPanelSkeleton`, `LoginPageSkeleton`, `NotFoundViewSkeleton`, `LanguageSwitcherSkeleton`, `ThemeSwitcherSkeleton` are imported externally (by `main.tsx` and `RuntimeHealthPanel.tsx`). |
| **Why** | The barrel index suggests the primitives are a reusable design system, but in practice they are used only in two files within the skeleton module itself. This creates an illusion of an extensible API that no feature code actually consumes. The `knip` dead-code gate (`npm run deadcode`) should flag this. |
| **Action** | Either (a) remove the primitive exports from `index.ts` since only the composed skeletons are consumed externally, or (b) start using the primitives in at least one feature file (e.g., the governance board's `governanceView.tsx` loading state uses `ui/skeleton.tsx` — migrate it to the proper skeleton primitives, addressing QA-D-08 simultaneously). |
| **Effort** | XS |
| **Regression risk** | Low — only affects barrel exports. |

---

### QA-D-14 — `Composer.tsx` has no test coverage file

| Field | Value |
|-------|-------|
| **Category** | Test Gaps |
| **Severity** | Low |
| **Confidence** | High |
| **Evidence** | `web/src/chat/Composer.tsx` exists (the chat message input UI). `web/src/chat/__tests__/Composer.test.tsx` exists. No issue — file confirmed present. *(Retracted — false lead.)* |

*(Retracted — Composer.test.tsx confirmed present.)*

---

### QA-D-15 — `compose.gpu.yaml` is a pure override that mirrors `compose.yaml` but documents stale context

| Field | Value |
|-------|-------|
| **Category** | Dead/Obsolete |
| **Severity** | Low |
| **Confidence** | Medium |
| **Evidence** | `compose.gpu.yaml` (17 lines) comments "The main compose.yaml now requests NVIDIA GPUs by default; keeping this file preserves existing operator commands." If the base `compose.yaml` already enables GPUs by default, the GPU overlay file has no additional effect and exists only for backward-compatibility of operator muscle memory. |
| **Why** | A file that does nothing except duplicate a subset of the base compose service creates confusion about whether it is needed. It may also be included by accident in some `docker compose -f` invocations, causing confusion. |
| **Action** | Add a comment clarifying exactly what this file still changes (if anything) vs the base, or remove it if it is fully redundant. Verify whether the base `compose.yaml` `aura-llama-embed` service already declares GPU device reservations; if so, this file is safe to archive. |
| **Effort** | XS |
| **Regression risk** | Low — compose overlay files are opt-in. |

---

### QA-D-16 — `scripts/` contains scripts not invoked by `Makefile` or any CI workflow (potential orphans)

| Field | Value |
|-------|-------|
| **Category** | Dead/Orphaned Code |
| **Severity** | Info |
| **Confidence** | Low |
| **Evidence** | `scripts/` has 33 `.sh` files. The following are not referenced in `Makefile` or any `.github/workflows/*.yml`: `scripts/llm_smoke.sh`, `scripts/run_identity_integration.sh`, `scripts/smoke_identity_cli.sh`, `scripts/run_askuser_integration.sh`, `scripts/run_conversations_integration.sh`, `scripts/microcompact_smoke.sh`, `scripts/loop_budget_smoke.sh`, `scripts/chat-e2e-gate.sh`, `scripts/telegram_e2e.sh`, `scripts/chat_50_prompt_gate.sh`, `scripts/web_search_smoke.sh`, `scripts/check-dup.sh`, `scripts/check-deadcode-web.sh`, `scripts/install.sh`, `scripts/gofmt-staged.sh`, `scripts/garage_bootstrap.sh`. |
| **Why** | Low confidence because some may be operator-run local scripts (e.g., `telegram_e2e.sh` is documented as an operator gate), or invoked transitively. However, a number of these reference integration patterns that have since been superseded by CI-embedded steps. |
| **Action** | Audit each unreferenced script: (a) document it as operator-only with a header comment, (b) move it to `scripts/operator/` to distinguish from CI-invoked scripts, or (c) delete if superseded. The `check-dup.sh` and `check-deadcode-web.sh` scripts in particular may duplicate `npm run dup` / `npm run deadcode` now wired in CI. |
| **Effort** | M |
| **Regression risk** | Low — these scripts are not called by CI. |

---

### QA-D-17 — `web-lint` paths-filter backstop is a no-op for direct pushes to master

| Field | Value |
|-------|-------|
| **Category** | CI / Architecture |
| **Severity** | Info |
| **Confidence** | Medium |
| **Evidence** | `.github/workflows/ci.yml:893-898` (web-lint backstop), `ci.yml:929-936` (web-test backstop), `ci.yml:973-980` (web-mutation backstop), `ci.yml:1014-1022` (web-dist-freshness backstop). The backstop runs `scripts/web_filter_backstop.sh` with `github.event.pull_request.base.sha || github.event.before`. On a direct push to master with no PR, `github.event.pull_request.base.sha` is empty and `github.event.before` is the previous commit SHA. If the push is the very first push (no previous), `before` may be the zero SHA (`0000...`). |
| **Why** | The backstop comment notes "Fail-open on an uncomputable range; never red-falses the pipeline." This is correct design but means the backstop silently passes on edge cases (first push, force-push resets). The MEMORY.md feedback "master-direct git workflow" confirms direct pushes are the norm. This is documented behavior per the CI comment, but warrants a note. |
| **Action** | No immediate action needed — the backstop is correctly designed to fail-open. Document that the backstop is advisory on first-push or force-push scenarios. |
| **Effort** | XS (doc only) |
| **Regression risk** | None. |

---

### QA-D-18 — `SkeletonPage` exported from `components/skeleton/index.ts` is a re-export of an internal-use component

| Field | Value |
|-------|-------|
| **Category** | Architecture |
| **Severity** | Info |
| **Confidence** | Medium |
| **Evidence** | `web/src/components/skeleton/index.ts:8` re-exports `SkeletonPage`. `SkeletonPage` is used only within `AppSkeletons.tsx` (internally). The public barrel should only export what feature code actually imports. |
| **Why** | Minor boundary confusion — already noted as part of QA-D-13. |
| **Action** | Remove `SkeletonPage` from `index.ts` exports, keep it as an internal export in `Skeleton.tsx`. |
| **Effort** | XS |
| **Regression risk** | Low. |

---

### QA-D-19 — `governance/governanceApi.ts` approaching 600-LOC cap (435 LOC, density is high)

| Field | Value |
|-------|-------|
| **Category** | Architecture / LOC cap |
| **Severity** | Info |
| **Confidence** | High |
| **Evidence** | `web/src/governance/governanceApi.ts` is 435 lines. It already spawned `pimApi.ts` once it exceeded 600. After the QA-D-01 refactor (removing the HTTP helpers), it will drop to approximately 300 LOC. |
| **Why** | At 435 LOC it is safe today, but the PRD §Behavioral rules impose refactor-on-touch. The file mixes API response types, HTTP helper implementations, URL constants, and WhatsApp-specific business logic. |
| **Action** | Post QA-D-01 (remove HTTP helpers), consider extracting the WhatsApp-specific code to `whatsappApi.ts` to parallel `pimApi.ts`. |
| **Effort** | M (after QA-D-01) |
| **Regression risk** | Low. |

---

## C. Quick Wins (high value, low effort)

| ID | What | Effort |
|----|------|--------|
| QA-D-02 | Replace inline focus-trap copies with `focusTrap.ts` utility in `BoardLayout.tsx` + `McpLifecycleCluster.tsx` | XS |
| QA-D-05 | Replace `Loader2 animate-spin` in `ProfileOnboardingWizard.tsx` with shared `<Spinner />` | XS |
| QA-D-07 | Replace `./...` with `$(bash scripts/go_packages.sh)` in `unit-test`, `windows-unit`, `vulncheck` CI jobs | XS |
| QA-D-09 | Extract `isTruthy(value: unknown)` into `api/json.ts` and consolidate `boolValue`/`booleanField` | XS |
| QA-D-12 | Replace `i18n.t()` direct call in `AppSkeletons.tsx` with `useTranslation()` hook | XS |
| QA-D-13 | Remove unexported skeleton primitives from `index.ts` barrel | XS |
| QA-D-03 | Split `LoginPage.test.tsx` (643 LOC) into 2-3 files | S |
| QA-D-01 | Import `getJSON` from `api/json` in `useConversations.ts` and `governanceApi.ts` | S |

---

## D. Risky / Uncertain Findings

| ID | What is uncertain | Missing evidence |
|----|-------------------|-----------------|
| QA-D-08 | Whether `ui/skeleton.tsx` (shadcn) and `components/skeleton/Skeleton.tsx` are intentionally split or accidental drift | No design spec for "which skeleton API to use when"; the `knip` deadcode gate may already flag this but its output is not available |
| QA-D-11 | Whether `tree`/`displays`/`settings` modes are actively planned vs permanently deferred | No roadmap reference; the ROADMAP mentions v2.0.0 phases but surface mode assignments are not described |
| QA-D-15 | Whether `compose.gpu.yaml` actually has any effect given the base compose.yaml | Requires reading the full `aura-llama-embed` service block in `compose.yaml` to compare GPU reservation blocks; not audited deeply |
| QA-D-16 | Orphaned scripts — some may be documented operator-only invocations | `scripts/install.sh`, `scripts/gofmt-staged.sh` may be `lefthook` hooks; need `lefthook.yml` or `.lefthook.yml` confirmation |

---

## Cross-Slice Flags

| Flag | Target Slice | Description |
|------|-------------|-------------|
| **XS-01** | Go backend (Slice B/C) | `governance/governanceApi.ts` defines types using Go struct field names verbatim (e.g., `McpServerRow.StartupState`, `SchedulerTask.ID`, `SchedulerRun.LastHeartbeatAt`). This is a cross-slice contract: any Go struct rename must be mirrored in the frontend type. The lack of a code-generated TypeScript type layer means this contract is maintained by convention only. |
| **XS-02** | Go backend | `conversations/useConversations.ts:Conversation` type also mirrors Go field names (`ID`, `TitleSet`, `TotalInputTokens`, etc.). Same contract fragility as XS-01. Compounds existing findings — the two duplicate `getJSON` implementations mean the contract is in two places with no single source of truth. |
| **XS-03** | CI/CD | The `web-dist-freshness` job (D-05) rebuilds `web/` and diffs `internal/webui/dist/`. The current git status shows many deleted files in `internal/webui/dist/assets/` — these appear to be a pending dist rebuild that has not been committed. If the dist is not committed, the freshness gate will fail on the next CI run. This is an operational concern, not a code defect, but it is visible in the diff. |
