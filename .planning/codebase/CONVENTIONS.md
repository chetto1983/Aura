# Coding Conventions

**Analysis Date:** 2026-07-04

This is a dual-stack repo: a Go backend (`internal/`, `cmd/aura`, module `github.com/chetto1983/aura`, Go 1.26.4) and a React/TypeScript frontend (`web/`). Conventions below cover both; Go dominates (~437 non-test `.go` files under `internal/`, 573 `_test.go` files).

## Naming Patterns

**Go files:**
- `snake_case.go` for multi-word files: `llm_agent_completion.go`, `budget_dedup.go`, `store_branch_fork_test.go`.
- **God-file splitting by concern** is the dominant pattern, not by layer: `llm_agent.go` has 20+ siblings (`llm_agent_args.go`, `llm_agent_construct.go`, `llm_agent_consume.go`, `llm_agent_dispatch.go`, `llm_agent_display.go`, `llm_agent_events.go`, `llm_agent_finalize.go`, `llm_agent_parallel.go`, `llm_agent_pause.go`, `llm_agent_prefix.go`, `llm_agent_reasoning.go`, `llm_agent_retry.go`, `llm_agent_stream_retry.go`, `llm_agent_truncation.go` in `internal/agent/`) — each file owns one behavioral concern of the same struct. This is the CLAUDE.md "refactor-on-touch, split into `<name>_<concern>.go`" rule in practice.
- Common per-package filenames: `store.go` (12 occurrences — the repository/persistence file), `client.go` (8), `config.go` (6), `types.go` (5), `doc.go` (5, package-level doc comment only).
- Integration test files end `_integration_test.go` (`store_integration_test.go`, `onboarding_provision_integration_test.go`); property tests end `_property_test.go` (`loop_property_test.go`, `classify_property_test.go`, `envedit_property_test.go`, `swarm_property_test.go`); fuzz tests end `_fuzz_test.go` (`agent_fuzz_test.go`, `validator_fuzz_test.go`); live/paid-gate tests end `_live_test.go` / `_live_e2e_test.go` (`reasoning_tier_live_test.go`, `adaptive_reasoning_live_e2e_test.go`, `cot_live_e2e_test.go`).
- Internal (white-box) test files use `package <pkg>` and can name themselves `<file>_internal_test.go` when there's already a black-box `<file>_test.go` for the same subject (`llm_agent_breaker_internal_test.go` vs `llm_agent_finalize_test.go`).

**Go identifiers:**
- Exported sentinel errors: `Err<Noun>` — `ErrBudgetExhausted` (`internal/agent/errors.go`), `ErrPauseNotFound`, `ErrInvalidAnswer` (`internal/askuser/store.go`).
- Config struct fields are `PascalCase` mirroring the `AURA_<DOMAIN>_<UNIT>` env var they load from, documented inline: `ConversationTurnCapBytes int // AURA_CONVERSATION_TURN_CAP_BYTES — ...` (`internal/config/config.go`).
- Test helpers use `t.Helper()` unconditionally (`envOrSkip`, `migratedPool`, `withFastBackoff`).
- Interface implementations verified with compile-time assertions: `var _ Agent = stubAgent{}` (`internal/agent/agent_test.go`), `var _ llm.Client = (*FakeClient)(nil)` (`internal/agent/agenttest/fakeclient.go`).

**TypeScript/React files (`web/src/`):**
- Components: `PascalCase.tsx` (`ApprovalList.tsx`, `ApprovalBadge.tsx`, `InlineApprovalCard.tsx`).
- Tests co-located in `__tests__/` subdirectories per feature folder: `web/src/approvals/__tests__/ApprovalList.test.tsx`.
- Hooks: `useXxx.ts` (`useApprovals.ts`, `useAttachmentUploads.tsx`).

## Code Style

**Formatting:**
- Go: `gofmt` enforced via golangci-lint `formatters` block in `.golangci.yml`; lefthook pre-commit runs `gofmt -w`.
- TS/JS: Prettier, config in `web/.prettierrc`: `{ singleQuote: true, semi: true, trailingComma: "all", printWidth: 100, tabWidth: 2 }`.

