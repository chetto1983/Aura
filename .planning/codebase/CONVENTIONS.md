---
last_mapped_commit: 26745a062dd1017c8e9de39a39089bc63559b553
---

# Coding Conventions

**Analysis Date:** 2026-08-13

## Naming Patterns

**Files (Go):**
- `snake_case.go`, one primary concern per file. Multi-file components split by
  `<component>_<concern>.go`: `internal/runner/runner_delete.go`,
  `internal/runner/runner_resume_batch_atomic.go`,
  `internal/agui/governance_write_scheduler.go`,
  `internal/agui/governance_write_skills_api.go`. This is the mandated response
  to the ≤600 LOC cap (CLAUDE.md "no god class") — never one file that grows
  past the cap, always a split file that keeps the same package.
- Test files mirror the file they test: `runner.go` → `runner_test.go`; a
  focused sub-concern gets its own test file even without a matching prod file
  (`internal/agui/approvals_api_unit_test.go`,
  `internal/agui/conversations_api_unit_test.go`).
- Build-tag-gated files carry a tier suffix in the name even though the tag is
  what actually gates them: `*_integration_test.go`, `*_live_test.go`,
  `*_live_e2e_test.go` (see TESTING.md for the full tag inventory).
- `main_test.go` is the reserved name for a package's `TestMain` — used to wire
  `goleak.VerifyTestMain` (22 packages, see TESTING.md).

**Packages:**
- Short, lower-case, no underscores: `agent`, `runner`, `agui`, `conversations`,
  `skills`, `arcadedb`. Sub-concerns get their own package under the owner
  (`internal/agent/tools`, `internal/agent/mcptools`, `internal/agent/workflow`,
  `internal/agent/prompt`, `internal/agent/agenttest`) rather than filename
  prefixes inside one flat package.
- Package doc comment on the `package` line explains ownership boundary and
  what does *not* belong there — e.g. `internal/config/config.go`: "Per
  CONTEXT.md D-row 'Composition': per-subsystem configs (db, llm) live in
  their owning packages; this file only wires the top-level fields."

**Functions / identifiers:**
- Standard Go `MixedCaps`/`mixedCaps`; exported identifiers get a doc comment
  starting with the identifier name (enforced by `revive`'s `exported` rule,
  `.golangci.yml`, with `disableStutteringCheck` so `tools.ToolResult`-style
  names are allowed).
- Sentinel/marker identifiers are prefixed by kind: `Err*` for `error` values
  (`agent.ErrBudgetExhausted`, `agui.ErrBootstrapAlreadyConfigured`,
  `agui.ErrMCPServerExists`), `Kind*` for string enums
  (`tools.KindClarification`, `tools.KindApproval`, `tools.KindChoice`).
- Unexported sentinel errors use a lower-case `err*` prefix
  (`errUnsupportedParentOperation`, `errInvalidArcadeRID`,
  `errInvalidIdempotencyKey`) — exported only when a cross-package caller needs
  `errors.Is`/`errors.As`.

**Env vars:** `AURA_<DOMAIN>_<UNIT>` (e.g. `AURA_SWARM_MAX_GOALS`,
`AURA_CONTEXT_COMPACTION_ENABLED`, `AURA_SKILL_BODY_CAP_BYTES`). Third-party
sidecar/library env vars keep their upstream canonical names without the
`AURA_` prefix (`TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`,
`POSTGRES_PASSWORD`, `SEARXNG_URL`). Every field in `internal/config/config.go`
documents its backing env var inline as a trailing comment.

## Code Style

**Formatting:**
- `gofmt` is enforced as a CI/hook gate, not a suggestion (`.golangci.yml`
  `formatters: enable: [gofmt]`; `lefthook.yml` pre-commit `gofmt` step runs
  `scripts/gofmt-staged.sh {staged_files}` with `stage_fixed: true`).
- Frontend: `prettier --check` at `web-lint` (`Makefile` `web-lint` target;
  `.github/workflows/ci.yml` job `web-lint`).

