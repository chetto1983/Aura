# P0 Closure Validation - 2026-06-10

Both P0 findings from the production-readiness audit are closed in code and validated by targeted tests.

Full-suite validation: `go test -count=1 ./...`

## R-02 / A-2 - MCP call timeout and shutdown unblocking

**Status:** CLOSED

**Code changes:**
- `internal/mcp/client.go` now passes call contexts into stdio round trips and makes response reads context-aware.
- Timed-out stdio reads close/poison the transport so blocked readers and `Close()` unblock.
- `internal/agent/mcptools/bridge.go` applies `AURA_MCP_CALL_TIMEOUT_SEC` per bridged MCP call.
- `internal/agent/mcptools/bridge_reconnect.go` no longer holds the reconnecting-server mutex across blocking calls.

**Validation:**
- `go test ./internal/mcp`
- `go test ./internal/agent/mcptools`

**Regression coverage:**
- Hung stdio server returns a deadline/transport error.
- `Close()` returns after a timed-out read.
- A separate MCP client remains usable after another server hangs.
- Reconnecting server `Close()` does not wait behind an in-flight blocked call.
- Bridged MCP calls observe the configured timeout.

## R-01 / A-11 - Untrusted tool-output provenance

**Status:** CLOSED

**Code changes:**
- `tools.ToolResult` now carries runtime-only provenance metadata.
- MCP bridged tool results are marked `trust="untrusted"`.
- `LlmAgent` wraps untrusted outputs before adding them to prompt history.
- The wrapper escapes NFKC-normalized content and adds a host-minted nonce.
- The system prompt now tells the model that untrusted `tool_output` envelope content is data, not instructions.

**Validation:**
- `go test ./internal/agent`

**Regression coverage:**
- `web_fetch` output is wrapped before the next LLM request.
- Forged `</assistant>`, `<|im_start|>`, and `</tool_output>` payloads are neutralized.
- Tool-result provenance from arbitrary MCP-style tool names is wrapped.
- Trusted built-in style output, such as `current_time`, remains unwrapped.
