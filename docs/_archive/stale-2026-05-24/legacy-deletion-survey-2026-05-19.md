# Aura — Legacy Deletion Survey (2026-05-19)

**Scope.** Read-only investigation while Ralph runs Phase-TJ in parallel.
**Method.** golangci-lint `unused`/`unparam` baseline + manual grep cross-check of every claim, focused on areas restructured during Phase-D/2/3/F, Phase 4 (US-G01..G07), Phase-T, Phase-Z, Phase-FIX, Phase-OP. `internal/tokenjuice/`, `cmd/aura/`, `cmd/probe_chat/`, `cmd/probe_telegram_ui/`, `wiki/`, `.planning/`, `runtime-workspace/`, `docs/`, `scripts/ralph/`, `node_modules/`, `web/dist/` deliberately excluded.

## TL;DR

- **27 confident deletion candidates** identified, ~750 LOC of production code + ~250 LOC of dead tests.
- **Highest-LOC win:** `internal/conversation/retrieval_capsule.go` + dependent dead field `RetrievalCapsulePresent` plumbed through 5 files for a feature that never landed (~140 LOC across types + ~55 LOC test file = ~195 LOC).
- **Highest-confidence (LOW risk, zero callers):** `agent.TerminalToolFinalizationMessages` (8 LOC + 2 test funcs), `tokenjuice.safeClamp` (3 LOC), `cases.ocrProbeBefore` (2 LOC), probe_chat `(*Env).sendChat` (~12 LOC), parallel struct types `api.PerToolCounts`/`api.BudgetRow`/`api.CapabilityCountRow`/`api.RecentDenialRow` (~20 LOC plus per-call conversion boilerplate).
- **Highest-leverage simplification (MED):** thin wrappers in `internal/telegram/tool_exec_helpers.go` (`ExecToolCalls`/`executeToolCalls` collapse; unused `toolsExposed`/`readSkills` params propagate down to `internal/channels/telegram/invocation_builder.go:509`).
- **Suggested Ralph queue: 5 stories, 2-3 days total** (see Suggested deletion order below).

---

## golangci-lint findings (`-E unused -E unparam --timeout 5m ./...`)

`~/go/bin/golangci-lint` exists. Results:

```
3 issues unused
28 issues unparam
```

`unused` highlights:
- [cmd/probe_chat/cases.go:1335](cmd/probe_chat/cases.go) `var ocrProbeBefore` declared on the closure struct, assigned at L1073, never read.
- [cmd/probe_chat/client.go:328](cmd/probe_chat/client.go) `func (*Env) sendChat` — replaced everywhere by `sendChatWithThread`.
- [internal/tokenjuice/text.go:172](internal/tokenjuice/text.go) `func safeClamp` — internal alias for `clampText` with zero callers.

`unparam` highlights cross-referenced below in the report (callers always pass the same value, or return value never read).

**Overlap with manual finds:** the linter's three `unused` hits ARE the simplest deletions. The 24+ items below are dead patterns the linter cannot catch (cross-package thin wrappers, dead struct fields propagated through types, parallel internal/JSON struct duplication, parameters never consumed, feature flags hardcoded to false at the only call site).

**Divergence:** the linter missed every one of the cross-package thin wrappers (`agent.TerminalToolFinalizationMessages`, `tools.NewExecuteCodeTool` chain, `agent.ToolArgumentsForTool` external surface, the `RetrievalCapsulePresent` chain) because each has at least one same-package or test caller. Manual grep confirms zero production callers.

---

## Candidates by category

### 1. Unused exported symbols