**Linting:** `golangci-lint` v2.12.2 (version pinned in `Makefile` `tools:` and
mirrored in CI, `.github/workflows/ci.yml` `build-and-lint` job), configured in
`.golangci.yml`:
- Enabled linters: `errcheck`, `govet`, `ineffassign`, `staticcheck`, `unused`,
  `misspell`, `gosec`, `revive`, `dupl`, `modernize` (Go 1.21+ idiom
  modernizations — `min`/`max`, `slices`/`maps` helpers, range-over-int,
  `strings.Cut`, `any`-type simplifications).
- `gosec` excludes `G115` (integer overflow, noisy on `int32` pool fields) and
  `G404` is NOT excluded (restore `math/rand/v2` usage if flagged).
- `dupl` threshold is `100` tokens; `_test.go` files are exempt from `dupl` and
  `gosec`/`errcheck` (table-driven test boilerplate is intentionally
  repetitive).
- Path exclusions (never linted): `internal/db/sqlc` (generated, golden),
  `internal/llm/client.go`, `third_party`, `web/node_modules`, `.planning`.
- `revive`'s `exported` rule runs with `disableStutteringCheck`; a special
  exception suppresses the "Spec/Execute missing comment" check in
  `internal/agent/tools` because the interface contract is documented on the
  Tool interface itself (`internal/agent/tools/spec.go`), not duplicated on
  every implementation.

**Vet / static analysis:** `go vet` is a required pre-push and CI gate
(`Makefile` `vet:`, `.github/workflows/ci.yml` `build-and-lint` job). `gofmt`
and `go vet` both run before `golangci-lint` in every gate ordering.

**Dead code:** `deadcode -test` via `scripts/deadcode_gate.sh` (`Makefile`
`deadcode:` target; wired at both pre-push (`lefthook.yml` →
`scripts/deadcode_pre_push.sh`) and CI (`build-and-lint` job)).

**File size:** hard 600-LOC cap enforced by `scripts/check-file-size.sh` over
`*.go`, `*.ts`, `*.tsx` (excludes `internal/db/sqlc`, `third_party`, `vendor`,
`node_modules`, `dist`, `*.d.ts`). Runs on staged files at pre-commit
(`lefthook.yml`) and on the whole tree at pre-push and in CI
(`.github/workflows/ci.yml` step "File-size cap (600 LOC)"). At HEAD the
largest owned files sit just under the cap (`internal/agui/server_run_resume_test.go`
600, `internal/agent/tools/skill_write_test.go` 596, `internal/config/config.go`
592) — the convention in practice is "split before you hit 600", confirmed by
the extensive `<name>_<concern>.go` file families under `internal/runner/` and
`internal/agui/`.

## Import Organization

**Order:** standard library, blank line, then everything else (module-internal
`github.com/chetto1983/aura/...` imports and third-party imports share one
group, alphabetized) — standard `goimports`/`gofmt` grouping, no custom import
grouping tool enforced beyond that. Example
(`internal/config/config.go:10-25`):
```go
import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/envutil"
	"github.com/chetto1983/aura/internal/idroot"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/secret"
	"github.com/joho/godotenv"
)
```

**Module path:** `github.com/chetto1983/aura` (see `go.mod`). All intra-repo
imports use the full module path, no relative imports.

**Path Aliases:** none in Go (module-path imports only). The web app uses a
single alias `@` → `web/src` (`web/vite.config.ts`, `web/tsconfig.json`,
mirrored in `web/vitest.config.ts`).

## Error Handling

**Wrapping:** `fmt.Errorf("<pkg-or-scope>: <what failed>: %w", err)` — over
1,100 occurrences of `%w` wrapping across `internal/`
(`internal/config/config.go:303`: `fmt.Errorf("config: load llm: %w", err)`).
The prefix names the failing scope (`"config: …"`, `"graphview: …"`), so a
wrapped chain reads as a breadcrumb without needing a stack trace.

