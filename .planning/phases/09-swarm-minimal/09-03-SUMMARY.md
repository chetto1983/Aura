---
phase: 09-swarm-minimal
plan: 03
subsystem: infra
tags: [mcp, mcptools, deferred-tools, allowlist, fail-soft, slog, mail-mcp, whatsapp-mcp, recipes]

# Dependency graph
requires:
  - phase: 08.1-tool-search-hardening
    provides: "mcptools.Bridge/Mount/MountServer 8.1 namespacing + BM25 tool_search (indexes name+description+arg fields) + tools.Spec.Deferred + Registry.Validate non-deferred guard"
  - phase: 09-swarm-minimal (plan 01)
    provides: "PRD-amendment-gate locking D-19/D-20/D-21 (swarm v1 shape, managed-config registration path, no AURA_MCP_*_SERVER env vars)"
provides:
  - "Bridged MCP tools mount Deferred:true (out of the 30-50-tool manifest degradation zone; discoverable via tool_search)"
  - "Per-server tool allowlist threaded through Bridge/Mount/MountServer — footgun tools dropped before adaptation (nil/empty = mount all)"
  - "Fail-soft MCP boot: a dead/misconfigured server WARN-and-drops, boot continues (buildRegistryWithMCP no longer aborts aura chat)"
  - "mcpAllowlist resolver (mail/whatsapp v1 tool sets) wired into the boot mount loop"
  - "mail (martinzarfl/mail-mcp) + whatsapp (chetto1983/whatsapp-mcp fork) recipes installable via `aura mcp install`"
affects: [09-swarm-minimal swarm_spawn worker registry, 09 live E2E (mail/whatsapp read-back), Phase 16 risky-tool labeling]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Deferred-by-default for bridged MCP tools (manifest protection via tool_search, mirrors the built-in deferred-tool rule)"
    - "Per-server allowlist filter at the bridge seam (drop-before-adapt; nil/empty = back-compat mount-all)"
    - "Fail-soft per-server boot (slog.Warn + continue), never a supervisor/ticker (mini-PC no-busy-poll, lifecycle-study locked model)"

key-files:
  created: []
  modified:
    - "internal/agent/mcptools/bridge.go - Deferred:true flip + allow []string filter in Bridge/Mount + rewritten doc-comment"
    - "internal/agent/mcptools/mount.go - MountServer threads the allow []string param"
    - "internal/agent/mcptools/mount_test.go - TestMountAllowlistDeferred (filter + Deferred flip)"
    - "internal/agent/mcptools/bridge_test.go - 10 call sites updated to pass nil; inverted Deferred assertion rewritten"
    - "cmd/aura/main.go - buildRegistryWithMCP fail-soft + mcpAllowlist resolver + log/slog import"
    - "cmd/aura/main_test.go - TestBuildRegistryFailSoft"
    - "cmd/aura/mcp.go - mail + whatsapp recipes"

key-decisions:
  - "Allowlist signature = optional `allow []string` (Discretion) — nil/empty mounts all (calculator back-compat); a non-empty allow filters by RAW tool name before adaptation."
  - "Allowlist resolver lives in main.go as a small `switch` (mcpAllowlist) — mail/whatsapp get the v1 sets, every other server gets nil. No new env vars (D-21 supersedes D-23's env-add list)."
  - "mail recipe = `npx -y github:martinzarfl/mail-mcp` (Node analog of the calculator's `uvx --from <git>` self-contained-source pattern); SMTP/IMAP creds ride managed-config Env placeholders, not git."
  - "whatsapp recipe = `wsl.exe -e bash -lc '... uv run main.py'` against the chetto1983/whatsapp-mcp fork (WSL topology validated in spike 002); same fork-trust pattern as recipe:calculator."

patterns-established:
  - "Bridged MCP tools are Deferred:true — the manifest stays under the BM25 tool_search degradation threshold; full specs load on demand."
  - "Per-server allowlist at Mount — destructive MCP footguns never reach a worker."
  - "Fail-soft boot per server (WARN-and-drop) — one dead sidecar cannot kill aura chat; the non-deferred built-ins keep the registry valid even when every MCP server is dropped."

