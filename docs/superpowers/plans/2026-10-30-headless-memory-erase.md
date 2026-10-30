# A run with no operator cannot erase memory — Implementation Plan

**Status (2026-10-30):** approved by the operator on 2026-10-30 ("procedi con l'implementazione
della spec memory_forget"). The spec's step 0 on the lab VM has not run: its deterministic part
(the gateway's verdict) is measured in-process instead, as red tests, and the lab-VM run stays the
spec's open acceptance (A4).

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
- [ ] Red: `classify` of a memory `memory_forget` with `dry_run: true` is `Safe`; without it, with
  `dry_run: false`, with `"true"` as a string, with broken JSON, and for another recipe's tool of
  the same name, it is not.
- [ ] Green: `mcptools.MemoryForgetDryRun`; the branch in `classify`.
- [ ] Commit `feat(gateway): a memory_forget dry run is a read`.

### Task 2 — Erasing memory is Destructive (D1, D2; acceptance A1–A3)
- [ ] Red: the bridge classifies `memory_forget` `(true, true)` with nil, lying and read-only
  annotations; `Decide` with no responder denies it with `no_approver`, and allows its dry run;
  with a responder it returns an approval request.
- [ ] Green: the recipe table entry; `DestructiveHint: true`; the golden regenerated.
- [ ] Commit `fix(memory): a run with no operator cannot erase memory`.

### Task 3 — Close
- [ ] `go vet`, `go build`, `go test -race` on the three packages; `golangci-lint` on them.
- [ ] Mutation spot-check on the carve-out, if `go-mutesting` is available here.
- [ ] `docs/superpowers/verification/2026-10-30-headless-memory-erase-local.md`: what ran here, and
  that the lab VM (step 0, A4) did not.
- [ ] Lab VM, step 0 and A4, in its own verification file.
- [ ] This plan's status; merge to `master`.
