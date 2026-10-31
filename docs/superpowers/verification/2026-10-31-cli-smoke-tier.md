# Every CLI verb runs in CI, measured — 2026-10-31

This file closes `docs/superpowers/plans/2026-10-31-cli-smoke-tier.md` (spec
`docs/superpowers/specs/2026-10-31-cli-smoke-tier-design.md`, approved by the operator: "esegui tutto
E2E"). It answers C1 of `2026-10-31-cli-audit.md`.

Where it ran: a cloud container with Docker, Postgres, ArcadeDB and the embedder. The live memory run
followed the `aura-model-in-the-loop` skill. Not the lab VM.

## What landed

| Layer | File | What it checks |
|---|---|---|
| contracts | `cmd/aura/cli_contract_test.go` | each of 15 `aura memory` argv lines, validated against the golden input schemas the way go-sdk v1.8.0 validates on the server |
| inventory | `cmd/aura/cli_inventory_test.go` | the dispatchers' `case` labels (102 leaf verbs), each a smoke row, a contract row or one of 24 named exemptions |
| smoke | `cmd/aura/cli_smoke_rows_test.go`, `cli_smoke_integration_test.go` | 76 rows run by the built binary on a database of the run's own, under `db_integration` |

## Measured

**Contracts catch D4.**
- With `memoryRememberArgs`' `"source":{"run_id":…}` put back, `TestMemoryVerbsMatchTheServerSchemas`
  fails both `remember` rows. The message is the server's own: "validating /properties/source:
  unexpected additional properties".
- `TestAnArgumentTheServerRefusesFailsTheContract` pins that refusal directly.
- On HEAD all 15 rows validate.
- The four tools no verb sends are listed with reasons: `memory_batch`, `memory_recall`,
  `graph_path` and `graph_diagnostics`.

**The inventory fails closed.** I removed the `task_doctor` row and the `web tool web_fetch`
exemption, and renamed the `db reset` exemption to `db nuke`. The test then named:
- the three dispatched verbs left with no row, contract or exemption: `task doctor`,
  `web tool web_fetch` and `db reset`;
- the stale exemption, `db nuke`.

`TestCLIVerbInventoryFailsClosed` pins the four kinds of gap with synthetic data.

**The smoke passes and does not skip under CI.**
- All 76 rows pass in 53–67 s, the build included.
- With `CI=true` and `POSTGRES_PASSWORD` unset, it fails with "integration test requires
  POSTGRES_PASSWORD under CI".
- Locally, with nothing set, it skips.

**Calibration found what the rows now pin:**
- Under `single_user_hardened`, CI's sample object-store keys stop every `chat` verb. The binary
  gets non-sample keys and `filesystem-dev`.
- `config set` writes `$HOME/.aura/llm.json`, and `skills create` writes `$AURA_SKILLS_DIR`. Both
  are the run's own.
- `skills create`, `update` and `always` record in `aura.skill_audit`, not in `skill_catalog`;
  `always` records as `update`.
- `chat delete` removes the row; it does not mark it.
- `identity recover` mints a break-glass token without any prompt. My first exemption for it was
  wrong, and it is now a row.
- **C9, new.** On a fresh install `mcp profile list` shows `* default`. After
  `mcp profile use <other>`, `default` is gone from the list, and `mcp profile use default`
  answers "MCP profile \"default\" not found". This lasts until a server has been installed into
  `default`. It was reproduced on a fresh database, and the row
  `mcp_profile_use_default_after_switching_is_not_found_C9` pins it.

**CI found two things the local runs could not.** CI on `257cc73` was red in two jobs; both are
fixed:
- **`chat_delete` and `docs_search` depended on the host.**
  - In "Race + leak DB tier" there is no ArcadeDB, so both rows met connection refused on
    `127.0.0.1:2480`.
  - In the coverage gate, ArcadeDB is up but has no tenant for the run's identity, so both met
    "http 403: User/Password not valid".
  - Locally an ArcadeDB from an earlier run held the tenant, so both rows passed.
  - The fix (`9697b2e`): the binary now gets ArcadeDB and the embedder on a closed port, as its
    model route already was. The rows pin what the CLI does without them:
    - `chat delete` refuses with exit 1 and "delete lifecycle", because it must also erase the
      memory derived from the conversation, and the conversation stays whole;
    - `docs search` exits 1 with "route document cards".
  - The success path of `chat delete` needs a provisioned tenant, which this tier no longer
    reaches.
- **A data race in `internal/conversations`, unrelated to the CLI.**
  - The parallel test `TestWarnIfOversized_LogsOverThresholdSilentUnder` swapped the
    process-wide logger. Another parallel test's `slog.Warn` then wrote into its buffer while it
    read it. CI's `-race` report names both sides.
  - The test is now serial (`9c9ef4c`). 8 local `-race` runs did not reproduce the race before
    the fix; after it, `-race -count=5` and `-count=2` with `db_integration` are green.

**The rows reach the code the audit found untested (A5).** Measured at `257cc73`, before the two
rows above were made host-independent. These are `cmd/aura` statements with
`db_integration`. "Tests" means the package's own tests; "tests + smoke" merges the counters of
the instrumented binary each row ran (`GOCOVERDIR`, `go tool covdata merge`):

| File | Statements | Tests | Tests + smoke |
|---|---|---|---|
| `chat.go` | 126 | 0.0% | 61.1% |
| `db.go` | 75 | 0.0% | 62.7% |
| `skills_share.go` | 72 | 0.0% | 25.0% |
| `gateway_grants.go` | 68 | 0.0% | 54.4% |
| `web.go` | 66 | 0.0% | 10.6% |
| `paused_states.go` | 61 | 0.0% | 47.5% |
| `recover_operator.go` | 47 | 0.0% | 0.0% (exempt: interactive) |
| `skills.go` | 135 | 3.0% | 63.0% |
| `identity.go` | 84 | 7.1% | 65.5% |
| `task.go` | 270 | 10.4% | 63.3% |
| **`cmd/aura`** | 10,147 | **54.9%** | **63.4%** |

The `cmd/aura` row's 54.9% is this tree. The audit's 55.3% was taken at `e0c6d77`.

**Memory verbs, live.**
- **Setup.** The stack followed the skill: `arcadedb-mcp` and `aura serve` built from `257cc73`,
  on `aura_e2e`, with memory mounted (`tools=13`).
- **Every row exited 0.** All 16 invocations covered the 15 argv shapes the contract checks, plus
  one extra `remember` that set up the merge.

| Verb | Result |
|---|---|
| `remember` (default run, then `--supersedes --subject-kind --object-kind --run import-2026`) | both written; `search` returns the superseding fact |
| `search`, `search --as-of` before the facts existed | the fact; then `"abstained":true` |
| `facts`, `facts <predicate> --as-of` | the fact |
| `entities`, `digest`, `schema` | the graph's lists and text |
| `merge "G. Verdi" "Giulia Verdi"` | `"moved":1` |
| `forget --entity` (dry run) | `{"dry_run":true,"entities":4,"facts":3}` |
| `forget --run import-2026` (dry run) | `"facts":1`: the run named by the actor header is found |
| `forget --subject … --object Bologna --apply` | `{"entities":1,"facts":1}` |
| `reembed`, `reembed --all` | `"embedded":0`, then `"embedded":5` |

## Gates

- `go vet` and `golangci-lint` v2.14.0, with and without `db_integration`: 0 issues. The lint
  found one unused field (`stdin`), which was removed.
- `go test -race ./cmd/aura`: green.
- Coverage gate (`bash scripts/coverage_docker.sh`, smoke included, at `257cc73`): 50137/57194 =
  87.66%, and the package-local policy passed.
- Mutation on the memory mapping functions, with the contract and memory tests only.
  - **First run.** It left 6 survivors, all in code older than this slice:
    - `facts --as-of`;
    - `merge`'s source;
    - four rewrites of `takeFlag`'s bounds check.

    `68bc5a9` adds tests that kill each one, checked mutant by mutant. That run was then stopped,
    because the disk filled and a failed build looked like a kill.
  - **Second run.** 86 mutants of `memory.go`: 15 do not compile, 70 are killed and 1 survives.
  - **The survivor.** `arg`'s blank-argument check: no test passed a blank positional such as
    `aura memory facts ""`. `TestABlankPositionalIsMissing` kills it, checked by hand. Final: **71
    of 71** compiling mutants killed.
  - **The cost.** All 86 mutants were done in about 25 minutes. go-mutesting then spent more than
    40 minutes type-checking the other 160 files of `package main` one by one. `--match` filters
    after each file has loaded, and none of those files had a function to mutate. It was stopped
    by hand. Mutating `cmd/aura` stays this slow until its pure functions leave `package main`,
    which is step 4 of the split spec.
- CI on `9697b2e`: green in every workflow (CI with both `db_integration` jobs that run the smoke,
  CodeQL, Skills, Publish Aura edge image, Publish ArcadeDB MCP, create-aura-appliance).
  - `257cc73` had been red, for the two reasons recorded above.

## What this does not show

- Interactive verbs, and the 24 exempt ones. Each exemption names who covers it, if anyone.
- `chat delete` succeeding. The row pins its refusal while memory is unreachable.
- The memory verbs live in CI: they are checked there only at the contract tier.
- That an operator's real sequence of commands works. The rows run one verb at a time.
- The lab VM, and Windows.
