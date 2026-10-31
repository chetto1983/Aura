# Aura's harness on ArcadeDB 26.11.1-SNAPSHOT

Date: 2026-10-31. Roberto Franchini wrote that 26.11.1 ships this week and asked us to open issues for
anything that does not work. The operator asked to bring the snapshot up and try it with the harness
("tira su la versione di docker e proviamola con tutto l'harness di aura"), and then what the release
removes from our code ("cosa toglie nel nostro codice questa release?").

**Verdict: no regression on any tier that ran.**
- **One engine bug found, and it is older than this release:**
  [ArcadeData/arcadedb#11040](https://github.com/ArcadeData/arcadedb/issues/11040). It is also on 26.10.2.
- **The release removes nothing from our code.** The four vector DDL statements stay in `arcade.py`, by
  the verdict of [2026-10-28-cypher-vector-index-on-arcadedb-snapshot.md](2026-10-28-cypher-vector-index-on-arcadedb-snapshot.md).
- **This does not clear 26.11.1 for the bump.** See "What this does not show".

## Setup (workstation, Docker Desktop through WSL)

- **The engine.** `arcadedata/arcadedb:26.11.1-SNAPSHOT@sha256:3067e2f32d80aaccc95d4f31be0d76e562e465a71e24a75753481b068b5ea0f3`,
  build `4e6cd888f`, image created 2026-10-31T10:44Z, 530 commits after 26.10.2.
- **The comparison engine.** `26.10.2`, the `compose.yaml` pin, in a throwaway container on port 12480.
- **The code.** A shallow clone in the WSL filesystem, so concurrent sessions could not move it.
  `84ee10717` for the live tier and the ingest suite, `f0cf5a408` for every later tier. Between them
  `9697b2e43` fixed a host dependency in `TestCLISmoke`.
- **The isolation.**
  - Compose project `aurasnap`, so every volume carried that prefix. A render check refused to start
    if a real database, object or backup volume appeared.
  - `.env` held CI's placeholders and fresh random Postgres and ArcadeDB passwords, so a misdirected
    client would be refused by a real server. No provider key and no Telegram token.
  - The overlay joined the existing `aura_default` network and mounted the embed model read-only from
    `aura_aura-llama-embed`.
  - The harness ran unchanged: the two real containers stepped aside by `docker rename`, so the scripts
    found the names they expect.

## Results on the snapshot

| Tier | Command | Code | Result |
|---|---|---|---|
| Live `arcadedb_integration` | `go test -race -p 1` on `internal/arcadedb`, `internal/docfields`, `internal/runner`, `cmd/arcadedb-mcp`, `cmd/aura` | `84ee107` | 3141 passed, 3 failed |
| Ingest suite | `make ingest-test` | `84ee107` | 273 passed |
| Coverage gate | `scripts/coverage_docker.sh` | `f0cf5a4` | passed, 87.71% (50281/57326), package policy passed, `empty_tiers` 0 |
| Two identities | `make musr-e2e`, again with `GOFLAGS=-v` | `f0cf5a4` | 23 run (5 top-level, 18 subtests), 0 skipped, 0 failed |
| Reconciliation | `make ingest-reconcile` | `f0cf5a4` | add, unchanged rerun, modify, delete, live second cycle, two-identity isolation all ok |
| DR drill | `scripts/restore_drill.sh` | `f0cf5a4` | 4 of 4 planes, checksums ok. ArcadeDB RTO 128 ms on a 2 KB fixture |
| Agent Memory | `make agent-memory-eval` | `f0cf5a4` | `MRS=100.00`, `memory_search` p95 157.7 ms (limit 1000 ms) |
| Load and chaos | `scripts/production_load_chaos.py --race` | `f0cf5a4` | passed, concurrency 32, 4 of 4 scenarios |

**The engine log.** 23790 lines, 130 `SEVER`, 0 `WARNI`. All 130 are accounted for: 129 `SchemaException`
for a type or index that a fresh database does not have yet, and 1 `IllegalStateException` from the edge
probe below. 0 restarts, 0 OOM kills.

### The three red tests of the live tier

None comes from the release.

- **`TestMemoryCaptureLive_ExplicitUserEvent` and `TestMemoryCaptureLive_DurableArtifactEvent`.** Both fail
  with `zero real ToolInvocationEnd events`, and `TestMemoryCaptureLive_TerminalBarrier` hangs on a
  channel at line 473, not in an ArcadeDB call. The three behave the same on 26.10.2, run with a 150 s
  timeout. No gate runs `internal/runner` with this tag, so this is a dark tier. The cause was not
  looked for.
- **`TestAgentMemoryCLILiveSearchP95`.** This run had not started the sidecar on `:8096`. It passed inside
  the Agent Memory gate.

The first coverage run also stopped on two `TestCLISmoke` rows, with `http 401: User/Password not valid`.
That is the host dependency `9697b2e43` fixed: its message records the same rows failing on CI on
26.10.2 with `http 403`. The status differs because of upstream #10180, and
`CredentialAccepted` already accepts both.

## ArcadeData/arcadedb#11040

Eight clients run `CREATE PROPERTY Entity.name IF NOT EXISTS STRING` at the same moment on a new database.
The published script, 100 rounds per line:

| Version | Clients | Refused | Rounds that lost the property |
|---|---|---|---|
| 26.11.1-SNAPSHOT | 8 | 25 of 800, HTTP 409 | 4 of 100 |
| 26.11.1-SNAPSHOT | 1 | 0 of 100 | 0 of 100 |
| 26.10.2 | 8 | 108 of 800, HTTP 500 | 7 of 100 |
| 26.10.2 | 1 | 0 of 100 | 0 of 100 |

In the rounds that lose it, the property is in memory but not in `schema.json`, before the close and
after. `close database` / `open database` then shows the type without it. Two more snapshot runs of 50
rounds lost it in 3 and 4. One 20-round run right after the live tier refused nothing, so the race is
timing sensitive.

The loss was found by a check added before publishing, to back a sentence that had not been measured.

## What the release removes

Each candidate was run on both engines, so a probe could tell them apart.

| Code | Measured | Decision |
|---|---|---|
| Four vector statements in `arcade.py` | The DDL of cocoindex passes on the snapshot, and 26.10.2 refuses it with `Only standard, RANGE and TEXT index types are supported` | Keep, by the 2026-10-28 verdict: cocoindex does not order the index before the rows |
| Full-text indexes in `arcade.py` | cocoindex 1.0.26 emits none. A Cypher-named index makes `SEARCH_INDEX('Passage[text]')` answer 500 | Keep |
| Row copy in `merge.go` | `UPDATE FACT SET @out` on an indexed edge fails the same way on both | Keep |
| `tenantOpenSlots` | Concurrent schema passes: 31 HTTP 500 on 26.10.2, 9 HTTP 409 on the snapshot, over 6 rounds | Keep until #11040 closes |
| `.trivyignore.yaml` netty entry | `netty-handler` 4.1.136.Final in both images | Keep |
| `fact_lock.go`, `upsertEntityWithRetry` | 0 lost writes and 0 refused UPSERTs on BOTH engines | No evidence: these probes cannot fail, so they prove nothing |

## What this does not show

- **Not the release.** One snapshot build. On 2026-10-31 the milestone had 60 open issues.
- **Web E2E did not run**, and it has an ArcadeDB spec, `web/e2e/graph-live-arcadedb.spec.ts`.
- **No real model.** `agent-eval`, `running_aura_conversation` and the LLM phases of the ingest gate did
  not run. No agent turn was scored.
- **Other tiers did not run:** `make quality`, Go and web mutation, and the Telegram, WhatsApp, calendar,
  multimodal, reasoning and sandbox Docker tiers. None of them talks to ArcadeDB on the paths checked
  here, but that is a reading of their jobs, not a measurement.
- **Not on the lab VM,** and not on data from an earlier version. Every database was created by this
  engine. Opening 26.10.2 archives or volumes with 26.11.1 was not tried.
- **Load and chaos barely touch the engine.** The gate uses `MCPFixture` for memory.
- **#10964 and the 2026-10-28 order.** #10964 merged after that measurement. Rows written before their
  index were not re-measured on this build.
- **The native image was not tried.**

## State after the run

- `aurasnap` is gone: 0 containers, 0 volumes, the clone and its `.env` removed. The images built for this
  run stay, including `aura-arcadedb-mcp:latest`, which the real project uses when it starts that service.
- **The real volumes were not touched.** Files changed during the run: 0 of 15 in `aura_aura-arcadedb`,
  0 of 27 in `aura_aura-arcadedb-backups`, 0 of 1595 in `aura_aura-postgres`, 0 of 3973 in
  `aura_garage-data`, 0 of 26 in `aura_aura-home`. A control file written at the same moment was found.
- **The two renamed containers disappeared during the run, and it is not known how.** Docker's event log
  no longer reached that far, and no script of this run names them. They held no data. `aura-llama-embed`
  was recreated from `D:\Aura` and is healthy.
- `aura-arcadedb` was already down, since the 14:20 reboot: `backup.json`'s bind mount from `/mnt/d` failed
  with `not a directory`. The container no longer exists. `docker compose up -d arcadedb` from `D:\Aura`
  recreates it on the same volume. That was left to the operator.
