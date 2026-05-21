# Phase-TOOL — Tool Surface Reduction

**Status:** 🟡 queued after Phase-CONS
**Provenance:** picobot scout (#1, #2), nanobot scout (#12), online 2026 scout (§2 Anthropic "Writing tools for agents")
**Estimated effort:** ~2 sessions
**LOC delta:** -1250

---

## Why this phase

Aura ships ~22 tools today. Online research (Anthropic) confirms MCP tool-bloat is the dominant 2026 anti-pattern — metadata = 40-50% of context. picobot's kitchen-sink action-enum pattern is the highest-LOC-delta lever for surface reduction. Done correctly (per ANALYSIS-DEEP.md §1.1), it doesn't conflict with Anthropic's "namespacing matters" rule — collapse only same-resource ops, NOT different stores.

Plus: picobot's `os.Root` sandboxing deletes a CVE class + ~150 LOC of validation helpers. Lands FIRST per ANALYSIS-DEEP.md §2.3.

---

## Stories

### US-TOOL-01 — `os.Root`-anchored sandboxing

- **Scope:** Open `os.Root` at workspace + wiki + sources + skills directories at startup. Route all filesystem operations through rooted FDs. Kernel-enforced via `openat()`. Replaces userspace path-traversal regex, removes symlink-escape vulnerability, removes TOCTOU race.
- **Files:** MODIFY [internal/agent/tools/registry/workspace_files.go](internal/agent/tools/registry/workspace_files.go), [internal/agent/tools/registry/wiki_path.go](internal/agent/tools/registry/wiki_path.go), [internal/wiki/store.go](internal/wiki/store.go), [internal/storage/sources/store/](internal/storage/sources/store/), [internal/skills/loader.go](internal/skills/loader.go). DELETE [internal/agent/tools/registry/workspace_validation.go](internal/agent/tools/registry/workspace_validation.go) (or equivalent).
- **LOC delta:** -150 (validation helpers + workspace_validation file go away).
- **Acceptance:**
  - `go test ./internal/agent/tools/... ./internal/wiki/... ./internal/storage/sources/...` green.
  - Symlink-escape test: create symlink to /etc/passwd inside workspace → read fails with `openat`-level error.
  - Path-traversal test: `..` in path → fails at OS layer, not application layer.
- **Provenance:** picobot `internal/agent/tools/filesystem.go:14-30`, `loop.go:82-86`.
- **Dependency:** Go 1.24+ confirmed (`go.mod` check first).

### US-TOOL-02 — Collapse `source_*` (6 tools → 1 with action enum)

- **Scope:** Collapse `source_store + source_read + source_list + source_delete + source_ocr + source_ingest` into ONE `source` tool with `action: enum{store,read,list,delete,ocr,ingest}`. `source_unified.go` already exists but is one of MANY source tools the LLM sees — make it the only one, delete the leaves.
- **Files:** MODIFY [internal/agent/tools/registry/source_unified.go](internal/agent/tools/registry/source_unified.go) (becomes the canonical entrypoint, gains ~80 LOC of dispatch glue). DELETE [internal/agent/tools/registry/source_store.go](internal/agent/tools/registry/source_store.go), [source_read.go](internal/agent/tools/registry/source_read.go), [source_list.go](internal/agent/tools/registry/source_list.go), [source_delete.go](internal/agent/tools/registry/source_delete.go), [source_ocr.go](internal/agent/tools/registry/source_ocr.go), and consolidate any remaining source_*.
- **LOC delta:** -700.
- **Acceptance:**
  - Tool registry contains exactly ONE `source` tool.
  - Probe: each action (store/read/list/delete/ocr/ingest) → behavior identical to pre-collapse.
  - golangci-lint clean + dupl clean on touched files.
- **Provenance:** picobot `filesystem.go`, `cron.go`, `exec.go` kitchen-sink pattern.
- **Risk:** LLM tool selection accuracy on enum vs. distinct names. Mitigation: probe BEFORE collapsing the leaves; if regression >5% on tool-selection accuracy, refine descriptions before deleting.

### US-TOOL-03 — Collapse `scheduler_*` (3 tools → 1 with action enum)

- **Scope:** Collapse `schedule_task + list_tasks + cancel_task` into ONE `task` tool with `action: enum{schedule,list,cancel}`. Matches picobot's `cron` shape directly.
- **Files:** NEW or MODIFY [internal/agent/tools/registry/scheduler.go](internal/agent/tools/registry/scheduler.go) (becomes single `task` tool). DELETE separate leaf files.
- **LOC delta:** -150.
- **Acceptance:**
  - Tool registry has ONE `task` tool.
  - Probe: each action works.
- **Provenance:** picobot `cron.go:25-62`.

### US-TOOL-04 — Collapse `wiki_*` triple (3 path/graph tools → 1 with action enum)

- **Scope:** Collapse `wiki_path + recall_god_nodes + wiki_subgraph` into ONE `wiki` tool with `action: enum{path,godnodes,subgraph,read,write,search}`. Keep `read/write/search` as actions (existing `wiki` tool already has action dispatch; extend it).
- **Files:** MODIFY [internal/agent/tools/registry/wiki.go](internal/agent/tools/registry/wiki.go). DELETE separate path/godnodes/subgraph tools if standalone.
- **LOC delta:** -200.
- **Acceptance:**
  - Tool registry has ONE `wiki` tool with all actions.
  - Probes for each action pass.
- **Provenance:** picobot pattern. Aura Phase-WIKI-A already created `recall_god_nodes` + `wiki_path` as separate tools — those collapse into `wiki(action=...)` here.

### US-TOOL-05 — Per-tool `read_only` / `concurrency_safe` / `exclusive` flags

- **Scope:** Add 3 boolean properties to the `ToolDefinition` shape:
  - `read_only` — side-effect free, safe to parallelize
  - `concurrency_safe = read_only && !exclusive` — automatic
  - `exclusive` — must run alone even under parallel dispatch
  - Executor two-phases the batch: `parallel := []`, `serial := []`; run parallel concurrently, then exclusive ones one at a time.
- **Files:** MODIFY [internal/agent/tools/registry/definition.go](internal/agent/tools/registry/definition.go); MODIFY [internal/agent/executor.go](internal/agent/executor.go); SET flags on each existing tool (sweep).
- **LOC delta:** +70 + 30 tests.
- **Acceptance:**
  - `workspace_write`, `wiki(action=write)`, `source(action=store/delete/ocr/ingest)`, `execute_code` are flagged exclusive.
  - Test: batch of [exclusive, read_only, read_only, exclusive] runs in correct phases.
- **Provenance:** nanobot `agent/tools/base.py:154-167`.
- **Note:** This is the safety primitive for Phase-STREAM stream-time parallel dispatch (Codex #1). Without it, the kitchen-sink action-enum tools become race-prone when LLM batches them.

---

## Sequencing

US-TOOL-01 (os.Root) FIRST — smaller, isolated, removes validation cruft that US-TOOL-02..04 would have to refactor anyway. Then US-TOOL-02 → US-TOOL-03 → US-TOOL-04 (collapse waves, can be parallel commits if branches kept tight). US-TOOL-05 last (depends on stable tool surface from 02-04).

**One story = one commit.**

---

## Risks

- **R1 (US-TOOL-02..04)**: LLM tool selection regression on collapsed enums. Mitigation: probe BEFORE deleting leaves; bench tool-selection accuracy; rollback if >5% regression.
- **R2**: `os.Root` is Go 1.24+ only. Confirm `go.mod` minimum version. If Aura's go.mod is below, US-TOOL-01 blocks on a go.mod bump.
- **R3**: Description quality matters more after collapse — single tool's description carries multiple action explanations. Mitigation: Phase-CACHE US-CACHE-03 description ≤200 char audit applies; bend the rule per-action-enum-tool to 400 chars if needed (justified one-shot exception).

---

## Verification

- `go test ./...` green.
- Tool registry contains the expected reduced count (~10 tools after all collapses).
- Probe suite (`cmd/probe_chat`) passes; tool-selection accuracy maintained.
- `golangci-lint run ./internal/agent/tools/...` clean.
- File count drops in `internal/agent/tools/registry/` — net deletion visible.

---

*Updated 2026-05-21.*
