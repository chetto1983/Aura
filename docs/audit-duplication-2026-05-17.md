# Duplication Audit: Aura — 2026-05-17

Read-only audit by subagent. Surfaces code clusters that can be collapsed into
shared helpers per CLAUDE.md's "Never DUPLICATE CODE CREATE A REUSABLE CLASS".

## Status of recommendations

| # | Cluster | Sites | LOC saved | Priority | Status |
|---|---------|-------|-----------|----------|--------|
| 1 | Tool execution boilerplate | 4 | ~120 | HIGH | Open (needs design — see Notes) |
| 2 | String uniqueness appenders | 5 (audit said 3) | ~45 | HIGH | **Shipped 9e010638** (4 of 5 collapsed; 2 left untouched, see below) |
| 3 | Message cloning | 2 | ~15 | MED | Open (only one known user; watch for re-occurrence) |
| 4 | Telegram entity rendering + edit throttling | 2 | ~40 | MED | Open |
| 5 | HTTP client construction | 3+ | ~60 | MED | Open |
| 6 | Env var defaults + validation | 2+ | ~10 | LOW | Open (already mostly solved in config/env.go) |
| 7 | Test fixtures (recordingLoop, fakeInbound, fakeOutbound) | 3+ | ~50 | MED | Open |
| 8 | Config validation warnings | 2+ | ~15 | LOW | Open |

## Cluster 2 — AppendUnique (shipped)

The audit listed 3 sites; actual count was 5. Four had identical semantics:
- `internal/agent/loop.go:appendUniqueStrings`
- `internal/agent/exec_helpers.go:toolExecAppendUnique`
- `internal/cron/agent_job.go:appendUniqueStrings`
- `internal/channels/telegram/invocation_builder.go:appendUniqueStrings`

Collapsed into `internal/stringx.AppendUnique` in commit `9e010638`.

Two sites kept their bespoke implementations because semantics differ:
- `cmd/aura/web_chat.go:cleanWebToolList` — lower-cases for case-insensitive
  tool-name matching. Mixing case sensitivity into the shared helper would
  widen the API for a single-caller flag.
- `internal/api/mcp.go:appendUniqueStrings` — skips the trim + empty-skip checks
  because tool names come from configs that are already clean.

## Cluster 1 — Tool execution boilerplate (highest open value)

Three places re-implement the same fan-out + record + classify + wrap pipeline:
- `internal/agent/exec_helpers.go:ExecuteToolCalls` (channel-neutral, used by Telegram via `tool_exec_helpers.go`)
- `cmd/aura/web_chat.go:webToolExecutor.ExecuteToolCalls` (web)
- `internal/telegram/tool_exec_helpers.go:executeToolCalls` (Telegram wrapper)

Same flow: WaitGroup fan-out → allowlist check → ToolRunner.Execute → classify
error → record attempt → wrap untrusted result → assemble outcomes. Web channel
also handles its own max-chars truncation and skill-name extraction inline.

Recommended approach: refactor `webToolExecutor.executeOne` into a package-scope
helper that ExecuteToolCalls can reuse, OR extend ExecuteToolCalls to accept
optional max-chars + stateAdder hooks. Don't collapse without verifying semantics
(web_chat truncates result inline; agent/exec_helpers defers to caller).

Risk: MEDIUM — the channel-neutral path is shared by terminal-tool detection,
phantom-tool guards, and TerminalPolicyEnabled. Read all three call sites
before merging.

## Cluster 5 — HTTP client construction (next-best DRY win)

`strings.TrimRight(strings.TrimSpace(baseURL), "/")` + `&http.Client{Timeout: t}`
+ default-timeout fallback is repeated in at least:
- `internal/storage/qdrant/client.go:NewClient`
- `internal/storage/sources/ocr/client.go:New`
- `internal/storage/sources/markitdown/client.go:New`
- Likely more in `internal/storage/search/` and MCP clients (not enumerated).

Proposed `internal/httputil/client.go`:
```go
func NormalizeBaseURL(url string) (string, error)
func NewHTTPClient(timeout, fallback time.Duration) *http.Client
```

LOC savings ~60 across ≥3 clients. Risk LOW.

## Anti-duplication invariants (for future CLAUDE.md updates)

- **Tool execution dispatch**: always go through `agent.ExecuteToolCalls` or a
  thin wrapper. If a channel needs different concurrency semantics, extract the
  common parts (allowlist, classify, record, wrap) — don't reimplement them.
- **Tool error handling**: use `tools.ClassifyToolError`, `tools.FormatToolError`,
  and `agent.WrapUntrustedToolResult` exclusively. Never roll your own.
- **String uniqueness**: use `stringx.AppendUnique` for trim+skip-empty+dedup.
  Case-insensitive matching is a separate concern and stays bespoke.
- **HTTP clients** (when extracted): use the future `httputil.NormalizeBaseURL` +
  `NewHTTPClient` for new REST clients.

---

Audit produced 2026-05-17. Slice 1 (AppendUnique) shipped same day.
