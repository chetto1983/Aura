# Aura — Verify-Work Runtime Findings (Phase 38 UAT session)

> **Historical source register.** Current `VW-*` dispositions are reconciled in
> [definitive-closure-ledger-2026-07-31.md](definitive-closure-ledger-2026-07-31.md).

Audit date: 2026-07-18

Audited repository: `d:\Repo\Aura` (live stack: freshly rebuilt `aura:local` + Postgres 18.4 + Neo4j 5.26 + sidecars, `AURA_IN_CONTAINER=1`).

## Source & method

Unlike [operator-reported-runtime-bugs-2026-07-18.md](operator-reported-runtime-bugs-2026-07-18.md) (bugs Aura recorded in its own memory), this file records defects **found by exercising Aura during the Phase 38 UAT / verify-work session** — real mounts of the GitHub MCP server + calculator/memory/calendar/whatsapp, deliberate hung-server / straggler / hung-probe injections, and the governance audit-ledger E2E. Findings are cross-checked against the live container and, where fixed, the fix is cited. No speculative claims: where a root cause is unconfirmed it is labelled as such.

## Findings summary

| ID | Finding | Verdict | Severity | Locus |
|----|---------|---------|----------|-------|
| VW-01 | Windows `aura.exe` boot hangs indefinitely on a never-responding stdio MCP server (ignores `AURA_MCP_MOUNT_TIMEOUT`) | **VALID (Windows dev-host only)** — root cause UNCONFIRMED; not reproduced in isolation; Linux prod path is clean | Medium (dev-host) / — (prod) | `cmd/aura/main.go` boot mount vs Windows `os/exec`+Docker pipe semantics |
| VW-02 | Scheduler `TestUpdateTask_RescheduleAndPayload` byte-compares a jsonb round-trip → spurious CI red | **VALID, FIXED today** | Low (test-only) | `internal/cron/store_manage_test.go:145` (fixed `e8c1fa39`) |
| VW-03 | `aura mcp tools` / `aura mcp doctor` probe uses a hardcoded 20s ctx, not the `AURA_MCP_*_TIMEOUT` knobs the boot path honors | **VALID (minor consistency)** | Low / Info | `cmd/aura/mcp_tools.go:78,93` (`openAndListMCPTools`/`openAndListManagedMCPTools`) |
| VW-04 | `db_integration` test class is invisible to the pre-push hooks → a broken db-integration test only surfaces in the Skills/CI gate ~20 min post-push | **VALID (process/observability gap)** | Low | `.git` hooks vs `.github/workflows/skills.yml` |

Positive validations (no defect): the three Phase-38 deadline mitigations (T-38-07/08/10b) and the governance audit ledger (T-38-11/13, append-only) were **LIVE-PROVEN** in the container — see [.planning/phases/38-mcp-governance-hardening/38-UAT.md](../../.planning/phases/38-mcp-governance-hardening/38-UAT.md) and `38-SECURITY.md`. Phase 38 also **closes R-021** (mixed url+command trust bypass) from `audit-index.json` via `internal/mcp/classify.go` (T-38-02).

---

## VW-01 — Windows `aura.exe` hangs on a never-responding stdio MCP server — **VALID (dev-host), root cause UNCONFIRMED**

### Observed

On Windows (`aura.exe`, w64), a stdio MCP server that spawns but never completes the MCP handshake makes `aura tools` (the boot mount path `buildRegistryWithMCP`) **hang for the full external timeout** (15–25 s observed, killed by an outer `timeout`), rather than dropping the server at `AURA_MCP_MOUNT_TIMEOUT`. Reproduced with two independent "hung" commands:

- `docker run -i --rm alpine sleep 3600` (spawns, never speaks MCP) — container left `Up` (not reaped).
- bare `sort` (reads stdin, never emits stdout) — same hang.

Repro (Windows):
```
AURA_MCP_SERVERS_JSON='{"mcpServers":{"hung":{"command":"sort"}}}' \
AURA_MCP_MOUNT_TIMEOUT=3 AURA_MCP_MOUNT_RETRY_ATTEMPTS=1 \
timeout 25 ./aura.exe tools    # → rc=124 (hung), no "mcp mount failed" log, no built-ins rendered
```

### Contradicting isolated evidence (why the root cause is UNCONFIRMED)

Every isolated reproduction on the **same Windows binary** honored the deadline:

- `mcp.OpenWithHandshakeContext(processCtx, handshakeCtx=3s, "hung", {Command:"sort"})` returned at **3.12 s** with `initialize: recv timeout: context deadline exceeded`. `readResponseContext` (`internal/mcp/client.go:373-397`) correctly runs the blocking read in a goroutine and aborts on `ctx.Done()`.
- `buildRegistryWithMCP(ctx.Background(), cfg{MCPServers:{hung:sort}}, ...)` (hand-built config, single server, no default-on memory) returned at **3.16 s**, dropping the server (`WARN mcp mount failed ... mount_timeout=3s`).
- The **Linux container** (`aura tools` inside the rebuilt image, `AURA_MCP_MOUNT_TIMEOUT=3`, hung server = `sleep 3600` alongside the 4 real servers) dropped the hung server at the **exact 3 s** deadline, reaped the subprocess (`no lingering sleep`), and mounted the healthy servers — `rc=0`, no hang.

