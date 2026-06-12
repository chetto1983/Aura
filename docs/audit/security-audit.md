# Security Audit — Aura `internal/agent` (2026-06-12)

Scope: `internal/agent/tools/*`, `mcptools/*`, `prompt/*`, `trust.go`, `llm_agent*`, with blast-radius into `internal/skills`, `internal/swarm`, `internal/mcp`, `internal/runner`, `internal/config`.

**Threat model honored.** Full-host `shell_exec` is an accepted design decision (amendment #50/D-15c, "Aura needs a full terminal like you"). Host access *per se* is **not** flagged. Findings target the **guardrails around** that power: untrusted-content steering, secret flow, exfiltration, self-extension persistence, and the audit trail.

Each finding is classed **ARCHITECTURAL** (inherent to the full-host design — needs a guardrail / explicit risk decision) or **IMPLEMENTATION** (a directly fixable bug).

---

## 1. Prompt injection / trust boundary

### The good news (verified closed)

The prior P0 — untrusted tool output entering the prompt unmarked — is **genuinely closed for the eight direct feeders**. `trust.go` wraps `web_fetch`, `web_search`, `fs_read`, `fs_grep`, `fs_glob`, `read_tool_output`, `shell_exec`, `shell_poll` output (and any result carrying `Provenance.Trust == TrustUntrusted`) in:

```
<tool_output source="…" trust="untrusted" nonce="<16 hex from crypto/rand>">
…NFKC-normalized, html.EscapeString'd content…
</tool_output>
```

This is a correct control-token neutralization: a forged `</tool_output>` or `<|im_start|>` in the payload is HTML-escaped to inert text, and the per-call random nonce makes the delimiter unguessable. All feeders route through `renderToolResultForPrompt` on both the error and success paths. This is solid, defense-grade work.

### [P1] B-02 — Swarm child reports bypass the envelope — ARCHITECTURAL

The envelope is keyed on a **tool-name allowlist** in `trust.go`, not on a universal property of untrusted results. `swarm_spawn` is not on the list and its adapter (`internal/swarm/runner_adapter.go:54`) sets no `Provenance`, so a worker that fetches attacker-controlled content and summarizes it re-enters the **parent** prompt unwrapped. A swarm worker is the *most* likely place to ingest untrusted bytes (the fan-out exists for research), so this is the boundary's weakest point. **Fix:** set `Provenance{Source:"swarm", Trust:TrustUntrusted}` at the adapter (the envelope path already keys off it). The deeper structural fix (target architecture) is to default `Trust=Untrusted` for any externally-sourced `ToolResult` so a new ingress can't forget. See [`bug-report.md`](bug-report.md) B-02.

### [P3] B-15 — Bridged MCP argument-schema descriptions reach the model unframed — ARCHITECTURAL

`trust.go`'s sibling, the MCP description framing (`bridge.go:149-202`), correctly frames + length-caps the tool's top-level description, but the per-argument `inputSchema` property descriptions are passed through raw (`bridge.go:140-143`) and printed verbatim by `tool_search` (`search.go:177`), outside any untrusted envelope. A hostile/compromised mounted MCP server can pack injection text into a property description. MCP servers are operator-configured (lower likelihood), so P3. **Fix:** cap+frame the argument descriptions too, or document MCP schemas as trusted-by-mount.

---

## 2. Secret handling

### [P1] O-04 — No boot-time secret validation; no secret-redacting log handler — IMPLEMENTATION

Only the LLM API key fail-fasts at boot; empty `NEO4J_PASSWORD`/`POSTGRES_PASSWORD` surface as late dial failures. `SanitizeString` exists for the AG-UI wire but is **not** applied to the daemon's `slog` error lines, which can embed a DSN (`serve.go:130` logs raw `err`). **Fix:** `Config.Validate()` at boot + a `SanitizeString` `ReplaceAttr` on the default log handler. See O-04 in the bug report.

### [P2] B-09 — Divergent `secretEnvKey` blocklists; shell variant leaks bare `*_KEY` — IMPLEMENTATION

Three implementations of the same concept. `internal/mcp/client.go:164` includes the marker `"key"`; `internal/agent/tools/shell_exec_env.go:22` does not — so `shell_exec`'s child-env redaction passes `PRIVATE_KEY`, `SIGNING_KEY`, `SSH_KEY`, `ENCRYPTION_KEY`, `STRIPE_KEY` through (they contain `key` but not `api_key`). MCP uses a strict allowlist (correct, stronger); shell uses a leaky denylist. Low isolation impact (under the full-terminal model the model can read the whole env via `env` — the denylist is preview hygiene), but the inconsistency gives a false sense of stripping. **Fix:** one `internal/secret.IsSecretEnvKey` with a canonical blocklist, called from all three sites.

### Verified good

- The model-facing shell/skill output preview is redacted (`redactModelPreview`) *before* the sidecar spill, so the spilled copy is also redacted.
- OTel spans never carry an `api_key` attribute (`tracing.go:122`, D-28).
- MCP child env uses an **allowlist** — only PATH/HOME/etc. inherit. This is the correct pattern; the shell denylist should converge toward it.

---

## 3. Subprocess & file safety

### Verified good (preserve)

- **`fs_edit` empty `old_string` rejected** (`fs_edit.go:52-54`) before any read — the prior file-corruption P1 is closed.
- **`send_file` workspace fence** (`send_file.go:119-148`) does `EvalSymlinks` on *both* root and target before the `filepath.Rel` containment check — symlink-escape resistant. The prior arbitrary-exfiltration risk is closed.
- **Sidecar id grammar** (`result.go:55-83`) is a strict allowlist `[A-Za-z0-9_-]` applied *before* `filepath.Join` — no traversal, no Windows ADS (`:`), no UNC; files are `0o600` (double-applied).
- **Subprocess output is ring/tail-capped** (`shell_exec.go:220-253`, `shell_bg.go:70-86`) with env knobs — the prior OOM risk is closed.
- **Process-group kill** on both OSes with `WaitDelay`; background shells have a running cap, prune-on-start, and a leak-clean `Shutdown`.

### [P2] B-10 — Destructive-shell gate is regex-bypassable and off by default — ARCHITECTURAL

`destructiveShellMatch` (`shell_exec_env.go:71-103`) is opt-in (empty default = disabled) and, when configured, is a line-level regex over the raw command — `rm -r -f`, `find . -delete`, `$(echo rm) -rf`, a Python `shutil.rmtree`, or "write to a file then run the file" all bypass it. It is a speed-bump against the model literally spelling a dangerous command, **not a containment boundary**. Consistent with the full-host-trusted-operator philosophy, but it must not be *relied on* as a sandbox. **Fix:** document it as advisory; for real containment, gate at the intent layer (`ask_user`) or route untrusted commands through the named sandbox escalation; ship a conservative default pattern set so it's on.

### [P3] B-16 — `fs_grep`/`fs_glob` have no node/time budget — IMPLEMENTATION

`WalkDir` ignores ctx, so `path:"/"` with a rare pattern walks the whole filesystem (`budget.NodeTimeout` cancels the goroutine but not the walk). Self-DoS only. **Fix:** a node-count/deadline cap the walk checks.

---

## 4. Self-extension persistence

### [P1] B-04 — Self-extension gate open for `always:false` skills; the contract lies — ARCHITECTURAL + IMPLEMENTATION

This is the headline security/contract finding. A P5 (2026-06-10) policy change made model-authored `skill create`/`update` with `always:false` **bypass the gate and auto-activate** (`StatusActive`, `ApprovalAuto`), and `action=delete` de-materialize immediately (`writer.go:94-148`). The decision is defensible under the "container = boundary, Claude-Code parity" philosophy — but:

- The model-facing tool schema (`skill.go:99-112`) and doc comments still state changes "require explicit human approval … you cannot approve your own changes." **The contract actively misleads** the model and any auditor.
- The headless `Alerter` only fires on the now-unreachable pending fallback, so the operator gets **no signal** when the model self-extends in-box. The compensating control R-09 promised is gone.
- The only remaining barrier on create/update is the static injection blocklist (`ValidateForWrite`) — a string blocklist, not a semantic gate.

**Fix:** (a) correct the schema + comments to the true policy; (b) fire the alert/audit row on the ungated path; (c) reconsider whether unattended `delete` is intended. This is not asking to re-gate (the product decision stands) — it's asking the contract and the audit trail to tell the truth.

### Verified good

- The model-facing `task` and `skill` routers no longer expose `approve` — the prior "model self-approves its own destructive task" path is closed (R-10).
- `always:true` skills (standing instructions that auto-load every turn) *do* stay gated — the highest-blast-radius case is still human-approved.

---

## 5. Audit trail

The tool-invocation ledger (`runner_persist.go:76-81`) is append-only and redacted, but **best-effort** — a ledger insert failure logs and continues (R-26, tracked). It is observability, not a pre-execution permission gate. When the write-ahead intent log (B-01 fix) lands, it should double as the audit gate, upgrading R-26 from tracked to closed.

---

## Summary table

| ID | Finding | Class | Severity | Status |
|---|---|---|---|---|
| B-02 | Swarm reports bypass the untrusted envelope | ARCHITECTURAL | P1 | OPEN |
| B-04 | Self-extension gate open + lying contract + lost alert | ARCH+IMPL | P1 | OPEN (policy intentional, contract/alert are bugs) |
| O-04 | No boot secret validation; no log redaction handler | IMPL | P1 | OPEN |
| B-09 | Divergent `secretEnvKey`; shell leaks `*_KEY` | IMPL | P2 | OPEN |
| B-10 | Destructive gate regex-bypassable + off by default | ARCHITECTURAL | P2 | OPEN (advisory by design — document) |
| B-15 | Unframed MCP argument-schema descriptions | ARCHITECTURAL | P3 | OPEN |
| B-16 | `fs_grep`/`fs_glob` no node budget | IMPL | P3 | OPEN |
| R-01 | Untrusted output envelope (direct feeders) | ARCHITECTURAL | P0 | **CLOSED** |
| R-03 | `fs_edit` empty `old_string` | IMPL | P1 | **CLOSED** |
| R-18 | `send_file` workspace fence | IMPL | P2 | **CLOSED** |
| R-10 | Model self-approves destructive task | IMPL | P1 | **CLOSED** |
| R-23/24/43 | Sidecar id grammar + perms | IMPL | P2/P3 | **CLOSED** |