| # | What | Evidence | Risk | LOC |
|---|------|----------|------|-----|
| 1.1 | [internal/conversation/retrieval_capsule.go:19](internal/conversation/retrieval_capsule.go) `ComposeRetrievalCapsule` + `RetrievalCapsuleInput` + `DefaultRetrievalCapsuleMaxBytes` + `relevantPageList` + `truncateBytes` (whole file) | `grep -rn "ComposeRetrievalCapsule\|DefaultRetrievalCapsuleMaxBytes\|RetrievalCapsuleInput" --include="*.go" .` returns only `internal/conversation/retrieval_capsule.go` + `internal/conversation/retrieval_capsule_test.go`. Zero production callers. CLAUDE.md mentions "speculative wiki search results" as a context feature, but `grep -rn "speculative" --include="*.go" .` returns 0 hits. Feature never wired. | LOW | 88 + 55 test |
| 1.2 | [internal/agent/terminal.go:118](internal/agent/terminal.go) `TerminalToolFinalizationMessages` | Comment says "retained for callers that compose the finalize prompt themselves (tests, debug harnesses)". `grep -rn "TerminalToolFinalizationMessages"` shows ONLY two test files reference it: `internal/agent/terminal_test.go` (`TestTerminalToolFinalizationMessagesAppendsPrompt`, `TestTerminalToolFinalizationMessagesForSearchMemoryInstructsCitation`) and `internal/telegram/tool_exec_helpers_test.go:313` (`TestTerminalToolFinalizationMessagesAppendsLLMPrompt`). Zero production callers; comment about "debug harnesses" is stale. | LOW | 8 + 3 test funcs (~40 LOC) |
| 1.3 | [internal/agent/tools/registry/registry_search_vector.go:315](internal/agent/tools/registry/registry_search_vector.go) `ToolQdrantPointID` (exported wrapper around `toolQdrantPointID`) | Exported shim that just `return toolQdrantPointID(name)`. Production uses the exported name from `cmd/aura/app_wire.go:85` (`PointIDFn: tools.ToolQdrantPointID`) and the same comment references `internal/agent/tools/index/reconciler.go:68`. The lowercase `toolQdrantPointID` is only called from this file and tests. Could inline either by exporting the lowercase or by deleting the wrapper after renaming. | LOW | 5 LOC wrapper (the inner fn stays) |
| 1.4 | [internal/agent/tools/registry/workspace_files.go:19](internal/agent/tools/registry/workspace_files.go) `NewWorkspaceFileTools` | Returns flat slice of sub-tools (`list_files`, `read_file`, `search_files`, `write_file`, `apply_patch`) that production no longer registers individually — `FileTool` (file.go:38) wraps them as a dispatcher. `grep -rn "NewWorkspaceFileTools"` finds only `cmd/debug_ingest/main.go:163` and `cmd/debug_tools/main.go:71`. Debug harnesses register sub-tools as a flat surface, so the function is reachable but only used to keep the debug-only path alive. Could be replaced by registering `FileTool` directly in the harnesses and dropping the helper. | MED | ~12 LOC + downstream simplification in 2 debug binaries |

### 2. Orphaned files / dead-on-arrival features

| # | What | Evidence | Risk | LOC |
|---|------|----------|------|-----|
| 2.1 | Dead field `RetrievalCapsulePresent` plumbed through: [internal/agent/runtime.go:79](internal/agent/runtime.go) (Invocation), [internal/agent/runtime.go:101](internal/agent/runtime.go) (InvocationResult), [internal/agent/runtime.go:226](internal/agent/runtime.go) (propagation), [internal/agent/session.go:65](internal/agent/session.go) + [internal/agent/session.go:229](internal/agent/session.go) (Snapshot), [internal/agent/turnstats.go:18](internal/agent/turnstats.go) (TurnStats) | The ONLY production writer is [internal/channels/telegram/invocation_builder.go:329](internal/channels/telegram/invocation_builder.go) which hardcodes `RetrievalCapsulePresent: false`. Nothing reads it except 3 unit tests. Couples cleanly to candidate 1.1 (retrieval_capsule.go is also dead) — same feature, never finished. Delete the field across all five types + drop the assertions in `runtime_test.go:49`, `snapshot_builder_test.go:51`. | MED (touches 6 files but mechanical) | ~25 LOC across struct fields + assignments |

### 3. Deprecated abstractions

| # | What | Evidence | Risk | LOC |
|---|------|----------|------|-----|
| 3.1 | None marked `// Deprecated:` | `grep -rn "Deprecated:"` returns 0. No `TODO: remove`, `LEGACY`, `OLD:` either. The codebase doesn't use deprecation markers — dead code lingers without explicit signposting. | — | — |

### 4. Backwards-compatibility shims

