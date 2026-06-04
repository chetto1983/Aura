---
phase: 16
slug: mcp-sidecar-manager-third-party-trust
status: planned
nyquist_compliant: true
created: 2026-06-04
---

# Phase 16 - Validation Strategy

## Test Infrastructure

| Property | Value |
|---|---|
| Framework | Go unit tests, httptest, temp config files, fake command runners |
| Quick run | `go test ./internal/mcp/ ./internal/agent/mcptools/ ./cmd/aura/` |
| Full run | `go vet ./... && go build ./... && go test ./...` |
| Live tier | WhatsApp/mail/calendar/Docker MCP optional operator checks |
| Runtime budget | CI tiers under 2 minutes, live tiers operator-run only |

## Sampling Rate

- After every implementation task: touched package tests.
- After every wave: `go test ./internal/mcp/ ./internal/agent/mcptools/ ./cmd/aura/`.
- Before phase verification: full build/test plus documented live tier skips/results.

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|---|---|---:|---|---|---|---|---|---|
| 01-T1 | 16-01 | 0 | MCP-V2-01/CAP-09 amendment | T-16-01 | Requirements, roadmap, and decisions explicitly include third-party trust boundaries | doc-gate | grep for CAP-09/MCP-V2-01/trust classes | planned |
| 02-T1 | 16-02 | 1 | Config v2 | T-16-01, T-16-02 | Existing config migrates; blocked third-party cannot boot | unit | `go test ./internal/mcp/ -run 'TestManagedConfig|TestProfile|TestTrust'` | planned |
| 03-T1 | 16-03 | 1 | Profiles/catalog/recipes | T-16-03 | Recipes and profiles exclude credentials; Calendar fixture mode exists | unit | `go test ./cmd/aura/ -run 'TestMCP.*Recipe|TestMCP.*Profile|TestMCP.*Install'` | planned |
| 04-T1 | 16-04 | 1 | Status/doctor/logs | T-16-04 | Doctor gives actionable checks without leaking secrets | unit | `go test ./cmd/aura/ -run 'TestMCP.*Doctor|TestMCP.*Status|TestMCP.*Logs'` | planned |
| 05-T1 | 16-05 | 2 | Streamable HTTP | T-16-05 | HTTP MCP client handles initialize/session/tools/errors/timeouts | unit + httptest | `go test ./internal/mcp/ -run 'TestHTTP|TestStreamable'` | planned |
| 06-T1 | 16-06 | 2 | Sandboxed runtime | T-16-06 | Docker runtime has no host mounts by default and blocks unsafe local boot | unit | `go test ./internal/mcp/ ./cmd/aura/ -run 'TestDockerRuntime|TestTrustGate'` | planned |
| 07-T1 | 16-07 | 2 | Risk policy | T-16-07 | Destructive/unknown-risk tools are blocked before registry mount | unit | `go test ./internal/agent/mcptools/ ./internal/mcp/ -run 'TestRisk|TestPolicy|TestMount'` | planned |
| 08-T1 | 16-08 | 3 | E2E/docs/onboarding | T-16-08 | Mock stdio+HTTP E2E green; live tiers explicit, not skipped as green | build/e2e | `go test ./cmd/aura/ ./internal/mcp/ ./internal/agent/mcptools/` | planned |

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation |
|---|---|---|---|---|
| T-16-01 | Elevation of privilege | New third-party local command | mitigate | Trust class defaults to `blocked`; chat boot filters blocked servers before launch. |
| T-16-02 | Information disclosure | Exported profile with credentials | mitigate | Export excludes env values/secrets; tests assert redaction. |
| T-16-03 | Supply chain | Built-in/custom recipe sources | mitigate | Built-in recipe metadata records source/trust; custom entries require explicit trust. |
| T-16-04 | Information disclosure | Doctor/log output | mitigate | Redact env-looking secrets and tokens. |
| T-16-05 | Spoofing/session confusion | Streamable HTTP | mitigate | Protocol header, session-id handling, localhost/auth guidance, error handling tests. |
| T-16-06 | Sandbox escape/host access | Dockerized local runtime | mitigate | No host mounts by default, explicit mounts only, resource limits, generated-args tests. |
| T-16-07 | Tool abuse | Destructive/unknown-risk tools | mitigate | Risk policy blocks before mount; unknown risk is visible and deny-by-default where configured. |
| T-16-08 | Availability | Broken MCP server | mitigate | Preserve fail-soft boot and `doctor --all` diagnostics. |

## Manual-Only Verifications

| Behavior | Why Manual | Instructions |
|---|---|---|
| WhatsApp bridge/session health | Needs paired account and WSL bridge | Start bridge, run `aura mcp doctor whatsapp`, record REST/connected-state output. |
| Mail recipe auth | Needs private SMTP/IMAP credentials | Configure env, run `aura mcp doctor mail`, verify no secrets are printed. |
| Calendar live account | External provider auth | Run only after fixture mode passes. |
| Docker MCP Gateway integration | Depends on Docker Desktop MCP Toolkit version | If installed, connect an Aura profile to Docker gateway and run mock tool census. |

## Sign-Off

- [ ] Every plan has an automated verification path.
- [ ] Live checks are marked operator-only.
- [ ] No third-party local command runs at chat boot without trust.
- [ ] Blocked/destructive tools are absent from the runtime registry.
- [ ] Secrets are redacted in status, doctor, logs, and exports.