**Sentinel errors:** exported package-level `var Err… = errors.New("…")` for
conditions a caller checks with `errors.Is` (`internal/agent/errors.go`:
`ErrBudgetExhausted`; `internal/agui/bootstrap_api.go`:
`ErrBootstrapAlreadyConfigured`, `ErrBootstrapInvalid`;
`internal/agui/governance_write_seam.go`: `ErrMCPServerExists`,
`ErrMCPServerNotFound`, `ErrSkillActiveExists`, `ErrSkillInvalidInput`). Each
sentinel carries a doc comment explaining WHY a caller needs `errors.Is`
rather than string matching.

**Structured error types (not plain sentinels):** a control-flow signal that
must carry a payload is a named `struct` implementing `error`, matched with
`errors.As`, never a formatted string. Canonical example —
`internal/agent/tools/ask_user.go`:
```go
type ErrAwaitingUserInput struct {
	Question   string
	Options    []Option
	Kind       string
	Priority   int
	ToolCallID string
	ResumeContext json.RawMessage
	ProxiedFromChildID string
	ProxiedToolCallID  string
}

func (e *ErrAwaitingUserInput) Error() string { return "awaiting user input" }
```
The agent dispatch loop (`internal/agent/llm_agent_pause.go`) intercepts this
type via `errors.As` to pause a turn — it is a control signal, not a failure
to be logged and swallowed. Unknown-input errors also favor a structured,
actionable message over a bare string:
`internal/agent/tools/action.go` `Dispatch`:
```go
return ToolResult{}, fmt.Errorf("unknown action %q: valid actions are %s", action, strings.Join(r.Actions(), ", "))
```