**Linting:**
- Go: `golangci-lint` v2.12.2 (CI-pinned, `.golangci.yml`, `run.timeout: 5m`). Enabled linters: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`, `misspell`, `gosec`, `revive`, `dupl`.
  - `gosec` excludes `G115` (integer overflow — noisy on safe int32 pool fields).
  - `dupl` threshold `100`; `_test.go` files exempt (table-driven boilerplate is intentionally repetitive).
  - `revive` rule `exported` has `disableStutteringCheck` (allows e.g. `agent.Agent`).
  - Path exclusions (both linters and formatters): `internal/db/sqlc` (generated, golden), `internal/agent/tools` and `internal/llm/client.go` (pre-rewrite skeletons owned by a future slice), `third_party`, `web/node_modules`, `.planning`.
  - The `.golangci.yml` header explicitly states architecture-boundary linters (e.g. `depguard`) are deliberately NOT enabled yet — "adding linters here without a slice-level rationale is forbidden."
- TS: ESLint flat config (`web/eslint.config.js`) — `typescript-eslint` `strictTypeChecked` + `stylisticTypeChecked`, `eslint-plugin-react-hooks`, `eslint-plugin-jsx-a11y`, `eslint-plugin-import-x` (with `import-x/order` warn, no-newlines-between groups: builtin/external/internal/parent/sibling/index). `npm run lint` runs with `--max-warnings=0` (zero-tolerance).

**`//nolint` usage:** must name the linter + include a justification comment (enforced by `nolintlint`, per the golang-lint skill); grep the codebase for the pattern `//nolint:<linter> // <reason>` before adding one.

## Import Organization

**Go:**
- Standard library first, blank line, then third-party, blank line, then `github.com/chetto1983/aura/...` internal imports — standard `gofmt`/`goimports` grouping (see `internal/config/config.go`: `fmt/net/net/url/os/path/filepath` then `github.com/chetto1983/aura/internal/db`, `.../envutil`, `.../knowledge`, `.../llm`, `.../mcp`, `.../profile`, then `github.com/joho/godotenv`).
- Module path: `github.com/chetto1983/aura` — all internal imports use this full path, never relative.

**TypeScript:**
- Path alias `@/` maps to `web/src/` (mirrored in `vite.config.ts`, `tsconfig.json`, `vitest.config.ts`).
- `import-x/order` enforces builtin → external → internal → parent → sibling → index, no blank lines forced between groups.

## Error Handling

Go stdlib only — **no third-party error library** (no `samber/oops`, no `pkg/errors`) is used or vendored.

**Wrapping:** `fmt.Errorf("<context>: %w", err)` throughout, lowercase message, no trailing punctuation:
```go
return nil, fmt.Errorf("config: load llm: %w", err)
return dir, fmt.Errorf("AURA_RUN_DIR=%q could not be resolved to an absolute path: %w", dir, err)
```

**Sentinel errors:** declared with `errors.New` at package scope, doc-commented with the *contract* they signal, not just what they are:
```go
// ErrBudgetExhausted is the exported sentinel for callers that inspect agent
// termination OUTSIDE the Event stream (D-04). Inside a Run the canonical signal
// is an explicit Event (Actions.Escalate=true + StateDelta termination_reason);
// this sentinel exists only so Phase 3/9 consumers can do errors.Is(err,
// agent.ErrBudgetExhausted) when a budget limit surfaces through the error slot.
var ErrBudgetExhausted = errors.New("agent budget exhausted")
```
(`internal/agent/errors.go`, mirrored by `ErrPauseNotFound`/`ErrInvalidAnswer` in `internal/askuser/store.go`.)

**Inspection:** `errors.Is`/`errors.As` for chain matching, `errors.Join` when combining independent failures (e.g. test `errors.Join(errors.New("loop hit cap"), ErrBudgetExhausted)` in `internal/agent/event_test.go`).

