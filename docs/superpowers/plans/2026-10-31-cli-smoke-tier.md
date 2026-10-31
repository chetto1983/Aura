# Every CLI verb runs in CI — Implementation Plan

**Status (2026-10-31):** done and on `master`, except the final mutation score on the memory mapping
functions, which is owed to the verification file. Approved by the operator ("esegui tutto E2E"). Evidence: `docs/superpowers/verification/2026-10-31-cli-smoke-tier.md`.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task by task. Steps use checkbox (`- [ ]`) syntax for tracking. One atomic commit per task.

**Goal:**
- Every verb `aura` dispatches is run by a test, or is exempt by name with a reason.
- Every MCP call the CLI makes validates against its server's published schema.
- A new verb with neither a test nor an exemption fails CI.

**Architecture:**
- **Contracts** (`cmd/aura/cli_contract_test.go`, unit tier). Each memory verb's arguments are
  checked against `docs/arcadedb-mcp-live-tools.json` with `github.com/google/jsonschema-go`.
- **Inventory** (`cmd/aura/cli_inventory_test.go`, unit tier).
  - It parses the dispatchers with `go/ast`.
  - It compares the `group verb` pairs found with the smoke rows, the contract rows and
    `cliExemptions`.
- **Smoke** (`cmd/aura/cli_smoke_integration_test.go`, tag `db_integration`).
  - `TestMain` builds the binary once.
  - Each run creates a database, migrates it with `aura db migrate`, and drops it afterwards.
  - Rows run in order: `{argv, stdin, wantExit, stdoutContains, stderrContains, after}`.
  - The `mcp` rows point the managed config at the recording HTTP fixture.

**Tech Stack:** Go (`cmd/aura` tests only), `go/ast`, `github.com/google/jsonschema-go` (already in
`go.mod`), Postgres via the existing `db_integration` env.

**Spec:** `docs/superpowers/specs/2026-10-31-cli-smoke-tier-design.md`.

## Global Constraints

- **No production code changes**, except a seam a test genuinely needs. Any such seam is named in
  its commit.
- **No migration, no env var, no dependency.**
- **Files stay ≤ 600 LOC.** If the smoke rows grow past that, split them by group into
  `cli_smoke_<group>_integration_test.go`.
- **No skip-as-green.** The smoke fails when `$CI` is set and `AURA_DB_URL` is unset.

## Review Focus

- **Fail closed.** The inventory must fail on a dispatched verb that is missing, and on a stale
  exemption.
- **Contracts use the server's own validator** (`jsonschema-go`), not a hand-written check.
- **Markers match a header word or a key**, never a whole line.
- **No row depends on the live `aura` database or on the network**, except the recording fixture
  on loopback.

## Decisions taken while writing this plan

- **The smoke lives under `db_integration`.** The coverage gate already runs `cmd/aura` with that
  tag (`scripts/coverage_gate.sh:71`), so the rows run in CI with no new job.
- **The inventory parses the source** instead of introducing a verb registry. A registry is step
  4 of the split spec.
- **The memory verbs stay at the contract tier.** Running them live is the spec's open question
  1.

## File structure

| File | Change |
|---|---|
| `cmd/aura/cli_contract_test.go` | new: the memory verbs against the golden schemas |
| `cmd/aura/cli_inventory_test.go` | new: the dispatcher parse, `cliExemptions`, and the fail-closed comparison |
| `cmd/aura/cli_smoke_integration_test.go` | new: `TestMain`, the per-run database, the row runner |
| `cmd/aura/cli_smoke_rows_integration_test.go` | new: the rows, by group |
| `prd.md` §18 | the amendment recording the measurement and the gate |

## Tasks

### Task 0 — Record the measurement (PRD-first)
- [x] `prd.md` §18 gets a paragraph:
  - the audit's measurement (coverage by file, what CI runs, D4's two months);
  - the decision: the CLI is gated by contracts, inventory and smoke, and not by the
    denominator;
  - what it does not show.
- [x] Commit `docs(prd): the CLI's verbs are gated by smoke, not by coverage`, together with the
  spec marked approved.

### Task 1 — Contracts (A2)
- [x] Red: `91123b7^` predates the `memoryCallFor` seam the test needs, so D4's shape was put
  back into HEAD's `memoryRememberArgs` instead. Both `remember` rows fail with "unexpected
  additional properties"; the file was then restored.