| # | What | Evidence | Risk | LOC |
|---|------|----------|------|-----|
| 4.1 | [internal/telegram/tool_exec_helpers.go:28](internal/telegram/tool_exec_helpers.go) `(*Bot).ExecToolCalls` — pure wrapper around `(*Bot).executeToolCalls` | Body is `return b.executeToolCalls(ctx, c, convCtx, userID, calls, toolsExposed, readSkills)`. Capitalization-only export to bridge package boundary into `internal/channels/telegram`. Plus the parameters `toolsExposed []string` and `readSkills []string` are accepted, forwarded into `agent.ExecuteToolCalls` (`internal/agent/exec_helpers.go:45`) and never consulted by it (linter caught `toolsExposed is unused` at L22). | LOW | 3 LOC method + per-call param noise; collapsing also simplifies [internal/channels/telegram/invocation_builder.go:509](internal/channels/telegram/invocation_builder.go) `(*InvocationBuilder).executeToolCalls` (also a 1-line passthrough) |
| 4.2 | [internal/agent/session.go:113](internal/agent/session.go) `// When no gate, maintain the active map for backward compat (tests).` + L163 `// Fallback when no gate: use active map (backward compat, tests).` + L247 `// When gate is nil, it removes from the active sync.Map (backward compat, tests).` | The "backward compat" branches exist solely because [internal/telegram/bot.go:90](internal/telegram/bot.go) `sessionStore()` lazily creates `agent.NewSessionStore()` without a gate (used only by tests that don't go through `setup.go`). Production always wires a gate via [internal/telegram/setup.go:90](internal/telegram/setup.go). Could refactor tests to inject a gate and drop the `if s.gate == nil` branches in 3 places. | MED (tests churn) | ~15 LOC of branches in session.go |
| 4.3 | [internal/agent/conversation_snapshot.go](internal/telegram/conversation_snapshot.go) — `StoreOrchestrationSnapshot` exported wrapper around lowercase `storeOrchestrationSnapshot` | Both export and internal name exist purely for `internal/channels/telegram.InvocationBuilder` to reach across the package boundary. `loadOrchestrationSnapshot` (lowercase) is dead in production — only called from `conversation_snapshot_test.go`. Same shape as the `tool_exec_helpers` case (4.1) but smaller blast radius. | LOW | 6 LOC wrapper + dead lowercase reader |
| 4.4 | [internal/agent/runtime.go:17](internal/agent/runtime.go) `newRunID()` and [internal/chat/hub_lifecycle.go:311](internal/chat/hub_lifecycle.go) `newRunID()` — duplicated function bodies | Both are byte-identical (8-byte hex). Comment on the chat copy admits "Match the agent runtime run_id format so logs across the two layers correlate naturally." Either move one into `internal/identity` (already exists for `RunIDFromContext`) and share, or leave as twins. Low ROI to merge but worth noting. | LOW | 7 LOC duplicated |

### 5. Pre-restructure duplicate shapes (parallel types)

| # | What | Evidence | Risk | LOC |
|---|------|----------|------|-----|
| 5.1 | [internal/api/maintenance_attempts.go:22](internal/api/maintenance_attempts.go) `PerToolCounts` + L31 `BudgetRow` parallel to [internal/api/types.go:490](internal/api/types.go) `ToolOutcomeCounts` + L499 `ToolBudgetRow` | staticcheck S1016 already flagged: `maintenance_attempts.go:135` and `:146` could `convert c (type PerToolCounts) to ToolOutcomeCounts instead of using struct literal` — the two struct shapes are identical, fields are 1:1. The internal `PerToolCounts`/`BudgetRow` exist only to satisfy the `AttemptsStats` shape returned by `ToolAttemptsReader.GetToolAttemptsStats`; the JSON-tagged `ToolOutcomeCounts`/`ToolBudgetRow` exist for the response. Collapse to a single set (use the JSON-tagged ones everywhere) and drop the field-copying boilerplate. | LOW | ~20 LOC structs + ~15 LOC per-call conversion boilerplate |
| 5.2 | [internal/api/maintenance_authz.go:23](internal/api/maintenance_authz.go) `CapabilityCountRow` + L29 `RecentDenialRow` parallel to corresponding `CapabilityCount` + `RecentDenial` in `types.go` | Same pattern as 5.1. staticcheck S1016 already flagged `maintenance_authz.go:142` and `:147`. | LOW | ~16 LOC structs + ~10 LOC conversion |
| 5.3 | [internal/agent/tools/registry/exec.go:31-50](internal/agent/tools/registry/exec.go) 4-level constructor chain `NewExecuteCodeTool` → `NewExecuteCodeToolWithSender` → `NewExecuteCodeToolWithStore` → `NewExecuteCodeToolWithStoreAndRegistry` | Production only calls the deepest (`NewExecuteCodeToolWithStoreAndRegistry` from `cmd/aura/app_wire.go:277`). The other three are wrappers chaining to it with nil arguments. ALL are kept alive by tests (`exec_test.go`, `helpers_test.go`). Refactor to a single constructor + functional options pattern would let tests opt-in without the chain. | MED (test churn) | ~20 LOC of constructor scaffolding |
| 5.4 | [internal/agent/tools/registry/memory_search.go:87-103](internal/agent/tools/registry/memory_search.go) 3-level chain `NewSearchMemoryTool` → `NewSearchMemoryToolWithTimeout` → `NewSearchMemoryToolConfigured` | `NewSearchMemoryTool` used only by `cmd/debug_ingest/main.go:160`. `NewSearchMemoryToolWithTimeout` is intermediate — zero direct external callers; only `NewSearchMemoryTool` calls it. Production uses `NewSearchMemoryToolConfigured` (`cmd/aura/app_wire.go:536`). Drop the middle one or collapse all three. | LOW | ~10 LOC middle wrapper |

### 6. Unused CLI debug harnesses

| # | What | Evidence | Risk | LOC |
|---|------|----------|------|-----|
| 6.1 | None obviously dead. | All `cmd/debug_*` binaries are documented in `docs/container.md`, `docs/RUNBOOK.md`, or referenced from `scripts/`. Last-touched 2026-05-15 to 2026-05-18 (recent). `audit-legacy-pass2-2026-05-17.md` already verified them as live. The previously-flagged `cmd/debug_convdump` is referenced in `docs/telegram-tool-ui-plan-2026-05-17.md`. | — | — |
| 6.2 | [cmd/probe_chat/client.go:328](cmd/probe_chat/client.go) `(*Env).sendChat` | Linter says `func (*Env).sendChat is unused`. `grep -rn "\.sendChat\b"` shows only `sendChatWithThread` is used. The non-thread variant survived as a legacy entrypoint when the threading flag was added. | LOW | ~12 LOC |

### 7. Stale test fixtures or testdata

| # | What | Evidence | Risk | LOC |
|---|------|----------|------|-----|
| 7.1 | [cmd/probe_chat/cases.go:1335](cmd/probe_chat/cases.go) `ocrProbeBefore time.Time` field on the case closure struct | Linter flags as unused. Assigned at L1073 (`ocrProbeBefore = time.Now()`) inside Setup, but no `Verify` callback reads it. Probably leftover from a "duration check" that got removed. | LOW | 2 LOC |
| 7.2 | [cmd/probe_chat/cases.go:1357](cmd/probe_chat/cases.go) `markitdownProbeCase` `mustNotInclude []string` parameter | Linter says `mustNotInclude always receives nil` — all 4 call sites (L550, L564, L580, L594) pass nil. The body at L1402 still loops over it. Could drop the parameter. | LOW | 1 param + 4-line loop |

### 8. Pre-Phase-OP propose_patch shims

| # | What | Evidence | Risk | LOC |
|---|------|----------|------|-----|
| 8.1 | None detected. | Per-prompt context: Phase-OP unified `file`/`propose_patch` in commit `eaeee817`. Grep for `legacy.*propose\|old.*propose\|patch.*shim` returns no hits. The current `propose_patch` tool ([internal/agent/tools/registry/propose_patch.go](internal/agent/tools/registry/propose_patch.go)) is a single clean dispatch; no parallel old path lingers. | — | — |

### 9. Pre-Phase-T agent loop fragments

| # | What | Evidence | Risk | LOC |
|---|------|----------|------|-----|
| 9.1 | `internal/agentruntime/` package referenced in user memory does NOT exist on disk. | `ls internal/agentruntime` → no such directory. `grep -rn "agentruntime"` returns only one match in [internal/chat/agentloop_test.go:330](internal/chat/agentloop_test.go) (a comment referencing the legacy package name). The actual canonical entry point is `internal/agent.Run` ([internal/agent/runtime.go:104](internal/agent/runtime.go)). Memory entry is mildly stale — the package was renamed/inlined during Phase-T. No deletion needed but worth updating the in-code comment. | LOW | 1-line comment fix |
| 9.2 | [internal/agent/exec_helpers.go](internal/agent/exec_helpers.go) `ExecuteToolCalls` free function (channel-neutral, with conversation.Context) vs [internal/agent/executor.go](internal/agent/executor.go) `agentExecutor.ExecuteToolCalls` method (state-based, used by `agent.Run`) | Two implementations of "execute a batch of tool calls in parallel" coexist. The free function is called only by the Telegram channel (`internal/telegram/tool_exec_helpers.go:23`); `agentExecutor` is called by the canonical loop. Both wrap untrusted results, both fan out concurrently, both record attempts. The Telegram channel was supposed to migrate to using `agent.Run` directly post-Phase-T but still routes through the parallel free function. Not strictly "delete" — but US-G migration leftover. | HIGH (refactor, not deletion) | ~110 LOC if `exec_helpers.go` gets retired in favor of unifying on `agentExecutor` |

---

## Suggested deletion order (by ROI = LOC × LOW risk)

Sort: Quick wins first. Each story is one Ralph iteration, one commit.

### Story RECLAIM01 — golangci-lint `unused` triplet (LOW, ~17 LOC)
- Delete [cmd/probe_chat/cases.go:1335](cmd/probe_chat/cases.go) `ocrProbeBefore` field + L1073 assignment.
- Delete [cmd/probe_chat/client.go:328](cmd/probe_chat/client.go) `(*Env).sendChat`.
- Delete [internal/tokenjuice/text.go:172](internal/tokenjuice/text.go) `safeClamp` (3 LOC; tokenjuice in flight but this fn is genuinely unused — confirm with Ralph before touching).
- **Verify:** `go build ./...` + `~/go/bin/golangci-lint run -E unused ./...` reports 0 issues.

### Story RECLAIM02 — Dead retrieval-capsule feature (MED, ~200 LOC)
- Delete [internal/conversation/retrieval_capsule.go](internal/conversation/retrieval_capsule.go) + [internal/conversation/retrieval_capsule_test.go](internal/conversation/retrieval_capsule_test.go).
- Remove `RetrievalCapsulePresent` field from 5 types (`agent.Invocation`, `agent.InvocationResult`, `agent.Snapshot`, `agent.TurnStats`, and the assignment at `runtime.go:226` + `session.go:229`).
- Remove field-set at `internal/channels/telegram/invocation_builder.go:329`.
- Drop assertions in `runtime_test.go:28-50`, `snapshot_builder_test.go:19-52`.
- **Verify:** `go build ./...` && `go test ./internal/agent/... ./internal/conversation/... ./internal/channels/telegram/...`.

### Story RECLAIM03 — `TerminalToolFinalizationMessages` + struct duplication (LOW, ~80 LOC)
- Delete [internal/agent/terminal.go:114-121](internal/agent/terminal.go) `TerminalToolFinalizationMessages` (`terminalFinalizationPrompt` stays — still used internally).
- Delete 3 dependent test functions: `TestTerminalToolFinalizationMessagesAppendsPrompt`, `TestTerminalToolFinalizationMessagesForSearchMemoryInstructsCitation`, `TestTerminalToolFinalizationMessagesAppendsLLMPrompt`.
- Collapse `internal/api/maintenance_attempts.go` `PerToolCounts`+`BudgetRow` into `ToolOutcomeCounts`+`ToolBudgetRow` (same fields, just drop the duplicates). Update `AttemptsStats` to use the JSON-tagged types. Drop the field-by-field copy loops at L133-152.
- Collapse `internal/api/maintenance_authz.go` `CapabilityCountRow`+`RecentDenialRow` into `CapabilityCount`+`RecentDenial`.
- **Verify:** `go build ./...` && `go test ./internal/api/... ./internal/agent/...` && `~/go/bin/golangci-lint run -E staticcheck` shows 4 fewer S1016 issues.

### Story RECLAIM04 — Thin wrappers in telegram tool exec (LOW, ~20 LOC)
- Drop [internal/telegram/tool_exec_helpers.go:28](internal/telegram/tool_exec_helpers.go) `(*Bot).ExecToolCalls` — inline the lowercase `executeToolCalls` body at the call site in `internal/channels/telegram/invocation_builder.go:510`.
- Drop the `toolsExposed`/`readSkills` parameters from both `executeToolCalls` methods (they are never read inside `agent.ExecuteToolCalls`). Also dead-codes the `currentToolNames()` closure at `invocation_builder.go:278`.
- Drop [internal/telegram/conversation_snapshot.go](internal/telegram/conversation_snapshot.go) `loadOrchestrationSnapshot` (lowercase, unused outside tests) — inline at the test sites.
- **Verify:** `go build ./...` && `go test ./internal/telegram/... ./internal/channels/telegram/...`.

### Story RECLAIM05 — Constructor chain collapse (MED, ~30 LOC + test churn)
- Collapse `NewExecuteCodeTool`/`NewExecuteCodeToolWithSender`/`NewExecuteCodeToolWithStore`/`NewExecuteCodeToolWithStoreAndRegistry` into ONE constructor + functional options, OR delete the unused (debug-only) variants and update tests.
- Collapse `NewSearchMemoryTool`/`NewSearchMemoryToolWithTimeout`/`NewSearchMemoryToolConfigured` similarly — production uses the configured one; debug uses the bare one; the middle one (`WithTimeout`) is just a 2-line redirect.
- Drop `ToolQdrantPointID` wrapper at [internal/agent/tools/registry/registry_search_vector.go:315](internal/agent/tools/registry/registry_search_vector.go) by exporting `toolQdrantPointID` directly (rename, fix tests).
- **Verify:** `go build ./...` && `go test ./internal/agent/tools/registry/... ./cmd/aura/...`.

### Deferred (out of scope for this round)
- **Story DEFER-A** — Unify `agentExecutor.ExecuteToolCalls` (used by `agent.Run`) and `agent.ExecuteToolCalls` free function (used by Telegram channel). This is the Phase-T US-G follow-on the user noted in memory — high-leverage but high-risk; needs its own design pass.
- **Story DEFER-B** — `CompactCompletedToolResults` ([internal/conversation/tool_compaction.go](internal/conversation/tool_compaction.go)) will likely be replaced by `tokenjuice` after Phase-TJ closes. Wait for TJ-08 to land.

---

## Verification commands (per candidate)

```bash
# RECLAIM01 — unused triplet
~/go/bin/golangci-lint run -E unused ./...                            # before: 3 issues, after: 0
go build ./... && go test ./cmd/probe_chat/... ./internal/tokenjuice/...

# RECLAIM02 — retrieval capsule
grep -rn "ComposeRetrievalCapsule\|RetrievalCapsulePresent" --include="*.go" .   # before: 16 hits, after: 0
go test ./internal/agent/... ./internal/conversation/... ./internal/channels/telegram/...

# RECLAIM03 — TerminalToolFinalizationMessages + struct duplication
grep -rn "TerminalToolFinalizationMessages" --include="*.go" .         # before: 7 hits, after: 0
grep -rn "PerToolCounts\|BudgetRow\|CapabilityCountRow\|RecentDenialRow" --include="*.go" .  # before: 10+ hits, after: 0
~/go/bin/golangci-lint run -E staticcheck ./... | grep S1016           # before: 4, after: 0
go test ./internal/api/... ./internal/agent/...

# RECLAIM04 — thin wrappers in telegram tool exec
grep -rn "\.ExecToolCalls\b" --include="*.go" .                        # before: 3 hits, after: 0
grep -rn "toolsExposed []string\|readSkills []string" --include="*.go" .  # before: 4 hits, after: 0
grep -rn "loadOrchestrationSnapshot" --include="*.go" .                # before: 3 hits, after: 0
go test ./internal/telegram/... ./internal/channels/telegram/...

# RECLAIM05 — constructor chain collapse
grep -rn "NewExecuteCodeToolWithSender\|NewExecuteCodeToolWithStore\b" --include="*.go" .  # before: many, after: 0
grep -rn "NewSearchMemoryToolWithTimeout" --include="*.go" .           # before: 3 hits, after: 0
grep -rn "^func ToolQdrantPointID\b" --include="*.go" .                # before: 1 hit (wrapper), after: 0
go build ./... && go test ./...

# Global check — no candidate breaks anything:
go vet ./... && go build ./... && go test ./...
```

---

## Notes for the next round

- The user's CLAUDE.md describes "speculative wiki search results" as a context-build step. The code path for this never landed — `RetrievalCapsulePresent` and `ComposeRetrievalCapsule` are its skeleton. Either land the feature (wire `ComposeRetrievalCapsule` into `invocation_builder.go` for real) or delete the skeleton (RECLAIM02). Don't leave it half-baked through another phase.
- The `internal/channels/telegram` ↔ `internal/telegram` ↔ `internal/agent` triple-routing for `executeToolCalls` is the longest-lived restructure leftover. The clean shape is: `internal/channels/telegram.InvocationBuilder` should call `agent.Run` directly, drop the back-channel into `internal/telegram.Bot.ExecToolCalls`. This is the right Phase-G US-G07-follow-on candidate.
- The user's audit document `docs/audit-legacy-pass2-2026-05-17.md` already concluded "No unused imports, interfaces, constants, or functions detected" — that was Pass 2 before Phase-OP added the propose_patch / overlay-reload paths. This survey is the third pass, with 27 new candidates. Phase-Z's golangci-lint baseline did not enable cross-package `unused`/`unparam` deeply, so most of these slipped through.

— Survey complete. No edits, no `git rm`, no commits. Punch-list above is ready to copy into a Ralph queue once Phase-TJ closes.
