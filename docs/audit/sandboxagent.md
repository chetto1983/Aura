# Audit: internal/sandboxagent

**Verdict:** needs-work — one not-wired exported method; package is otherwise clean.
**Counts:** critical 0 / high 0 / medium 1 / low 1

## Findings

### [MEDIUM][NOT-WIRED] `Client.Health` is exported but never called in production

**Location:** `internal/sandboxagent/client.go:106-123`
**Confidence:** high

`func (c *Client) Health(ctx context.Context) error` probes `/v1/health` on the sandbox-agent.
Grep across the entire repo (`D:/Aura`) for `.Health(` in non-test `.go` files returns zero hits outside
`internal/sandboxagent/client_test.go`. No production call site exists in:

- `cmd/aura/main.go` (registers `SandboxExec` tool, never calls `Health`)
- `cmd/aura/skills_snippet.go` (creates a client, calls `Run` only)
- any `aura web/mcp/task doctor` subcommand

The sandbox-agent client is constructed twice in production (`cmd/aura/main.go:116` and
`cmd/aura/skills_snippet.go:108`); neither uses `Health`. The method exists, the endpoint is
implemented server-side, but no operator-facing `aura sandbox doctor` subcommand or boot-time probe
calls it.

**Suggested fix:**
Either wire `Health` into a `aura sandbox doctor` subcommand (or the boot-time log that already
emits the sandbox URL), or unexport it (`health`) until it is wired. An unwired public health method
gives false confidence that the operator has a liveness check path.

---

### [LOW][OTHER] `mustRead` discards `io.ReadAll` error without annotation

**Location:** `internal/sandboxagent/client.go:127-129`
**Confidence:** high

```go
func mustRead(r io.Reader) []byte {
    b, _ := io.ReadAll(io.LimitReader(r, 4096))
    return b
}
```

The error is silently discarded. The function comment says "best-effort", which is a valid pattern for
draining an error-response body, but the name `mustRead` is misleading: `must*` conventionally implies
panic-on-error (see `regexp.MustCompile`, `template.Must`). A reader encountering `mustRead` for the
first time will expect a panic, not a silent partial read.

**Suggested fix:**
Rename to `drainBody` (or `readBodyForError`) to match the actual semantics, and optionally log the
error at debug level rather than dropping it entirely. No logic change needed.

---

## What was checked

- All exported and unexported identifiers in `internal/sandboxagent/client.go` (the only non-test file).
- Grep for every symbol across the full repo to confirm usage / non-usage.
- Goroutine lifecycle: no goroutines started; no ticker/timer leak surface.
- Resource management: `resp.Body.Close()` is correctly deferred after the nil-check on `err`; body is
  drained before close on error paths via `mustRead`; `io.LimitReader(r, 4096)` caps attacker-controlled
  body size.
- Error wrapping: all `fmt.Errorf` calls use `%w`; transport errors are correctly propagated.
- Context propagation: both `Run` and `Health` use `http.NewRequestWithContext(ctx, ...)`.
- Race surface: no shared mutable state; `Client` fields are written once in `New` and read-only
  thereafter; `http.Client` is goroutine-safe by stdlib contract.
- Config wiring: `Token`, `BaseURL`, `TimeoutSec` are all consumed in `New`; `AURA_SANDBOX_AGENT_TOKEN`
  is loaded in `internal/config/config.go:226` and flows through to both production call sites.
- Go version is 1.26.4 — loop-variable capture is fixed by language spec; no loop-capture findings.