The hang appeared only in the **full Windows `aura.exe` boot with multiple servers in the config** (the hung server was the 2nd mount, with the default-on `memory` streamable-HTTP server still pending). The single-server isolated unit did not reproduce it.

### Hypothesis (unverified)

Candidate: on Windows, `os/exec` stdin-pipe write or the process-group kill (`internal/procgroup/procgroup_windows.go`, `taskkill /F /T`) does not unblock the stdout `Scanner` goroutine promptly when a mount is in flight alongside other pending servers, so `readResponseContext`'s 100 ms abort-drain window (`client.go:391-394`) is entered but the underlying pipe read never returns and something upstream serializes on it. This is speculation — **it must be confirmed with a Windows goroutine dump at hang time** (`SIGQUIT`-equivalent / Delve `goroutines`), not asserted.

### Impact

- **Production (Linux container): NONE** — the shipped path drops correctly at the deadline (live-proven).
- **Windows dev-host: Medium** — `aura tools` / any boot on Windows can hang if a configured stdio MCP server is dead-but-spawning. Windows is a secondary dev target (`docs/` marks WSL/Linux as primary), so this is a developer-ergonomics defect, not a deploy risk.

### Fix direction

1. First, **capture a Windows goroutine dump at hang** to confirm the blocked frame (do not fix blind).
2. If confirmed as an un-cancellable stdin write / stdout read: give the boot mount an OS-level hard deadline that force-closes the child's pipes (not just cancels the ctx) on Windows, mirroring `abortTransport`'s intent.
3. Add a Windows-tagged regression test that boots `buildRegistryWithMCP` with a hung server + a second pending server and asserts a bounded return.

---

## VW-02 — Scheduler jsonb payload byte-compare — **VALID, FIXED (`e8c1fa39`)**

`internal/cron/store_manage_test.go` (the Phase-adjacent scheduler-cockpit feature, commit `e183e7a3`) asserted the round-tripped task payload **byte-for-byte** against the literal `{"text":"edited"}`. `payload` is a **jsonb** column (`0009_scheduler.up.sql:22`), which Postgres reserializes to canonical form `{"text": "edited"}` (space after the colon). The store persisted the value correctly; the exact-byte compare spuriously failed the Skills/CI gate (`--- FAIL: TestUpdateTask_RescheduleAndPayload`). Fixed by comparing the JSON **semantically** (unmarshal + re-marshal to compact form). `UpdateTask` / `UpdateTaskScheduleRow` are unchanged and correct.

Verified: single-test run PASS against a freshly migrated disposable DB; full CI green on `e8c1fa39`.

---

## VW-03 — CLI MCP inspection ignores the timeout knobs — **VALID (minor consistency)**

`aura mcp tools <name>` and `aura mcp doctor <name>` resolve their probe deadline from a **hardcoded 20 s** (`cmd/aura/mcp_tools.go:78` `openAndListMCPTools`, `:93` `openAndListManagedMCPTools`), whereas the boot mount honors `AURA_MCP_MOUNT_TIMEOUT` and `aura mcp status` / `doctor --all` honor `AURA_MCP_PROBE_TIMEOUT` (`mcp_status.go:103`). An operator tuning the knobs to diagnose a slow server will not see them reflected in the single-server `tools`/`doctor` inspection path. Arguably by-design (a generous fixed inspection budget), but the inconsistency is a diagnostic footgun. **Fix direction**: route `openAndListMCPTools`/`openAndListManagedMCPTools` through `resolveMCPProbeTimeout()` (or a dedicated inspection knob) so all operator-facing probe paths share one budget.

---

## VW-04 — `db_integration` invisible to pre-push — **VALID (process/observability gap)**

VW-02 (a broken db-integration test) passed every **pre-push** hook (`gofmt`, `file-size`, `vet`, `lint`, `build`, `deadcode`, `quality-snapshot`) and only surfaced in the **Skills gate** ~20 min after push, because the pre-push hook set does not run the `db_integration` tier (it needs a live Postgres). This matches the CLAUDE.md warning ("a green local full-matrix run is worth more than a push-and-wait CI cycle") but there is no local guard that reminds the author to run the tagged tiers before pushing a change under `internal/cron/**` / `internal/skills/**` / `cmd/aura/**`. **Fix direction**: a lightweight pre-push advisory (non-blocking) that, when changed files touch a `db_integration`-tagged package, prints "run `go test -tags db_integration ...` (needs stack up) before relying on CI" — or wire the disposable-DB pattern (`aura_verify`) into a `make test-db-integration` target.

---

## Prioritized action plan

| Prio | Item | Effort | Notes |
|------|------|--------|-------|
| **P2** | VW-01: capture a Windows goroutine dump at hang, then bound the boot mount with an OS-level pipe-close deadline on Windows | M | Dev-host only; do NOT fix blind — confirm the blocked frame first. Prod (Linux) is clean. |
| **DONE** | VW-02: jsonb semantic compare | — | Fixed `e8c1fa39`, CI green. |
| **P3** | VW-03: unify CLI MCP inspection on `resolveMCPProbeTimeout()` | S | Diagnostic consistency. |
| **P3** | VW-04: pre-push advisory for `db_integration`-tagged packages | S | Shortens the push→red feedback loop. |
