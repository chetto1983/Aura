# Audit: internal/sandboxagent

**Verdict:** needs-work — one high-severity security regression (bearer auth promised by Phase 11-07 never landed in client.go) and one medium dead-code / not-wired finding.

**Counts:** critical 0 / high 1 / medium 1 / low 0

---

## Findings

### [HIGH][NOT-WIRED] Bearer auth token never wired into Client — security regression

**Location:** `internal/sandboxagent/client.go:22-29, 67-70`  
**Confidence:** high

**Detail:**

Phase 11-07 (commit `0272a973`, SUMMARY line 93 + 111) explicitly states:
> `internal/sandboxagent.Client` sends `Authorization: Bearer` on every request (exec + a new `Health` probe), a 401 surfaces a clear auth-failed error…
> Files modified: `internal/sandboxagent/client.go` — `Config.Token` → Bearer on exec + new `Health`

The current `client.go` contains no `Token` field in `Config`, no `Authorization: Bearer` header set in `Run()` or `Health()`, and no 401 detection path. The compose healthcheck and PRD env-var table (`prd.md:5077`) both reference `AURA_SANDBOX_AGENT_TOKEN` as "Bearer sent by `sandboxagent.Client`". The config loader (`internal/config/config.go:222-225`) also has no `Token` field in the `SandboxAgent` block.

Grep confirms: zero occurrences of `Authorization`, `Bearer`, or `Token` anywhere in `internal/sandboxagent/client.go`. The SUMMARY doc and the compose service (`--token`) are inconsistent with the code — the compose side was updated but the client side was not committed, or was reverted without updating the SUMMARY.

Security impact: sandbox-agent running with `--token` (enforced auth) will reject every request from the production client with HTTP 401, causing all sandbox_exec calls to return `sandbox_unavailable`. If still running `--no-token`, the auth hardening intent (D-38 / spike 008) is entirely absent, leaving the exec endpoint open on loopback without access control.

**Suggested fix:**

```go
type Config struct {
    BaseURL    string
    TimeoutSec int
    Token      string // AURA_SANDBOX_AGENT_TOKEN; empty = no auth (--no-token mode)
}

type Client struct {
    baseURL string
    http    *http.Client
    token   string
}

func New(cfg Config) *Client {
    // ... existing init ...
    return &Client{baseURL: base, http: ..., token: cfg.Token}
}

// In Run() and Health(), after jsonHeaders:
if c.token != "" {
    httpReq.Header.Set("Authorization", "Bearer "+c.token)
}
```

Add `Token: envDefault("AURA_SANDBOX_AGENT_TOKEN", "")` to the SandboxAgent block in `internal/config/config.go`. Add a 401 check branch returning a clear `fmt.Errorf("sandbox-agent auth: 401 Unauthorized (check AURA_SANDBOX_AGENT_TOKEN)")`.

---

### [MEDIUM][NOT-WIRED] Client.Health is exported but never called in production

**Location:** `internal/sandboxagent/client.go:100-117`  
**Confidence:** high

**Detail:**

`Client.Health(ctx context.Context) error` is an exported method. Grep across all `*.go` files in `D:/Aura` for `\.Health\(` returns zero non-test, non-definition hits inside any production package. The `sandboxRunner` interface in `internal/agent/tools/sandbox_exec.go` only requires `Run()`. No startup health-check, no readiness probe, no CLI `doctor` command calls this method. The method was presumably intended as a pre-flight probe but was never wired to anything that runs in production.

Note: the 11-07-SUMMARY claims `Health` was added as part of the Bearer-auth wave (`exec + a new Health probe`). Both the token wiring and the `Health` call-site are absent from the committed code, consistent with the auth regression above.

**Suggested fix:**

Either (a) wire `Health` into the agent startup sequence (e.g., in `cmd/aura/main.go` after creating the `SandboxExec` tool, call `c.Runner.(*sandboxagent.Client).Health(ctx)` and log a warning if it fails), or (b) add `Health` to the `sandboxRunner` interface and call it from `SandboxExec.Execute` on the first invocation with a once-flag. Option (a) is simpler and matches the "sandbox is optional" design (fail-soft, surface as `sandbox_unavailable`).

---

## What was checked and found clean

- **Nil-pointer:** `resp` is always checked before `resp.Body.Close()`. `mustRead` accepts an `io.Reader` interface; no nil deref possible.
- **Unchecked errors:** `resp.Body.Close()` is intentionally discarded via `_ =` in a defer — correct pattern for response body cleanup. `mustRead` discards `io.ReadAll` error by design (best-effort, documented). `json.Marshal` errors in `Run()` are checked. JSON decode error is checked.
- **Resource leaks:** `defer resp.Body.Close()` is present in both `Run()` and `Health()`. `io.LimitReader(r, 4096)` bounds the body read in `mustRead`. No goroutines are started. No ticker, no time.After.
- **Context propagation:** both `Run()` and `Health()` use `http.NewRequestWithContext(ctx, ...)` — context is correctly threaded.
- **HTTP client timeout:** set at construction in `New()` via `http.Client{Timeout: ...}`. The `http.Client` is reused (not allocated per call).
- **Integer/conversion:** `TimeoutSec int` → `time.Duration(timeout) * time.Second` — no overflow risk at any realistic timeout value.
- **Status code check:** `resp.StatusCode < 200 || resp.StatusCode > 299` correctly handles all non-2xx. The `mustRead` call on the error path consumes the body before the `defer Close()` runs — no double-read issue because `LimitReader` drains at most 4 KiB and the remaining body is drained by the deferred close.
- **Races:** no shared mutable state; `Client` fields are set once in `New()` and then read-only. `http.Client` is safe for concurrent use. No maps written concurrently. No goroutines launched.
- **Dead code:** `mustRead` and `jsonHeaders` are package-private helpers used by `Run()` and `Health()` within the file — not dead.
- **Loop-variable capture:** no loops in this file.
- **JSON/SQL:** only `json.Marshal` (request) and `json.NewDecoder.Decode` (response) — both correctly checked.
