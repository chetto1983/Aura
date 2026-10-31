# Every CLI verb runs in CI — design

Date: 2026-10-31
Status: **approved by the operator on 2026-10-31** ("esegui tutto E2E"), with the defaults of the
two open questions in §6:
- the memory verbs stay at the contract tier in CI, and run live in the closing verification;
- the smoke pins today's behaviour, and each row named after the C2–C4 defect it pins.

C1 of `docs/superpowers/verification/2026-10-31-cli-audit.md`; that audit is step 0's measurement.
Plan: `docs/superpowers/plans/2026-10-31-cli-smoke-tier.md`.

## 1. Why

Measured on 2026-10-31 (`2026-10-31-cli-audit.md`, C1):

| Fact | Evidence |
|---|---|
| `cmd/aura` is outside the coverage denominator, as "CLI glue (flag parsing, os.Exit dispatch) — covered behaviourally by integration + smoke" | `scripts/coverage_gate.sh:6-7` |
| the operator's verbs have no such smoke: CI calls the CLI for `db migrate/status/ping` and `mcp install` only | `ci.yml:393-398`, `:2014-2015` |
| with the `db_integration` tier `cmd/aura` covers 55.3%; `chat.go`, `gateway_grants.go`, `paused_states.go`, `web.go`, `skills_share.go` at 0%, `skills.go` 3.0%, `identity.go` 7.1%, `task.go` 10.4% | coverage profile, disposable DB |
| `aura memory remember` failed on every call from 2026-08-27 (D-10) to 2026-10-31 | `2026-10-31-briefing-model-in-the-loop.md`, D4 |

D4 is the cost. The CLI built `memory_upsert_fact` arguments that the server's schema rejected.
Nothing compared one to the other, and no test ran the verb.

## 2. Goals, non-goals

Goals:
- Every verb the CLI dispatches is either run by a test or exempted by name with a reason. A new
  verb with neither fails CI.
- Every verb that calls an MCP tool sends arguments that its server's published input schema
  accepts. The check runs at the unit tier, with no containers.
- The tier exercises the built binary, as an operator does: argv, exit code, stdout and stderr.

Non-goals:
- Changing behaviour: help, unknown flags and exit codes are C2–C4. The smoke pins today's
  behaviour, and the conventions slice updates the rows it changes.
- The coverage denominator. `cmd/aura` stays out, and the split spec (`4ca2b99`) moves its
  runtime parts where the gate counts them.
- Verbs that need the operator at a terminal (`identity create` and `recover-operator` read hidden
  prompts; `chat` and `shell` without a scripted stdin), or a service CI lacks (SearXNG for
  `web`). These are exempted by name.

## 3. Decisions (proposed)

**D1. Contracts at the unit tier, against the golden the server is pinned to.**
- **The schemas.** `docs/arcadedb-mcp-live-tools.json` holds each memory tool's `inputSchema`.
  `cmd/arcadedb-mcp/tool_manifest_test.go` already fails when the live server drifts from it.
- **The test.** A test in `cmd/aura` builds every `aura memory <verb>`'s arguments through
  `memoryVerbToTool`, plus the run-from-context path of `remember`. It validates them against
  that schema with `github.com/google/jsonschema-go`, the library the go-sdk uses to reject
  arguments. That gives a chain, CLI → golden ← server, with no container.
- **The check.** Run on `91123b7^`, the test must fail with D4's own message.

**D2. The smoke runs under `db_integration`, inside `cmd/aura`.**
- **Where it runs in CI.** The coverage gate already runs `cmd/aura` with that tag
  (`scripts/coverage_gate.sh:71`, `ci.yml:759`), so no new job is needed.
- **No-skip-as-green.** It follows `envOrSkip`: when `$CI` is set and the DSN is unset, the test
  fails instead of skipping.
- **The binary.** `TestMain` builds it once, as `serve_smoke_test.go` and
  `db_migrate_idempotency_integration_test.go` already do.
- **The database.** Each run gets its own database, created and migrated by the binary
  (`aura db migrate`) and dropped on cleanup, so a row can write freely.

**D3. A row is data.**
- **What a row holds.** `{argv, stdin, wantExit, stdoutContains, stderrContains, after}`, where
  `after` is an optional SQL probe of the effect.
- **Reads** assert exit 0 and a marker: a table header, or `ok:`.
- **Writes** use throwaway names, and check their effect in the database: for example,
  `skills create` makes a row that `skills delete` then removes.
- **Order.** Rows run in sequence, because the writes depend on order.

**D4. The verb inventory is read from the source, and it fails closed.**
- **Reading it.** A unit test parses `cmd/aura` with `go/ast`. It collects the top-level
  `switch os.Args[1]` in `main` and the `case "<verb>"` labels of each group's dispatcher
  (`runTask`, `runChat`, `runSkills`, `memoryVerbToTool`, `runMCP`, …). The list of dispatcher
  names lives in the test.
- **Checking it.** Every `group verb` pair must be a smoke row, a contract row (D1), or a member
  of `cliExemptions`, a map from pair to reason. A pair in none of them fails the test, and so
  does an exemption whose verb no longer exists.
- **Why parse.** It is the same kind of inventory as `scripts/coverage_package_policy.json`. The
  alternative was a verb registry, and refactoring the dispatch into one is step 4 of the split
  spec, not this one.

**D5. Each sidecar stays in its own tier.**
- **Memory verbs.** D1 covers them at the unit tier. Running them live needs ArcadeDB, the
  embedder and `arcadedb-mcp`, which is the `arcadedb_integration` job's stack. Adding them
  there is §6's open question 1, not this spec's.
- **`mcp` verbs.** They run against the recording HTTP fixture that `mcp_status_test.go` already
  has (`newMCPHTTPTestServer`).

## 4. Acceptance (machine-checkable)

- A1. `go test ./cmd/aura -run TestCLIVerbInventory` passes.
  - A dispatched pair with no row and no exemption fails it.
  - A stale exemption fails it.
  - Both are shown by a test that injects each case.
- A2. `go test ./cmd/aura -run TestMemoryVerbsMatchTheServerSchemas` passes. Run with
  `memoryRememberArgs` as of `91123b7^`, it fails with "unexpected additional properties".
- A3. `go test -tags db_integration ./cmd/aura -run TestCLISmoke` passes against a disposable
  database. It covers every non-exempt verb that needs only Postgres or the recording MCP fixture.
- A4. In `CI=true` with `AURA_DB_URL` unset, A3 fails instead of skipping.
- A5. `cmd/aura` coverage with `db_integration` is reported before and after. Every file below
  10% in the audit's table rises, or is exempt by name. It is a report, not a gate.
- A6. The tier adds under two minutes to the coverage job, measured on CI.

## 5. Risks

- **Brittle markers.** A marker tied to exact table text breaks on cosmetic changes. Assert a
  header word or a JSON key, never whole lines.
- **Shared state.** Rows share one database by design (D3). A row that depends on another's
  leftovers must say so in its name.
- **The parser.** It sees only literal `case "x"` labels. A verb dispatched through a map or a
  prefix test is invisible to it, so dispatchers of that shape must be listed explicitly. The
  test names them.

## 6. Open questions

1. Should the memory verbs also run live, in the `arcadedb_integration` job? D1 catches schema
   drift, but not a server that accepts the shape and then fails.
2. Should the conventions slice (C2–C4) land before the smoke rows, so that they pin the new
   behaviour rather than today's?

## 7. What this does not show

- That an operator's real sequence of commands works. The smoke runs verbs one at a time.
- Interactive verbs.
- Anything on the lab VM or on Windows.