requirements-completed: [CAP-03]

# Metrics
duration: 10min
completed: 2026-06-04
---

# Phase 9 Plan 03: First Production MCP Mount (D-19/D-20/D-21) Summary

**Bridged MCP tools now mount Deferred:true behind a per-server footgun allowlist, and a dead MCP server WARN-and-drops instead of aborting `aura chat`; mail + whatsapp recipes point at the spike-validated forks.**

## Performance

- **Duration:** ~10 min
- **Started:** 2026-06-04T09:54Z (base commit)
- **Completed:** 2026-06-04T10:04Z
- **Tasks:** 2 (both TDD)
- **Files modified:** 7

## Accomplishments
- **D-20:** Every bridged MCP tool's Spec is now `Deferred:true` (a 16-tool mail + 12-tool whatsapp surface no longer floods the per-turn manifest into the 30-50-tool degradation zone the 8.1 BM25 `tool_search` defends). The justifying doc-comment that *argued* non-deferred was rewritten to state the spike rationale.
- **D-20:** An optional `allow []string` per-server allowlist is threaded through `Bridge` → `Mount` → `MountServer`; tools whose RAW name is not in `allow` are dropped before adaptation (footguns `delete_mailbox`/`move_message`/`create_mailbox`, spike-001 census). A nil/empty allowlist mounts all advertised tools (calculator back-compat).
- **D-21:** `buildRegistryWithMCP` is fail-soft — a per-server `MountServer` error now `slog.Warn`s and `continue`s instead of `closeMCPServers` + `return err`. Already-mounted servers stay registered; the fatal `cfg.MCPServersErr` config-parse path is preserved. The non-deferred built-ins keep `Registry.Validate` green even when every MCP server is dropped (Pitfall 6).
- **D-19:** `mail` (martinzarfl/mail-mcp) + `whatsapp` (chetto1983/whatsapp-mcp@6de1dcd fork) recipes added to `mcpRecipes`, installable via `aura mcp install`. No `AURA_MCP_*_SERVER` boot env vars; no ping ticker / restart supervisor / lazy mount (D-21 non-goals).

## Task Commits

Each task was committed atomically (TDD: test+impl folded into one feat commit per task — signature changes failed to compile under the old call sites, so RED was a compile failure):

1. **Task 1: D-20 Deferred flip + allowlist through Bridge/Mount/MountServer** - `4fa87d37` (feat)
2. **Task 2: D-21 fail-soft boot + allowlist wiring + D-19 recipes** - `73a6dc66` (feat)

## Files Created/Modified
- `internal/agent/mcptools/bridge.go` - `Deferred:false`→`true` on every bridged tool; `allow []string` filter in `Bridge` (drop-before-adapt); `Mount` threads `allow`; doc-comment rewritten.
- `internal/agent/mcptools/mount.go` - `MountServer` gains the `allow []string` param, passes it to `Mount`.
- `internal/agent/mcptools/mount_test.go` - `TestMountAllowlistDeferred`: footgun-drop + nil/empty mount-all + every bridged Spec Deferred:true.
- `internal/agent/mcptools/bridge_test.go` - 10 `Bridge`/`Mount` call sites updated to pass `nil`; the inverted `if exec.Deferred` assertion rewritten to `if !exec.Deferred`.
- `cmd/aura/main.go` - `buildRegistryWithMCP` fail-soft (`slog.Warn`+`continue`); `mcpAllowlist(server)` resolver; `log/slog` import.
- `cmd/aura/main_test.go` - `TestBuildRegistryFailSoft`: a broken entry yields `err==nil` + a valid registry with the built-ins.
- `cmd/aura/mcp.go` - `mail` + `whatsapp` recipes pointing at the spike-validated forks; creds via managed Env.

