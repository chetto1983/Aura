# `cmd/aura` stops being a domain package — design

Date: 2026-10-31
Status: **proposed**, not approved. Written from a code read and one unit-tier measurement at
`57fc3e6`; step 0 (§3) is owed before any amendment, plan or code.
Scope: steps 1 and 2 of a four-step split. Steps 3 (split `serve` by area) and 4 (one package per
subcommand) need their own spec once this one closes.

## 1. Why

**What the code says** (measured 2026-10-31 at `57fc3e6`):

| Fact | Evidence |
|---|---|
| `cmd/aura` is one `package main` of 166 non-test files, ~29.7k LOC, 253 types, 75 package-level `var`s | `git ls-files 'cmd/aura/*.go'`, `grep -c '^type '` |
| it imports 87 internal packages; the next highest fan-out is `internal/agui` at 54 | `go list -f '{{.Imports}}'` |
| the coverage gate calls it "CLI glue (flag parsing, os.Exit dispatch)" and keeps it out of the denominator | `scripts/coverage_gate.sh:6` |
| it holds runtime components, not glue: a background-completion dispatcher, the ingestion polling loops, in-memory runner stores, the live MCP mount and its first-party grant keeper | `background_completion.go:46`, `asset_processing_worker.go:37-271`, `cachefakes.go:31-381`, `mcp_live_mount.go:27`, `mcp_first_party_grants.go:61` |
| its own unit-tier coverage is **52.5%** | `go test -cover ./cmd/aura`, 2026-10-31, no `db_integration` tag |
| `chat` and `serve` share one composition root, `chatEnv` (33 fields), which 29 non-test files take as `*chatEnv` | `chat_boot.go:51-90`, `grep -l '\*chatEnv'` |
| `chat` calls ~40 functions defined in `serve_*.go` files (`buildDispatch`, `buildSandboxRouter`, `buildAuthDeps`, the `new*ResumeHook`s, `newMemoryContextProvider`, `newChatGPTPlan`…) | cross-reference of top-level identifiers between file groups |

So the shared turn-runtime assembly exists, but only as an accident of everything living in one
package. Any split by subcommand (step 4) would either duplicate it or import it back from
`main`, which Go forbids. Extracting it is the prerequisite. Moving the runtime components to
their owners first shrinks what that extraction has to carry.

## 2. Goals, non-goals

Goals:
- Every runtime component in `cmd/aura` lives in the package that owns its data or its loop, and
  is counted by the coverage gate.
- The shared composition root is one importable package that both `serve` and `chat` call.
- The binary's behaviour is unchanged: same boot order, same shutdown, same CLI output.

Non-goals:
- Steps 3 and 4.
- `idempotency.go` (485 LOC). It is CLI-only (`cliReplayEnvelope`, `runCLIIdempotent`): it makes a
  mutating *CLI command* replayable. It belongs with step 4, not in `internal/idempotency`.
- A shared periodic-loop helper. `runtimeIngestionWorker` and `firstPartyGrantKeeper` each own a
  ticker loop, and 23 non-test `internal/` files use `time.NewTicker`. Folding those is a separate
  decision with its own inventory.