**No panic for expected conditions.** Agent-level panics ARE recovered at the goroutine/loop boundary and turned into errors flowing through the `iter.Seq2` error slot: `yield(nil, fmt.Errorf("agent panic: %v", r))` (`internal/agent/llm_agent.go`).

**Retryability classification:** transient vs permanent errors are classified via typed checks (`net.Error.Timeout()`, `url.Error`, string markers like `"wsarecv:"`), not via a single blanket retry-everything policy — see `retryableStreamOpenError` and `llm_agent_retry.go`.

## Logging

**Framework:** `log/slog` exclusively (60 files import `"log/slog"`), no `fmt.Println`/`log.Printf` for structured logging.

**Pattern:** stable low-cardinality message + structured key/value attributes, following the single-handling rule (log OR return, never both):
```go
slog.Info("agent turn start", "request_id", requestID, "thread_id", a.sessionID, "agent", a.name)
slog.Error("agent llm call error", "request_id", requestID, "thread_id", a.sessionID, "kind", llmErrorKind("stream_open", err), "err", err)
```
(`internal/agent/llm_agent.go`)

## Comments

**Style:** comments explain *why*, not *what* — heavy use of doc comments referencing design decisions by ID (`D-04`, `D-21`, `F-041`, `T-08.2-13`, `ONBD-01a`) that trace back to `prd.md`/phase plans. Example:
```go
// RunDir string  // absolute — a relative AURA_RUN_DIR is normalized to absolute at
// load (F-041) so sidecars are not cwd-dependent
```
- Package-level doc comments (`doc.go` files, 5 occurrences) or a top-of-file comment describing the package's scope and its slice/phase ownership boundary (see `internal/config/config.go` lines 1-8: explains what belongs here vs. subsystem packages, and which future slice may add fields).
- No comments restating obvious code (CLAUDE.md rule: "Identifier names already explain what. Comments only for hidden constraints, workarounds, or surprising behavior").

**JSDoc/TSDoc:** not used in `web/`; TypeScript relies on type signatures + inline `//` comments for non-obvious behavior (e.g. the `stubFetch`/mock-setup rationale comments in test files).

## Function Design

**Size:** enforced by `scripts/check-file-size.sh` (`make file-size`) — hard 600-LOC cap per file (CLAUDE.md "NO GOD CLASS"). Files approaching the cap get split by concern (see Naming Patterns above). Largest current files: `internal/agui/server.go` (598), `internal/agent/tools/shell_exec.go` (596), `internal/agent/llm_agent.go` (578), `internal/runner/runner.go` (575), `internal/config/config.go` (550).

**Parameters:** `context.Context` always first parameter, named `ctx`, propagated end-to-end (never re-created mid-chain); this is enforced structurally in agent/tool/db code paths.

**Return values:** Go idiomatic `(value, error)` or `(value, bool, error)` triples for domain "not found" distinctions (see the `golang-database` skill pattern, mirrored in `internal/askuser/store.go` and similar `store.go` files).

## Module Design

**Package boundary discipline:** subsystem configs live in their own packages (`db.Config`, `knowledge.Config`, `llm.Config`), composed into one root `config.Config` — never inlined into a monolithic settings struct. Comment in `internal/config/config.go` explicitly states the composition contract and which future phase may extend it.

**Interfaces:** small, consumer-defined interfaces (per `golang-structs-interfaces` skill, actively followed) — e.g. `llm.Client` is a narrow interface implemented by both the real client and `agenttest.FakeClient`; `Agent` interface in `internal/agent/agent.go` is intentionally "open" (no sealing), verified in tests via ad hoc `stubAgent` structs plus a compile-time assertion.

**Exports:** no barrel files/re-export shims observed in Go; each package exports directly. In `web/`, feature folders (`chat/`, `approvals/`, `governance/`) export components/hooks directly from their files, no `index.ts` barrels observed in the sampled folders.

**Generated code is fenced off:** `internal/db/sqlc` (sqlc-generated Postgres client) is excluded from lint/format and from the coverage floor — treated as "golden," never hand-edited (see `sqlc.yaml`, `make sqlc`).

---

*Convention analysis: 2026-07-04*
