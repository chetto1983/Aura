# Coding Conventions

**Analysis Date:** 2026-08-02

## Naming Patterns

**Files:**
- Name Go files in lowercase `snake_case` and split large responsibilities by concern: `internal/agent/llm_agent_retry.go`, `internal/agent/llm_agent_stream_retry.go`, and `cmd/aura/serve_memory_readiness.go` are representative. Keep every owned `.go`, `.ts`, and `.tsx` file at or below 600 lines; `scripts/check-file-size.sh` enforces the cap and recommends `<name>_<concern>.{go,ts,tsx}` splits.
- Name Go tests `<subject>_test.go`; use explicit suffixes such as `_integration_test.go`, `_property_test.go`, `_fuzz_test.go`, `_bench_test.go`, and `_live_test.go` when the tier matters. Put a build constraint on line 1 of tagged tests, as in `internal/db/tx_integration_test.go`.
- Name React component files and their exported components in PascalCase, for example `web/src/onboarding/OnboardingWizard.tsx` and `web/src/graph/GraphExplorer.tsx`. Name non-component TypeScript modules in camelCase, for example `web/src/chat/durationFormat.ts` and `web/src/governance/governanceApi.ts`.
- Name frontend unit tests `*.test.ts` or `*.test.tsx`, either beside the implementation (`web/src/api/json.test.ts`) or in a feature `__tests__/` directory (`web/src/governance/__tests__/governanceApi.test.ts`). Name browser tests `*.spec.ts` under `web/e2e/`.
- Name Python release-gate tests `scripts/<gate>_test.py` and shell self-tests `scripts/<gate>_test.sh`.

**Functions:**
- Use PascalCase for exported Go functions and camelCase for package-private functions. Constructors use `New<Type>` (`internal/arcadedb/embedding.go`) and context/functional options use `With<Concern>` (`internal/db/tx.go`, `internal/sandbox/usersandbox/docker_backend.go`).
- Use `Test<Subject>_<Case>`, `Benchmark<Subject>`, and `Fuzz<Subject>` for Go test entry points. Use named `t.Run` cases for table-driven tests, as demonstrated by `TestSanitizeName` in `internal/skills/validator_test.go`.
- Use camelCase for TypeScript functions and PascalCase for React components. Hooks must start with `use`, such as `web/src/approvals/useThreadApprovals.ts`. API methods should be verb-led and state their domain, such as `createPimAccount` in `web/src/governance/pimApi.ts`.
- Use `snake_case` for Python functions and `test_<behavior>` for `unittest` methods, as in `scripts/critical_mutation_gate_test.py`.

**Variables:**
- Keep Go locals short only when their scope is obvious (`ctx`, `err`, `tx`, `q`); use descriptive names at package and API boundaries. Exported sentinel errors use `Err<Condition>`, for example `ErrBlocklisted` in `internal/skills/validator.go`.
- Use lower camelCase for TypeScript values, SCREAMING_SNAKE_CASE for fixed module constants (`SERVE_ENV_KEYS` in `web/playwright.config.ts`), and `as const` for closed literal sets (`THEMES` in `web/src/theme/applyTheme.ts`).
- Prefix repository-owned environment variables with `AURA_`; third-party variables retain upstream names. Tests reference the exact variable names through helpers such as `envOrSkip` in `internal/db/db_test.go`.

**Types:**
- Use PascalCase for Go structs and interfaces. Keep small dependency interfaces close to the consumer, such as `StoreBackend` in `internal/assets/service.go` and `Runner` in `internal/agui/server.go`; verify important implementations with compile-time assignments such as `var _ llm.Client = (*FakeClient)(nil)` in `internal/agent/agenttest/fakeclient.go`.
- Use PascalCase for TypeScript interfaces and type aliases. Mark component props and immutable collections `readonly` where mutation is not part of the contract, as in `SourcesButton` tests at `web/src/chat/displays/__tests__/SourcesButton.test.tsx`.
- Represent closed domains with typed string unions and constants rather than free-form strings, as in `GraphOp` in `web/src/graph/types.ts` and `Density` in `web/src/theme/density.ts`.

## Code Style