- [x] Green on HEAD:
  - every memory verb, with representative flags, is validated against its tool's `inputSchema`;
  - `remember` is checked with its run taken from the context;
  - `forget --run` keeps `source.run_id`, which the forget schema allows.
- [x] A golden tool that no verb maps to is listed in the test with a reason. Today those are
  `memory_batch`, `memory_recall`, `graph_path` and `graph_diagnostics`.
- [x] Mutation on `memoryVerbToTool` and its helpers: see Task 5.
- [x] Commit `test(cli): every memory verb matches its server's schema`.

### Task 2 — Inventory (A1)
- [x] Red: an injected dispatcher holding an unlisted verb fails the test, and so does a stale
  exemption.
- [x] Green, with the dispatchers listed:
  - `main`'s top-level switch;
  - `runTask`, `runChat`, `runSkills`, `runIdentity`, `runGateway`, `runPausedStates`;
  - `runConfig`, `runDB`, `runObjectStore`, `runWeb`, `runAgent`, `runDocsCommand`,
    `runRetentionCommand`;
  - `memoryVerbToTool`, `runMCP`, the `mcp profile` dispatcher, and `skills snippet` and `share`.
- [x] Seed `cliExemptions` with the audit's list: interactive verbs, `serve`, `shell`,
  `swarm-demo`, `web` (no SearXNG), `objectstore restore`, `db reset`, and the hidden
  `toolpipe` and `cache-audit`. Each gets one line of reason.
- [x] Commit `test(cli): every dispatched verb is tested or exempt by name`. It landed after the
  smoke commit, because the inventory reads the rows.

### Task 3 — The smoke harness (A3, A4)
- [x] `TestMain` builds the binary.
  - `setupCLIDatabase` creates `aura_cli_smoke_<nanos>`, runs `aura db migrate`, and drops it
    on cleanup.
  - The environment checks are no-skip-as-green.
- [x] A row runner checks exit code, markers and `after` SQL. It reports the whole stderr on a
  failure. The row struct has no `stdin`: no row needed one, and the lint flagged it as unused.
- [x] The first rows: `db ping`, `db status`, `config show`, `tools --json`.
- [x] Tasks 3 and 4 share one commit, `test(cli): a smoke tier that drives the built binary
  through every verb`: the runner without rows proves nothing.

### Task 4 — The rows (A3, A5)
- [x] Reads, by group: `task`, `chat`, `skills`, `identity`, `gateway grants`, `paused-states`,
  `retention plan`, `config`, `mcp` (against the fixture), `doctor`, `cache-stats`, `pack`
  (negative), `docs search`.
- [x] Writes on throwaway data, each with an `after` probe:
  - `skills create`, `update`, `always`, `delete`;
  - `task schedule`, `pause`, `resume`, `cancel`;
  - `chat new` with a scripted stdin, then `rename`, `archive`, `unarchive`,
    `delete --confirm`;
  - `paused-states purge`;
  - `gateway grants revoke` (when there is nothing to revoke);
  - `config set` on a key restored afterwards.
- [x] Each row pins today's behaviour, C2–C4 included. Where a row pins a known defect, its name
  says so, for example `task_list_ignores_unknown_flag_C3`. The conventions slice will then flip
  those rows.
- [x] Commit `test(cli): smoke rows for every Postgres-only verb`.

### Task 5 — Close
- [x] `go vet`, `golangci-lint` v2.14.0, and `go test -race` (with and without `db_integration`) on
  `cmd/aura`: 0 issues, green.
- [x] `bash scripts/coverage_docker.sh`: 50137/57194 = 87.66%, package policy passed, smoke
  included.
  - A5: `cmd/aura` is at 54.9% from its own tests and 63.4% with the smoke's instrumented binary,
    measured through `GOCOVERDIR`, which `cli_smoke_integration_test.go` honours.
- [x] The memory verbs, live on the stack per the `aura-model-in-the-loop` skill: all 16
  invocations exit 0.
- [x] CI green on `9697b2e`. A6 was not measured: the smoke adds 53–67 s locally, the build
  included, but the coverage job's own time before and after was not compared.
- [x] `docs/superpowers/verification/2026-10-31-cli-smoke-tier.md`; this plan's status; merge to
  `master`.
