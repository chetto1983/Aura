# A run with no operator cannot erase memory — Implementation Plan

**Status (2026-10-30):** Tasks 0–3 done in a cloud container and merged to `master`; the lab-VM run
(step 0 and A4) is still owed. Approved by the operator on 2026-10-30 ("procedi con
l'implementazione della spec memory_forget") before the lab VM was available, so step 0's
deterministic part, the gateway's verdict, was measured in-process instead.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task by task. Steps use checkbox (`- [ ]`) syntax for tracking. One atomic commit per task.

**Goal:**
- `memory_forget` that removes something is `Destructive`: an interactive turn asks, a run with no
  responder is denied.
- `memory_forget` with `dry_run: true` stays a read: it counts and changes nothing.
- The server's annotation, the bridge's recipe table and the gateway agree.

**Architecture:**
- **mcptools.** `trustedRecipeActions[recipe:memory]["memory_forget"]` becomes
  `MCPActionDestructive`. A predicate `MemoryForgetDryRun(spec, raw)` lives beside the table,
  identifying the tool by `TrustedRecipeSource` and `TrustedRecipeTool`, the pattern
  `messagedrafts` and `serve_notify` already use.
- **gateway.** `classify` answers `Safe` for that predicate before the `Destructive` bit. Nothing
  else in `Decide` changes: `gated` and `routeApprove` already deny a Destructive call with no
  responder.
- **arcadedb-mcp.** `memory_forget` declares `DestructiveHint: true`, like `memory_batch`.

**Tech Stack:** Go (`internal/agent/mcptools`, `internal/gateway`, `cmd/arcadedb-mcp`), go-sdk MCP
annotations.

**Spec:** `docs/superpowers/specs/2026-10-30-headless-memory-erase-design.md`.

## Global Constraints

- **No migration, no env var, no dependency.**
- **Files stay ≤ 600 LOC.** The largest touched is `bridge_risk_test.go` at 457.
- **A test changes only for a stated reason.** Three existing tests encode "forget is an ordinary
  write"; each changes because the spec changes that contract, said in the commit.

## Review Focus

- **Fail closed.** Unparseable arguments, `dry_run` as a string, or a spec from another server
  named `memory_forget` must never reach `Safe`.
- **No interactive loop.** The dry run the tool's own description asks for first must not prompt.
- **Headless.** `Decide` with no responder on a strict profile denies the erasure with
  `degraded_deny(reason=no_approver)`.

## Decisions taken while writing this plan

- **The carve-out keys on recipe and raw tool, not on the namespaced name.** The memory mount's
  namespace is configuration (`defaultBridgePolicy`), while `TrustedRecipeSource` is assigned only
  by the bridge after classifying the mount, so another server cannot claim it.
- **A dry run stays in the mutating funnel at `Safe`,** as the calendar's reads do: `Destructive`
  forces `Mutating`, and the reservation of a count is harmless.
- **The table and the annotation both change.** The table is what Aura trusts; the annotation is
  what any other client of the server reads.

## File structure

| File | Change |
|---|---|
| `internal/agent/mcptools/bridge_risk.go` | `memory_forget` → `MCPActionDestructive`; `MemoryForgetDryRun`; the comment on memory writes. |
| `internal/agent/mcptools/bridge_risk_test.go`, `bridge_risk_recipes_test.go` | Expectations for `memory_forget`; the predicate's table. |
| `internal/gateway/classify.go` | The dry-run carve-out. |
| `internal/gateway/classify_test.go` | The carve-out's table. |
| `internal/gateway/injection_suite_test.go` | `memory_forget` leaves the ordinary cases; erasure cases use the bridge's real recipe fields; a headless-strict case. |
| `cmd/arcadedb-mcp/tool_forget.go` | `DestructiveHint: true`. |
| `cmd/arcadedb-mcp/tool_manifest_test.go`, `docs/arcadedb-mcp-live-tools.json` | The golden; an assertion that both erasing tools declare it. |
| `prd.md` §5 | The amendment recording the measurement and the change. |

## Tasks

### Task 0 — Record the measurement (PRD-first)
- [x] Probe, in-process, not committed (output in the verification file): the bridge builds
  `memory_forget` with the server's real annotation as `Mutating`, not `Destructive`, and
  `memory_batch` as `Destructive`. `Decide` on `single_user_hardened` with no responder answers
  `allow`, tier `normal`, reserved, no approval request.
- [x] `prd.md` §5: the measurement, the change, what it does not show. Spec status: approved.
- [x] Commit `docs(prd): measure that memory_forget runs ungated in a headless run`.

### Task 1 — A dry run is a read (D1's carve-out; acceptance A1, dry-run half)
- [x] Red: `classify` of a memory `memory_forget` with `dry_run: true` is `Safe`; without it, with
  `dry_run: false`, with `"true"` as a string, with broken JSON, and for another recipe's tool of
  the same name, it is not.
- [x] Green: `mcptools.MemoryForgetDryRun`; the branch in `classify`. The key is read exactly
  from a map: go-sdk v1.8.0 decodes case-sensitively, so a struct decode with encoding/json would
  have taken `DRY_RUN` as a dry run the server never sees.
- [x] Commit `feat(gateway): a memory_forget dry run is a read`.

### Task 2 — Erasing memory is Destructive (D1, D2; acceptance A1–A3)
- [x] Red: the bridge classifies `memory_forget` `(true, true)` with nil, lying and read-only
  annotations (red with `DestructiveHint: false`). `Decide` with no responder denies it with
  `no_approver` and allows its dry run, and with a responder it returns an approval request; these
  pass on a Destructive spec already, so they pin the contract rather than turn red.
- [x] Green: the recipe table entry; `DestructiveHint: true`; the golden regenerated (one line);
  an assertion that both erasing tools declare the hint, checked to fail with `false` even after
  the golden is regenerated.
- [x] Commit `fix(memory): a run with no operator cannot erase memory`.

### Task 3 — Close
- [x] `go vet`, `go build`, `go test -race` on the three packages and their dependants;
  `golangci-lint` v2.14.0 on them: 0 issues.
- [x] Mutation: `classify.go` 80.00% through the critical gate's own scope; `bridge_risk.go`
  92.45% after a test commit that killed the two new survivors only the gateway's tests reached.
- [x] `docs/superpowers/verification/2026-10-30-headless-memory-erase-local.md`: what ran here, and
  that the lab VM (step 0, A4) did not.
- [x] `bash scripts/coverage_docker.sh` on a local Docker stack: 50042/57111 = 87.62%, package
  policy passed; the `arcadedb_integration` live tier passes, after `e05fed9` fixed a test that
  leaked `AURA_EMBED_MODEL` into the process env.
- [ ] Lab VM, step 0 and A4, in its own verification file.
- [x] This plan's status; merge to `master`.