**Formatting:**
- Treat `gofmt` as mandatory. `.golangci.yml` enables the `gofmt` formatter, `lefthook.yml` formats staged Go files through `scripts/gofmt-staged.sh`, and `.editorconfig` requires tabs for Go and Makefiles.
- Use UTF-8, LF endings, a final newline, and trimmed trailing whitespace from `.editorconfig`; Markdown alone preserves trailing whitespace for hard line breaks.
- Format `web/` with Prettier using single quotes, semicolons, trailing commas, a 100-column print width, and two-space indentation from `web/.prettierrc.json`.
- Do not hand-format generated code in `internal/db/sqlc/` or built assets in `internal/webui/dist/`; their generators and freshness gates own those files.

**Linting:**
- Run the Go packages selected by `scripts/go_packages.sh`; it deliberately excludes Go examples inside `web/node_modules`. Use `make vet`, `make lint`, and `make quality` from `Makefile`.
- Follow `.golangci.yml`: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `misspell`, `gosec`, `revive`, `dupl`, and `modernize` are enabled with warnings treated as failures in CI. Production duplication is rejected at 100 tokens; `_test.go` is exempt because test tables are intentionally repetitive.
- Fix lint findings instead of suppressing them. If a suppression is unavoidable, scope `//nolint:<linter>` to the specific linter and include the non-obvious justification; `.golangci.yml` contains narrow, documented path/rule exclusions.
- Run `npm run lint`, `npm run typecheck`, and `npm run format:check` in `web/`. `web/eslint.config.js` uses strict and stylistic type-aware TypeScript rules, React Hooks rules, JSX accessibility rules, import ordering, and Prettier compatibility. `web/tsconfig.json` enables `strict`, unused checks, `noUncheckedIndexedAccess`, and `exactOptionalPropertyTypes`.
- Keep both languages free of dead code and copy/paste clones. Go uses `deadcode` and `dupl`; TypeScript uses `web/knip.json` and `web/.jscpd.json` through `scripts/check-deadcode-web.sh` and `scripts/check-dup.sh`.
- Preserve the 600-line cap enforced for both source and tests by `scripts/check-file-size.sh`; split by cohesive concern before extending an overgrown file.

## Import Organization

**Order:**
1. Go standard-library imports.
2. One blank line, then module and third-party imports, formatted by `gofmt`/`goimports`; see `internal/db/tx_integration_test.go`.
3. In TypeScript, builtin/external packages first, project alias imports next, then parent/sibling/index imports. `web/eslint.config.js` enforces that group order without blank lines and reports ordering as a warning; CI's zero-warning command makes it blocking.
4. Use inline type imports (`type Page`, `type ComponentProps`) when values and types share an import, and `import type` for type-only dependencies; examples are in `web/playwright.config.ts` and `web/src/chat/displays/__tests__/SourcesButton.test.tsx`.

**Path Aliases:**
- Use the `@/*` alias for shared frontend code under `web/src/`; it is defined consistently in `web/tsconfig.json`, `web/vite.config.ts`, and `web/vitest.config.ts`.
- Use relative imports within the same feature or test directory. Use `@/components/ui/*`, `@/lib/utils`, and the aliases recorded in `web/components.json` for cross-feature/shared UI imports.
- Go imports use the full module path `github.com/chetto1983/aura/...`; do not introduce relative Go imports.

## Error Handling

**Patterns:**
- Return errors instead of panicking for expected failures. Create stable sentinel errors with `errors.New`, wrap them with contextual lowercase messages and `%w`, and classify them with `errors.Is`/`errors.As`; `internal/skills/validator.go` is the canonical compact example.
- Combine independent cleanup/state errors with `errors.Join` where callers need both causes, as in `internal/agent/llm_agent_retry.go`. Preserve panic only at explicit lifecycle boundaries that must roll back and re-panic, such as `internal/db/tx.go`.
- Check every returned error. If an error is intentionally ignored during best-effort cleanup, make that reason explicit at the narrow call site; transaction rollback in `internal/db/tx.go` is an example.
- Log an error or return it, but avoid doing both at the same layer. Add context while propagating and let command/server boundaries emit structured logs.
- In frontend API code, reject non-2xx responses and keep the status available through a typed error (`HttpError` in `web/src/api/json.ts`) when callers branch on status. Shared response parsing belongs in helpers such as `web/src/chat/http.ts`, not duplicated per client.
- Catch frontend errors as `unknown`, narrow with `instanceof Error`, and convert them to user-visible state or a deliberate fallback. Use `void promise.catch(...)` only for explicitly fire-and-forget work. Unexpected render failures are contained by `web/src/ErrorBoundary.tsx`.

