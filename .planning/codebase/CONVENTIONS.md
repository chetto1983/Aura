# Coding Conventions

**Analysis Date:** 2026-08-25

## Naming Patterns

**Files:**
- Use lowercase package directories and lowercase snake-style Go filenames: `internal/agent/llm_agent.go`, `internal/arcadedb/memory_backfill.go`, and `internal/db/tx_integration_test.go`.
- Keep Go tests beside the implementation as `*_test.go`; name live tiers `*_integration_test.go`, `*_live_test.go`, or `*_e2e_test.go` and put the build constraint at the top of the file.
- Name React component files in PascalCase (`web/src/shell/ShareShell.tsx`, `web/src/graph/GraphExplorer.tsx`) and hooks with a `use` prefix (`web/src/health/useRuntimeHealth.ts`).
- Name TypeScript utilities and API modules in lower camelCase (`web/src/chat/toolGrouping.ts`, `web/src/settings/settingsApi.ts`). Co-locate focused tests as `name.test.ts[x]` or group a component area's tests under `__tests__/`.
- Name Playwright files after the user surface with `.spec.ts`, such as `web/e2e/governance-write.spec.ts`.
- Use snake_case for Python modules and `test_<subject>.py` for Python tests, as in `services/ingest/source.py` and `services/ingest/tests/test_source_file_name.py`.
- Treat `internal/db/sqlc/` and `internal/webui/dist/` as generated output. Change their generators or source inputs instead of hand-editing generated files.

**Functions:**
- Use PascalCase for exported Go functions and methods and lower camelCase for package-private helpers. Constructors use `New...`; contextual decorators use `With...` (`internal/agent/tools/result.go`).
- Put `context.Context` first on I/O or cancellable Go APIs. Keep receiver names short and stable, and use action-oriented method names.
- Use lower camelCase for TypeScript/Python functions and PascalCase for React components. Hooks must start with `use` so the hooks linter can reason about them.
- Name tests after observable behavior, not implementation branches: `TestWithTx_RollbackOnError` in `internal/db/tx_integration_test.go` and `it('the still-running tool is never a member...')` in `web/src/chat/__tests__/toolGrouping.test.ts`.

**Variables:**
- Prefer short Go locals only when their role is obvious (`ctx`, `err`, `cfg`, `res`, `got`, `want`); use descriptive names across longer scopes.
- Use `got`/`want` in Go assertions and name table rows with a `name` field. Keep sentinel values and externally meaningful limits as named constants.
- Use `const` by default in TypeScript, `let` only for mutation, and declare public data shapes `readonly` where callers must not mutate them (`web/src/chat/toolGrouping.ts`).
- Use uppercase snake case for module-level Python constants and immutable TypeScript protocol constants (`PROSE_FORMATS` in `services/ingest/tests/test_extract.py`, `TOOL_GROUP_MIN` in `web/src/chat/toolGrouping.ts`).

**Types:**
- Use PascalCase for Go exported types and TypeScript interfaces/type aliases. Keep unexported implementation types lower camelCase.
- Define small Go interfaces at the consuming boundary and use role names such as `Store`, `Reader`, `Resolver`, `Dispatcher`, or `Backend`. Tests provide hand-written `fake...`, `stub...`, `recording...`, or `capture...` implementations.
- Use concrete structs for configuration and results; pass dependency interfaces only where substitution is required.
- Model TypeScript domain variants with string-literal unions and explicit interfaces. Prefer `unknown` at untrusted boundaries, then narrow it before use (`GroupablePart` in `web/src/chat/toolGrouping.ts`).

## Code Style

