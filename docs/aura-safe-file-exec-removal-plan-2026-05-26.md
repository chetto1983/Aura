# Aura Safe File Surface And Exec Removal Plan

Role: phase-plan
Date: 2026-05-26

## Goal

Remove `execute_code` and `execute_shell` from Aura's LLM-facing/default runtime
surface, then make `file(action=...)` cover the safe filesystem operations that
previously pushed the model toward shell usage.

## Sources

- PRD active route: `D:/Aura/PRD.md`, post-DRIFT Phase-TOOL/CTX/OUT notes.
- Local state: `D:/Aura/scripts/ralph/progress.txt`, latest OH1 and DOCSKL
  shipped entries.
- Current slice files: `cmd/aura/app_wire.go`,
  `internal/agent/tools/registry/file.go`,
  `internal/agent/tools/registry/workspace_files.go`,
  `internal/workspace/root.go`, `internal/workspace/operations.go`,
  `internal/tokenjuice/reduce.go`, `cmd/probe_chat/cases.go`.
- External reference: Anthropic, "Effective context engineering for AI agents"
  (2026 context-engineering guidance: keep context lean, use tools
  just-in-time, preserve high-signal state rather than dumping raw history).
- Example paths:
  - `D:/tmp/picobot/internal/agent/tools/filesystem.go`: workspace-rooted file
    operations instead of broad shell for common file tasks.
  - `D:/tmp/picobot/internal/agent/tools/exec.go`: shows why allowlisted exec is
    still a separate risk surface even when guarded.
  - `D:/tmp/hermes-agent/AGENTS.md`: skill prose maps shell utilities to native
    tools (`grep` to search, `cat/head/tail` to read, `sed/awk` to patch).
  - `D:/tmp/openhuman/src/openhuman/tokenjuice` and README TokenJuice notes:
    compress noisy terminal/tool payloads, but do not destroy high-signal
    structured evidence.

## Alternatives

1. Hard-delete `execute_code` and `execute_shell` implementation now.
   - Rejected for this slice: too much blast radius in sandbox health, artifact
     tests, historical probes, and terminal finalization.
2. Keep implementation internal but stop registering/exposing it by default.
   - Adopted: removes model access immediately while preserving compile safety
     and letting dead-code cleanup follow after E2E proof.
3. Replace shell with a new `safe_shell` command whitelist.
   - Rejected: still teaches command execution and reintroduces quoting/path
     hazards under a different name.
4. Expand `file` with bounded actions.
   - Adopted: `grep`, `path_info`, `mkdir`, `rmdir`, `remove_file`, `move`,
     `copy`, `walk`, and `pwd` cover the shell-shaped filesystem workflow while
     staying inside `workspace.Root` and the Aura denylist.

## Slice 1 Contract

- `execute_code` and `execute_shell` are absent from the default container
  allowlist and app registration.
- The default prompts no longer recommend execution tools.
- Toolsets no longer expose a `sandbox_code` bundle.
- `file` extended operations pass unit tests and deny sensitive paths.
- TokenJuice remains terminal-output focused and preserves structured Aura tool
  payloads.
- Probe cases stop expecting exec tools and add safe-file replacements.

## Verification

Targeted:

```powershell
go test ./internal/workspace ./internal/agent/tools/registry ./internal/agent/tools/sets ./internal/tokenjuice ./internal/agent -count=1
go test ./cmd/probe_chat -count=1
```

Broader gate if targeted passes:

```powershell
go vet ./...
go build ./...
go test ./...
```

Live Q&A after container update:

- Ask Aura to create/read/grep/move/remove a workspace file.
- Ask Aura to inspect a large prior conversation and answer without losing
  load-bearing details.
- Verify `tool_attempts` has `file` and no `execute_code`/`execute_shell`.

## Results

- 2026-05-26: removed `execute_code` and `execute_shell` from app registration,
  default allowlist, prompt guidance, and the `sandbox_code` toolset.
- 2026-05-26: `file(action=...)` covers safe shell-shaped filesystem work:
  `grep`, `path_info`, `mkdir`, `rmdir`, `remove_file`, `move`, `copy`, `walk`,
  and `pwd`.
- 2026-05-26: targeted tests passed:
  `go test ./internal/workspace ./internal/agent/tools/registry ./internal/agent/tools/sets ./internal/tokenjuice ./internal/agent -count=1`,
  `go test ./cmd/probe_chat -count=1`, `go test ./cmd/aura -count=1`,
  `go test ./internal/api -count=1`, `go test ./internal/conversation -count=1`.
- 2026-05-26: broad gates passed: `go vet ./...`, `go build ./...`,
  `go test ./... -count=1`.
- 2026-05-26: rebuilt and restarted `aura:local`; container is healthy.
- 2026-05-26: live Q&A `tool-file-safe-ops` passed with `file=7`,
  `execute_code=0`, `execute_shell=0` since probe start.
- 2026-05-26: live Q&A `web-fetch-summarize-context-engineering` passed with
  isolated thread, `web` fetch, and context-engineering synthesis.