## Decisions Made
- **Allowlist signature** = optional `allow []string` (Discretion granted the shape). nil/empty = mount all; a non-empty allow filters by RAW (wire) tool name before adaptation, so the model never even sees a dropped tool's namespaced name.
- **Allowlist resolver placement** = a small `switch` (`mcpAllowlist`) in `main.go` (not a new config knob) — mail/whatsapp get their v1 sets, all other servers get `nil`. This honors D-21/OQ1 (managed config is the registration path; no `AURA_MCP_*_SERVER` env vars).
- **mail recipe** uses `npx -y github:martinzarfl/mail-mcp` — the Node analog of the calculator's `uvx --from <git>` self-contained-source trust pattern (vs the spike's local `node D:/tmp/...` build, which is not reproducible for other operators). SMTP/IMAP creds are managed-config `Env` placeholders.
- **whatsapp recipe** uses `wsl.exe -e bash -lc '... uv run main.py'` against the chetto1983 fork — the exact WSL stdio topology validated in spike 002; same fork-trust pattern as `recipe:calculator → chetto1983/*`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated `bridge_test.go` call sites + rewrote the inverted Deferred assertion**
- **Found during:** Task 1 (Deferred flip + allowlist signature change)
- **Issue:** Adding `allow []string` to `Bridge`/`Mount` broke `bridge_test.go`'s 10 call sites (compile failure); and `TestBridge_TranslatesTools` asserted `if exec.Deferred { t.Fatal(...) }` — the exact behavior D-20 reverses. `bridge_test.go` is not in the plan's `files_modified`, but the build/test gate cannot stay green without it.
- **Fix:** Passed `nil` for the new allow arg at all 10 sites; rewrote the assertion to `if !exec.Deferred` with a justifying message (per CLAUDE.md "NEVER MODIFY TESTS TO MAKE THEM PASS unless the test itself is broken" — this test asserted the now-superseded contract).
- **Files modified:** internal/agent/mcptools/bridge_test.go
- **Verification:** `go test ./internal/agent/mcptools/` green (incl. the rewritten assertion); `go test -race` green.
- **Committed in:** 4fa87d37 (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking).
**Impact on plan:** The `bridge_test.go` touch is mechanical (signature back-compat) plus one justified assertion rewrite required by the intended D-20 behavior change. No scope creep; no production-file changes beyond the plan's `files_modified`.

## Issues Encountered
- The plan's `files_modified` listed `cmd/aura/main.go` for Task 2; Task 1's signature change required a transient `nil` placeholder at `main.go:121` to keep the build green between tasks. That line was then replaced by the real `mcpAllowlist(name)` call in Task 2 — `main.go` was staged/committed only in Task 2.
- Race tier ran natively green on Windows (no toolchain fallback needed). MCP integration/live-mount tiers are out of scope here (Wave 3 / 09-06, operator-gated).

## Verification Evidence
- `go build ./...` clean (all `Mount`/`MountServer`/`Bridge` call sites updated).
- `go test ./cmd/aura/ ./internal/agent/mcptools/` green; `go test -race` green on both.
- Acceptance greps: `Deferred:    true` ×1 / `Deferred:    false` ×0 in bridge.go; `allow` in all three signatures; `recipe:mail` ×1 + `recipe:whatsapp` ×1; `AURA_MCP_MAIL_SERVER`/`AURA_MCP_WHATSAPP_SERVER` absent under `cmd/aura/`; `slog.Warn` + no in-loop `return nil, nil, err`; both allowlist literal sets present; no `time.Ticker`/`supervisor`.

## Known Stubs
None. The recipe `Env` SMTP placeholders (`CHANGE_ME_app_password`, `you@example.com`) are intentional operator-edit-after-install defaults (managed config, not git) — mirroring the existing CLI `aura mcp add --env` contract; they are not data-flow stubs.

## Next Phase Readiness
- The mcptools seam is now swarm-ready: workers inherit Deferred MCP tools (no manifest bloat) with footguns pre-filtered, and a dead bridge cannot abort the parent boot. The `swarm_spawn` worker registry (09-05) and the live mail/whatsapp E2E (09-06) build on this.
- Operator pre-req for the live tier: register `mail`/`whatsapp` via `aura mcp install`, edit the SMTP Env, and bring up the whatsmeow bridge (REST :8080) — documented in spikes 001/002.

## Self-Check: PASSED

All modified production files exist; both task commits (`4fa87d37`, `73a6dc66`) are in the worktree history.

---
*Phase: 09-swarm-minimal*
*Completed: 2026-06-04*