**Formatting:**
- Run `gofmt` on every Go edit. `.golangci.yml` enforces `gofmt`; `lefthook.yml` runs `scripts/gofmt-staged.sh` and stages the result.
- Follow `.editorconfig`: UTF-8, LF, final newline, trimmed trailing whitespace, two-space indentation generally, tabs for Go and Makefiles.
- Format `web/` with Prettier using single quotes, semicolons, trailing commas, a 100-column print width, and two-space indentation (`web/.prettierrc.json`).
- Keep every authored `.go`, `.ts`, and `.tsx` file at or below 600 lines. `scripts/check-file-size.sh`, `Makefile`, and `lefthook.yml` enforce the cap.
- Python has no repository-level formatter configuration. Match the existing four-space, type-hinted, PEP 8-style layout in `services/ingest/` and `scripts/`.

**Linting:**
- Use golangci-lint v2.12.2 with the repository's `.golangci.yml`. Enabled checks are `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `misspell`, `gosec`, `revive`, `dupl`, and `modernize`.
- Do not add broad `//nolint` directives. Name the exact linter and explain a genuine false positive. Existing exclusions are narrow and documented in `.golangci.yml`.
- Do not run Go gates as bare `./...` when `web/node_modules` is present. Use `$(bash scripts/go_packages.sh)` or the Make targets so Go examples inside frontend dependencies are excluded.
- Run frontend static gates through `make web-lint`: ESLint with zero warnings, strict TypeScript checking, and Prettier verification.
- `web/eslint.config.js` enables strict/stylistic type-aware rules, React Hooks, JSX accessibility, React Refresh, and import ordering. Test-only relaxations are scoped to test files.
- Keep `web/` free of copy/paste and dead code with `web/.jscpd.json`, `web/knip.json`, `scripts/check-dup.sh`, and `scripts/check-deadcode-web.sh`.

## Import Organization

**Order:**
1. Go standard-library imports.
2. A blank line, then Aura module imports (`github.com/chetto1983/aura/internal/...`).
3. Third-party imports, grouped with the Aura imports when they belong to the same dependency layer; always let `gofmt` own indentation.
4. TypeScript built-ins and external packages, then `@/` aliases, then parent/sibling imports. `import-x/order` enforces the category order without blank lines.
5. Use `import type` or inline `type` imports for TypeScript type-only dependencies (`web/src/components/ui/button.tsx`).

**Path Aliases:**
- Go code imports through module path `github.com/chetto1983/aura/...`; do not create relative Go imports.
- `web/tsconfig.json`, `web/vite.config.ts`, and `web/vitest.config.ts` define `@/*` as `web/src/*`. Use `@/` for cross-feature/shared imports and relative paths within a local feature.
- Avoid new TypeScript barrel files. Import the concrete module directly unless an existing package boundary already exposes an intentional index such as `web/src/components/skeleton/index.ts`.

## Error Handling

**Patterns:**
- Check every returned Go error. Return immediately with added operation context using `fmt.Errorf("<operation>: %w", err)` so callers can inspect the chain.
- Match sentinels and typed errors with `errors.Is`/`errors.As`; never branch on error strings when a typed contract exists. `internal/db/tx_integration_test.go` demonstrates sentinel matching and typed Postgres error inspection.
- Keep Go error text lowercase and without trailing punctuation. Log an error or return it, not both, unless a boundary is explicitly converting an error into a degraded result.
- Reserve `panic` for invariant violations and deliberate re-panic behavior. Expected runtime failures return errors or typed outcomes.
- Degrade only where the contract explicitly permits it. `internal/agent/tools/result.go` retains a preview after a sidecar write failure but returns hard errors for malformed path identifiers.
- Use `HttpError` from `web/src/api/json.ts` when callers need HTTP status/reason branching. At UI boundaries, narrow `unknown` with `instanceof Error`/`instanceof HttpError` and translate it into user-visible state.
- Do not swallow TypeScript errors casually. An empty `catch` is acceptable only for documented optional capabilities or intentionally ignored non-JSON/DOM behavior.
- In Python, let extraction/process errors fail loudly; subprocess calls use `check=True`, explicit timeouts, and postcondition checks (`services/ingest/extract.py`).

Canonical Go shape:

```go
value, err := dependency.Load(ctx, id)
if err != nil {
	return Result{}, fmt.Errorf("load result: %w", err)
}
```

## Logging