- The erasure contract note in `asset_processing_worker.go:77-86` (erasure moved to eventual
  reconciliation, which PRD amendments #114/#118 still describe as durable). The code moves
  verbatim; the note is reported in §6, not resolved here.

## 3. Step 0, before any amendment or code

Recorded in `docs/superpowers/verification/<date>-cmd-aura-composition-split-baseline.md`:

1. **The real coverage.** Run `bash scripts/coverage_docker.sh` (full tag matrix). Record:
   - the aggregate;
   - per-file covered/total for every file §4 moves.

   The 52.5% above is unit-only; the integration tier exercises more of `cmd/aura`.
2. **The exact closure of the composition root.**
   - An AST name-based walk from `bootChatEnvWithConfig` reached 334 declarations, ~4.2k lines,
     across 49 files (largest: `chat_boot.go` 419, `serve_adapters.go` 379, `main.go` 334,
     `mcp_live_mount.go` 316, `serve_provisioning_objectstore.go` 310).
   - That walk resolves by name and over-approximates. Redo it with `go/types`, and record the
     declarations and their statement count from the coverprofile.
3. **Seams in the closure.** List which of the 75 package-level `var`s the closure reads
   (function variables such as `loadManagedMCPConfig`, `mcpInstallGuard` and
   `settingsListerForCLI` are swapped by tests). Each one becomes an injected field (D6).
4. **Fake overlap.** Compare the method sets of `cachefakes.go`'s `memConvStore`/`memPauseStore`
   and `internal/runner/fakes_test.go`'s `fakeConvStore` (551 LOC). Record whether one type can
   serve both (D3).
5. **Behaviour baseline on the lab VM**:
   - `aura serve`: boot to ready, then graceful stop. Record the log sequence and the exit code.
   - one `aura chat` turn with a tool call;
   - one background shell completion that wakes its conversation;
   - one uploaded asset processed end to end;
   - `aura cache-audit`, with its exit code.

   These five runs are the comparison for A6.

If step 0.1 shows the aggregate cannot absorb step 2 at its measured coverage (§6, first risk),
step 2's plan must add the tests before the move, not after.

## 4. Decisions (proposed)

### Step 1 — runtime components to their owners

Each move is one commit with its tests, done with `git mv` plus edit so blame survives. The
`cmd/aura` constructor that reads `config` or opens stores stays behind. Only the loop or the
logic moves, and it takes interfaces.

- **D1. Background completions → `internal/runner`.**
  - What moves: `background_completion.go` (245 LOC) and `background_completion_format.go` (54),
    with `background_completion_test.go` (496).
  - Measured: it reads no `package main` symbol, and all seven of its internal imports are
    already in `runner`'s dependency set. Zero new import edges.
  - Unit coverage: 91.5% and 100%.
  - The call site `serve_lifecycle.go:60` uses the exported constructor.
- **D2. Ingestion → `internal/documents` and `internal/assets`.**
  - **To `internal/documents`:**
    - the polling loop and fan-out from `asset_processing_worker.go`: `runtimeIngestionWorker`,
      `runtimeTenantIngestionProcessor`, `runtimeProcessingWorkers`;
    - the job-worker builder at `:128-153`, which already builds a `documents.IngestionJobWorker`;
    - `documentFieldsPass` (`document_fields_pass.go`).
  - **To `internal/assets`:** `asset_processing_queue.go` and `asset_processing_handler.go`.
    `assets` already imports `documents`, never the reverse.
  - **Stays in `cmd/aura`:** `newRuntimeAssetProcessingWorker` (`:58-97`), because it reads
    `cfg`, `pool` and `identity.New` (wiring).
  - **New import edges:**
    - `documents` → `docfields`, cycle-free: `docfields` does not import `documents`.
    - No `documents` → `identity` edge: `runtimeIdentityLister` is already an interface, and the
      concrete store is injected by the constructor that stays behind.
- **D3. In-memory runner stores → `internal/runner/runnertest`.**
  - What moves: `cachefakes.go` (385 LOC, 40.8% unit), on the `internal/agent/agenttest`
    precedent. It is non-test code because the shipped hidden subcommand `cache-audit` uses it
    (`cache_audit.go:199`).
  - If step 0.4 shows the method sets match, `runner/fakes_test.go` uses the same types and its
    copies are deleted in the same commit (REUSABLE CODE). If they do not match, the files stay
    two and the reason is written down.

### Step 2 — one composition root

- **D4. A new package holds the shared root.**
  - Name proposed: `internal/composition`. `internal/setup` is taken by the QR pairing flow, and
    `serve_bootstrap*.go` already uses "bootstrap" for something else.
  - `chatEnv` becomes `composition.Runtime`, with its 33 fields exported only where a caller
    outside the package reads them.
  - `bootChatEnvWithConfig`, `assembleChatEnv`, `assembleChatEnvAtMigrationHead` and the closure
    measured in step 0.2 move with it.
  - Callers use `composition.Boot(ctx, composition.Options{RequireLLMKey: …, DeferOAuthMounts: …})`,
    which replaces the pair `bootChatEnv`/`bootServeChatEnv` (`chat_boot.go:210-218`).
- **D5. No alias, no shim.** The 29 files that take `*chatEnv` are rewritten to
  `*composition.Runtime` in the same commit. A `type chatEnv = …` kept "for now" is the legacy
  code CLAUDE.md forbids.
- **D6. Test seams become fields.** Every package `var` from step 0.3 becomes a field of
  `composition.Options` (or of the component that uses it), set by the test that swaps it today.
  None is re-declared as a global in the new package.
- **D7. The MCP runtime moves with the root, not in step 1.**
  - What moves: `liveMCPMount`, `firstPartyGrantKeeper`, `mcpAuthService` and `mcpRuntimeSet`.
  - Why not step 1: they read `*chatEnv`, `runtimeToolHandles`, `mcpMountOptions`,
    `runtimeMCPOAuth` and `closeMCPServers`. Moving them alone would mean inventing an interface
    for a struct that is about to move anyway.
  - Why not `internal/mcp`: `mcpAuthService` implements an `agui` interface, so an `mcp` →
    `agui` edge would point the wrong way.
- **D8. Commit order inside step 2**, each one green on Gate 2:
  1. move the root type and boot functions;
  2. move the MCP runtime;
  3. move the remaining closure.

  What is left in `serve_*.go` is serve-only wiring and stays for step 3.
- **D9. The gate tells the truth.**
  - `coverage_gate.sh:6` is rewritten to describe what `cmd/aura` still holds after step 2.
  - Every new package is added to `scripts/coverage_package_policy.json` in the commit that
    creates it. Mode `target` if it measures ≥85% on the full matrix; otherwise `baseline` with
    the exact covered/total measured, plus an entry in the plan to raise it.

Alternative considered: extract by subcommand first (step 4 before 2). It fails on the measured
cross-references: `chat` would have to import `serve`'s builders from `main`.

## 5. Acceptance (machine-checkable)

- **A1. Nothing moved remains.** `grep -E '^type (backgroundCompletion|runtimeIngestion|runtimeTenantIngestion|runtimeProcessingWorkers|runtimeAssetProcessingQueue|mem(Conv|Pause|Identity|CacheMetric|ToolInvocation)Store|chatEnv|liveMCPMount|firstPartyGrantKeeper|mcpAuthService)\b' cmd/aura/*.go` returns nothing.
- **A2. Import graph.**
  - `go build ./...` passes.
  - `internal/documents` gains exactly one internal import, `internal/docfields`, and none of
    `internal/identity`.
  - `internal/runner` gains none.
  - `internal/composition` imports no `cmd/`.
- **A3. Size.** `cmd/aura` non-test LOC drops by at least the step-0.2 closure plus the step-1
  files. Measured with `git ls-files` before and after, both numbers in the verification file.
- **A4. Gate 2.** `go vet`, `go build`, `go test -race` on every touched package, `golangci-lint`
  (incl. `dupl`), `deadcode`, `make file-size` (no file >600 LOC).
- **A5. Gate 3.**
  - `scripts/coverage_gate.sh` green on the full matrix, with the policy entries of D9.
  - Aggregate ≥85%.
  - Mutation spot-check ≥70% on `internal/runner`'s background dispatcher and on the moved
    ingestion loop.
- **A6. Behaviour.** Step 0.5 repeated on the lab VM after each step. Same log sequence modulo
  timestamps and package names in log keys, same exit codes, the completion wakes its
  conversation, the asset reaches its terminal state. Recorded in
  `docs/superpowers/verification/`.

## 6. Risks and open questions

- **The aggregate floor. This is the main risk.**
  - Moving the root into `internal/` puts code with low unit coverage into the denominator: in
    the last measurement it was outside. Measured on the unit tier: `chat_boot.go` 47.0%,
    `chat_boot_memory.go` 48.6%, `chat_boot_settings.go` 60.0%, `serve_adapters.go` 52.5%,
    `mcp_live_mount.go` 46.9%, `mcp_oauth_provider.go` 29.4%.
  - The last recorded aggregate, 27,671/31,839 = 86.91%, dates from 2026-08-26 and is stale.
    A thousand uncovered statements would take that figure under 85%.
  - Step 0.1 decides it. If it does not fit, step 2's plan writes daemon-free tests for the moved
    pure logic first (CLAUDE.md, coverage section).
- **Diff size of D5.** 29 files in one commit. It is mechanical; the review checks it with a diff
  that ignores the rename (`git diff --word-diff`), and A6 checks behaviour.
- **The erasure note** (`asset_processing_worker.go:77-86`). The code says erasure moved from a
  fenced multi-store proof to eventual reconciliation and "has to be amended into the PRD". Not
  verified here whether that amendment exists. Report it to the operator before the plan.
- **`runnertest` in the denominator.** `agenttest` is excluded from the coverage gate as
  test-support. `runnertest` ships in the binary through `cache-audit`. Proposed: count it. The
  operator decides.
- **Package name.** `internal/composition` is a proposal; the operator decides.

## 7. What this does not show

- **Only unit coverage was run**: `go test -cover ./cmd/aura` at `57fc3e6`. The integration and
  smoke tiers, the lab VM and the full matrix were not.
- **The closure is approximate.** 334 declarations / ~4.2k lines come from a name-based AST walk,
  not from type information.
- **No build- or test-time gain is claimed.** None was measured. If the split is to be justified
  by speed, step 0 must measure it too.
- **That steps 3 and 4 are worth doing.** This spec makes them possible; it does not argue for
  them.