## Logging

**Framework:** `log/slog` for Go; UI-visible state and error boundaries for the browser.

**Patterns:**
- Emit structured `slog.Info`, `slog.Warn`, and `slog.Error` records at process, server, scheduler, and degradation boundaries. Keep message templates stable and attach identifiers, durations, and errors as key/value attributes; examples are in `cmd/aura/serve.go` and `internal/gateway/reconcile.go`.
- Never log credentials, reset tokens, raw private payloads, or other secrets. `cmd/aura/recovery.go` documents the exceptional stdout-only operator handoff.
- Do not add browser `console` logging as a substitute for product error handling. Surface actionable failures in components and let tests assert the visible state.

## Comments

**When to Comment:**
- Comment hidden constraints, security boundaries, protocol quirks, concurrency/lifecycle invariants, and surprising dependency behavior. `internal/db/tx.go`, `web/playwright.config.ts`, and `web/vite.config.ts` demonstrate the expected “why” level.
- Include the relevant PRD decision/threat identifier when it makes a load-bearing rule traceable. Avoid comments that merely restate an identifier or the next statement.
- Tagged integration tests should state required services/environment and the exact invocation near the file header, as in `internal/db/db_test.go` and `cmd/aura/serve_smoke_test.go`.

**JSDoc/TSDoc:**
- Go exported identifiers require proper doc comments under `revive`; package-level architectural contracts belong in `doc.go`, for example `internal/web/doc.go`.
- TypeScript predominantly uses concise `//` comments for non-obvious runtime constraints. Use JSDoc for a shared API contract only when types and names do not already explain it; `web/src/chat/http.ts` is an example of a justified module-level contract comment.

## Function Design

**Size:** Keep functions focused and keep their files under 600 lines. Extract pure parsing, validation, formatting, and state-reduction logic so it is directly testable; examples include `internal/skills/validator.go`, `web/src/chat/durationFormat.ts`, and `web/src/graph/SigmaCanvas_reducers.ts`.

**Parameters:**
- Put `context.Context` first in Go operations that may block or cross a boundary. Accept dependency interfaces or configuration structs instead of global lookups and long positional argument lists.
- Use functional options for optional Go constructor behavior (`internal/sandbox/usersandbox/docker_backend.go`) and explicit dependency structs for larger compositions (`internal/channels/telegram/bot.go`).
- Use typed props interfaces and destructuring for React components. Mark props `readonly` and use optional properties only when absence is semantically valid under `exactOptionalPropertyTypes`.

**Return Values:**
- Use Go's `(value, error)` contract and return zero values with the error. Use named returns only when a deferred lifecycle genuinely needs to update the result, as in `internal/db/tx.go`.
- Use `Promise<T>` for frontend API operations, `void` for intentional side-effect-only helpers, and discriminated unions/typed status values for multi-state results.

## Module Design

**Exports:**
- Prefer small, explicit Go package APIs and keep implementation helpers unexported. Define interfaces at the consuming boundary and place reusable deterministic test doubles in dedicated support packages such as `internal/agent/agenttest/`.
- Prefer named TypeScript exports for components, helpers, and types. Default exports are reserved mainly for lazy-loaded workspace/page modules such as `web/src/graph/GraphExplorer.tsx` and `web/src/documents/DocumentsWorkspace.tsx`.
- For shadcn-derived UI, use the installed Radix-backed wrappers in `web/src/components/ui/`, the `cn()` helper in `web/src/lib/utils.ts`, semantic Aura tokens, configured Lucide icons, and accessible component composition. Preserve required dialog titles, native button semantics, focus behavior, and `aria-*` state.

**Barrel Files:**
- Barrel files are intentionally rare; direct imports are the norm. `web/src/components/skeleton/index.ts` is a narrow exception. Do not add broad `index.ts` barrels that hide ownership or create cycles.

---

*Convention analysis: 2026-08-02*