**Framework:** Go standard-library `log/slog`; browser console only at explicit UI failure boundaries.

**Patterns:**
- Use structured `slog.Debug`, `slog.Info`, `slog.Warn`, or `slog.Error` in services and background workers. Keep the message stable and put request IDs, thread IDs, task IDs, steps, and errors in keyed attributes (`internal/agent/llm_agent.go`, `internal/cron/scheduler.go`).
- Use `fmt.Print*` only for intentional CLI/user output under `cmd/`, not operational service logging.
- Never log secrets, full credentials, or raw sensitive payloads. Reuse redaction helpers such as `internal/secret` before persistence or display.
- In React, prefer rendered error state or an error boundary. `console.warn`/`console.error` is limited to failures where the UI has already contained the error (`web/src/graph/ArcadeGraphCanvas.tsx`, `web/src/conversations/ConversationSidebar.tsx`).

## Comments

**When to Comment:**
- Explain hidden constraints, measured behavior, security boundaries, compatibility traps, and why a non-obvious branch exists. `internal/canonicaljson/canonicaljson.go` and `web/vite.config.ts` are canonical examples.
- Do not narrate identifiers or obvious control flow. Keep comments synchronized when behavior changes.
- Cite the governing decision, live measurement, issue, or external API behavior when the reason would otherwise be lost.
- Use file/package comments for load-bearing contracts and build/run instructions, especially integration tiers.

**JSDoc/TSDoc:**
- Document exported TypeScript contracts when callers need semantic detail beyond the type, as in `web/src/chat/toolGrouping.ts` and `web/src/api/json.ts`.
- Go `revive` requires exported symbol comments. Interface implementations' repetitive `Spec`/`Execute` comments are narrowly excluded in `.golangci.yml`; do not generalize that exclusion.
- Python modules and public transformation functions use docstrings when behavior or supported formats are non-obvious (`services/ingest/extract.py`).

## Function Design

**Size:**
- Keep functions focused and files at or below 600 lines. Split a touched oversized module by concern instead of adding another branch to it.
- Prefer early returns and small pure helpers for parsing, validation, normalization, and transformation. Keep external I/O in thin boundary functions.
- Extract reusable behavior rather than duplicating it. Go `dupl` and frontend jscpd enforce this on production code.

**Parameters:**
- Pass `context.Context` first for cancellable Go operations.
- Use a config/options struct when a constructor has several related settings. Use functional `With...` helpers only when they decorate an existing value or context.
- Accept the smallest dependency interface the function consumes. Do not introduce wrappers around third-party APIs without first inventorying the installed dependency's public surface.
- Use explicit TypeScript props interfaces with `readonly` fields. Destructure React props at the component boundary.

**Return Values:**
- Return `(value, error)` for Go fallible operations and preserve zero-value usability where practical.
- Use typed result structs for multi-field outcomes. Do not encode status in ad-hoc strings when a type or sentinel can represent it.
- Return `null` in TypeScript only when absence is an intentional part of the declared contract (`toolRun`); throw typed errors for failed requests.

## Module Design

**Exports:**
- Go packages expose only the symbols required by consumers. Constructors validate dependencies before returning a usable component.
- Keep interfaces near consumers and implementations near their owned package. Test doubles remain in `_test.go` or shared test-support packages such as `internal/agent/agenttest` and `internal/dbtest`.
- Prefer named TypeScript exports for components, hooks, types, and utilities. Default exports are reserved for route/lazy-load boundaries already structured that way, such as `web/src/governance/GovernanceWorkspace.tsx`.
- Keep feature code together (`web/src/<feature>/`) and shared primitives under `web/src/components/ui/`, `web/src/lib/`, or `web/src/api/`.

**Barrel Files:**
- Go does not use barrel files; the package is the public boundary.
- TypeScript barrels are exceptional. Import concrete files to preserve traceability and bundle analysis, and extend an existing intentional barrel only when it is already the feature's public contract.

---

*Convention analysis: 2026-08-25*