**Never silent:** `errcheck` is a hard lint gate (unchecked errors fail the
build); the Python EOL rule from CLAUDE.md ("Errors should never pass
silently. Unless explicitly silenced.") is enforced mechanically, not just by
convention.

## Logging

**Framework:** `log/slog` (standard library structured logging) — no
third-party logging framework. 73+ files call `slog.*` directly (no
package-level logger wrapper).

**Levels in practice:**
- `slog.Warn` for a degraded-but-continuing path — the majority of call sites.
  Pattern: name the subsystem and the consequence in the message itself, e.g.
  `internal/runner/runner_delete.go:181`:
  `slog.Warn("delete lifecycle: expire pauses failed (cascade delete will reconcile)", ...)`.
  `internal/runner/runner_persist.go:105`:
  `slog.Warn("tool invocation ledger insert failed (continuing)", ...)`.
- `slog.Error` for failures that corrupt state going forward, e.g.
  `internal/runner/runner.go:451`:
  `slog.Error("runner: flush pause assistant turn failed; resume history may be malformed", ...)`.

**Attribute convention:** `slog.Warn("<subsystem>: <what and consequence>", "key", value, ...)`
— key/value pairs are the identifiers relevant to reproducing the failure
(`identity_id`, `conv`, `err`), never string-interpolated into the message.

## Comments

**Policy (CLAUDE.md, enforced in review):** "NO COMMENTS UNLESS WHY IS
NON-OBVIOUS. Identifier names already explain what. Comments only for hidden
constraints, workarounds, or surprising behavior." In practice this produces
long, dense doc comments on exported types/functions that explain a *design
decision or measured trade-off*, frequently citing a PRD requirement ID
(`D-05`, `SC#1`), a phase number, or a dated incident. Example —
`internal/agent/tools/manifest.go` `RenderToolDefs` doc comment cites the
specific bug it fixes ("the old code emitted every deferred tool as a callable
function with an EMPTY parameter schema, which made the model hallucinate
arguments") rather than describing what the function does line-by-line.

**Citations:** comments cite external documentation pages when behavior is
surprising ("Cite the page in the code comment when the behaviour is
surprising" — CLAUDE.md), and cite dates/measurements when recording an
empirical finding. Example from `internal/agent/tools/always_active_test.go`:
"2026-08-07: the set was aligned to the reference coding agent's own
always-active tools, which is a measured configuration rather than an opinion."
Another from `internal/agent/tools/skill.go`: "1.638 token, il singolo
tool piu' caro del manifest e il 12% di OGNI prompt".

**JSDoc/TSDoc (web/):** not systematically enforced; TypeScript types carry
most of the contract. `eslint` + `tsc --noEmit` are the frontend static gates
(`web-lint`), not a comment-coverage tool.

## Function Design

**Size:** governed indirectly by the 600-LOC file cap — a function that grows
large enough to push its file over the cap gets extracted into a sibling
`_<concern>.go` file, not compressed. No standalone function-length linter is
configured.

**Context propagation:** every I/O-bound or cancelable function takes
`ctx context.Context` as its first parameter — confirmed across
`internal/runner/runner.go` (`Turn`, `TurnBranch`, `runTurn`, `turnLocked`,
`scopeContextToConversation`, `appendUserTurn`, `buildAgent`, all
`ctx context.Context`-first). Streaming responses use
`iter.Seq2[*agent.Event, error]` (Go 1.23+ range-over-func iterators) instead
of channels for agent turn output (`Runner.Turn`, `Runner.TurnBranch`).

**Tool handlers:** the multi-action tool pattern
(`internal/agent/tools/action.go`) — `ActionFunc = func(ctx context.Context, args json.RawMessage) (ToolResult, error)` —
receives the *whole* raw JSON args object including the discriminator field;
each handler re-unmarshals only the fields it needs. `ActionRouter.Dispatch`
never panics on an unknown action; it returns a structured error naming the
valid actions.

**Return values:** Go idiomatic `(value, error)`; no custom `Result[T]`
generic wrapper for ordinary functions. Tool execution instead uses a
domain-specific `ToolResult` type (`internal/agent/tools/result.go`) alongside
the error return, so "tool succeeded but reports failure to the model" and "Go
call itself errored" are distinct channels.

## Module Design (Go)

**Exports:** small, focused interfaces at consumption points rather than one
large interface per concrete type — e.g. `llm.Client` is satisfied by both the
production client and `agenttest.FakeClient` (`var _ llm.Client = (*FakeClient)(nil)`
compile-time assertion pattern, used throughout for interface-satisfaction
checks without a runtime cost).

**Composition root:** `internal/config` composes only top-level wiring;
subsystem configuration (DB pool config, LLM client config, MCP server
config) is owned by its subsystem package and merely aggregated into the root
`Config` struct (`internal/config/config.go` `Config` struct embeds
`db.Config`, `llm.Config`, `mcp.ServerConfig`/`mcp.ManagedServer` maps, etc.).
Loading is staged: `Load()`, `LoadServe()`, `LoadDB()` — increasingly-strict
subsets of the same root, so a subcommand only pays for the validation it
needs (`internal/config/config.go:297-354`).

**Barrel files:** not used — Go has no barrel/index-file idiom; each package
exports directly from its own files.

## Deferred-tool `Spec` pattern (`internal/agent/tools/`)

This is the mandatory shape for adding a new agent tool (CLAUDE.md "Tool
design — deferred-tool pattern"). The `Spec` struct
(`internal/agent/tools/spec.go:39`) is the full contract:

```go
type Spec struct {
	Name        string
	Summary     string          // one line, always shown in the manifest
	Description string          // full description; only shown when not Deferred OR after a tool_search hit
	Parameters  json.RawMessage // JSON-schema for the tool arguments
	Deferred    bool            // true → full spec hidden until tool_search loads it
	Mutating    bool            // runtime-only hint: can this call change host state?
	Destructive bool            // runtime-only hint: destructive/irreversible mutation
	Multiplexed bool            // runtime-only hint: multi-action tool (task/skill/swarm_spawn)
	OperationScope      idempotency.Scope
	OperationNormalizer string
	ReplayPolicy        ReplayPolicy
}
```

Convention:
- Big tools (long description, complex schema, examples) set `Deferred: true`
  and get a comment quantifying the token cost that justifies deferring them —
  e.g. `internal/agent/tools/skill.go:168-171` ("1.638 token, il singolo tool
  piu' caro del manifest e il 12% di OGNI prompt … Deferred: true"),
  `internal/agent/tools/todo.go:69-71`, `internal/agent/tools/task.go:137-139`,
  `internal/agent/tools/current_time.go:32-33`. A `Deferred: true` tool is
  fully excluded from the LLM-visible callable set until `tool_search`
  promotes it (`internal/agent/tools/manifest.go` `RenderToolDefs`) — this
  fixed a hallucinated-argument bug from an earlier design that shipped
  deferred tools with an empty schema.
- Small, always-needed tools (`ask_user`, `document_open`, `document_search`,
  `patch`, `read_file`, `read_tool_output`, `search`, `search_files`,
  `send_file`, `shell_exec`, `text_response`, `write_file`) set
  `Deferred: false`.
- One tool implementation lives in one file:
  `internal/agent/tools/<name>.go` (e.g. `ask_user.go`, `send_file.go`,
  `skill.go`, `web_fetch.go`, `web_search.go`). Multi-action tools use
  `internal/agent/tools/action.go`'s `ActionRouter` to dispatch one manifest
  entry to N per-action handlers instead of N near-duplicate tool files — the
  documented anti-pattern it replaces is a "587-LOC scheduler.go god-tool".
- The manifest never re-orders: `Registry.Render()` /
  `Registry.RenderToolDefs()` / `Registry.RenderJSON()` all sort by `Name`
  before returning — any non-deterministic ordering would invalidate the
  provider-side prompt cache (`internal/agent/tools/manifest.go:23-25`).
- `Mutating`/`Destructive`/`Multiplexed` are never wire-encoded to the model —
  they are runtime-only hints consumed by the policy gateway
  (`internal/gateway`) and the agent's completion-gate critic.

## Config loading (`internal/config/`)

- One file per config concern: `config.go` (root composite + `Load`/
  `LoadServe`/`LoadDB`), `config_env.go` (raw env accessors), `config_defaults.go`,
  `config_knobs.go`, `config_paths.go`, `config_sandbox.go`, `config_embed.go`,
  `config_mcp.go`, `config_retention.go`, `config_routes.go`,
  `config_runtimeprofile.go`, `config_share.go`, `config_agui_run.go`,
  `config_document_pipeline.go`, `arcadedb.go` — each with a matching
  `_test.go`.
- **Fail-fast vs silent-fallback split is deliberate and load-bearing**: a
  required secret/credential fails boot loudly (`fmt.Errorf` returned from
  `Load`); an ad-hoc numeric/boolean tuning knob absorbs a malformed or unset
  value to a hardcoded fallback via `internal/envutil` (`IntDefault`,
  `BoolDefault`) rather than blocking startup. See
  `internal/envutil/envutil.go` doc comment: "a typo in an ad-hoc env tweak
  should never block startup; the REQUIRED secrets fail fast in their own
  Validate paths instead."
- Every `Config` struct field is documented inline with its backing
  `AURA_*`/upstream env var name as a trailing comment
  (`internal/config/config.go:33-120`), which is the de facto env-var catalog
  cross-referenced from `prd.md` §Caps & Limits.
- `.env` is loaded via `github.com/joho/godotenv` (`config.go:24`); secrets are
  never logged (`internal/secret` package is imported for redaction —
  `config_secret_redactor_test.go` exists as its dedicated test).
- Composed DSNs (`AURA_DB_URL`, `AURA_DB_MIGRATE_URL`) are assembled from
  `POSTGRES_*` primitives at `Load()`-time rather than read as a single
  pre-composed var by the application, but integration TESTS read the
  composed DSNs directly (see TESTING.md).

---

*Convention analysis: 2026-08-13*
