# CocoIndex's Neo4j connector on ArcadeDB, and ArcadeDB 26.10.2

Date: 2026-10-23. The operator asked what cocoindex's Neo4j connector lacks to speak to ArcadeDB
natively ("guarda cosa manca al driver Neo4J di cocoindex per parlare nativamente a ArcadeDB"). The
answer showed that 26.10.1 ignores the connector's constraint names, which 26.10.2 fixes. The
operator then chose ArcadeDB 26.10.2 on the JVM now, and cocoindex 1.0.26 in a slice of its own
("ArcadeDB 26.10.2 JVM, poi cocoindex"). Follows
[2026-10-15-cocoindex-neo4j-connector-on-arcadedb-native.md](2026-10-15-cocoindex-neo4j-connector-on-arcadedb-native.md).

**Verdict:**
- **Rows need nothing.** Every row statement the connector sends works on 26.10.1 and 26.10.2:
  `MERGE … SET n += $props`, `DETACH DELETE`, the triple `MERGE` of an edge, and 1.0.26's
  `CALL { … } IN TRANSACTIONS` table drop.
- **The schema has no Cypher path on ArcadeDB.** That is why `services/ingest/arcade.py` writes it in
  SQL over HTTP before the first Bolt write. Four pieces are missing:
  - the typed vector property: a `MERGE` declares nothing, and SQL then refuses `LSM_VECTOR` on the
    undeclared property;
  - the vector index: `CREATE VECTOR INDEX` is refused by the parser;
  - full text: `CREATE FULLTEXT INDEX` is refused the same way;
  - the database: Cypher has no `CREATE DATABASE`.
- **26.10.1 ignores the connector's constraint names.** `DROP CONSTRAINT coco_uniq_… IF EXISTS` is
  a silent no-op, and the connector's unique index outlives its table. 26.10.2 records the name and
  drops exactly what the constraint created.
- **Aura's order is safe on both releases.** With the unique index created in SQL first, the
  connector's constraint is a no-op, and its drop leaves the SQL index in place.
- **26.10.2 changes nothing Aura measured.** On the live tier, it gives the same 613 passes and the
  same skips as 26.10.1, and the same set of server errors. The lab VM's databases open
  identically, and give the same answers, on 26.10.2 and back on 26.10.1.
- **cocoindex 1.0.26 deletes what 1.0.25 leaked.** When a table is rebuilt (a primary key change)
  or unmounted, 1.0.26 deletes every node with the label, rows written outside cocoindex included.
  1.0.25 left them all in place, untracked.

## Setup (workstation, local Docker 29.8.1)

- **The servers.** Throwaway ArcadeDB containers, one at a time, from Aura's pinned digest and the
  26.10.2 release:

  | Label | Image |
  |---|---|
  | 26.10.1 | `arcadedata/arcadedb@sha256:01506549…` (compose's pin) |
  | 26.10.2 | `arcadedata/arcadedb@sha256:7a498ebb…` |

  They had the settings compose gives the real one: the root password, `defaultDatabases=aura_memory[]`,
  the Bolt plugin, `--memory 3g` and `-Xms2G -Xmx2G`. The running `aura-arcadedb` was never
  touched.
- **The writer.** `python:3.12-slim` with `cocoindex==1.0.25` (Aura's pin) or `1.0.26` (released
  2026-10-22), both with `neo4j==5.28.1`.
  - The raw statements are the strings the connector's own `_cypher` builders return, so they
    are exactly what it sends.
  - HTTP goes through `services/ingest/arcade.py`'s helpers.
- **Scripts.** They live in the session scratchpad only, as on 2026-10-15.

## 1. The connector's statements

| Statement (connector unless marked) | 26.10.1 | 26.10.2 |
|---|---|---|
| `CREATE CONSTRAINT … REQUIRE n.k IS UNIQUE` (table create) | passes | passes |
| `MERGE (n:L {k: $key_0}) SET n += $props` | passes | passes |
| `CREATE INDEX … FOR ()-[r:T]-() ON (r.id)` (relation table create) | passes | passes |
| triple `MERGE` of an edge, `SET r += $props` | passes | passes |
| `CREATE VECTOR INDEX … OPTIONS {indexConfig: …}` (`declare_vector_index`) | `SyntaxError`: "Only standard, RANGE and TEXT index types are supported" | same |
| `DROP INDEX … IF EXISTS` | passes | passes, `indexes_removed: 1` when it exists |
| `MATCH ()-[r:T]->() CALL { WITH r DELETE r } IN TRANSACTIONS` (1.0.26) | passes, 2 of 2 deleted | same |
| `MATCH (n:L) CALL { WITH n DETACH DELETE n } IN TRANSACTIONS` (1.0.26) | passes, 4 of 4 deleted, the SQL-written node included | same |
| `DROP CONSTRAINT … IF EXISTS` | passes, removes nothing (section 3) | passes, `constraints_removed: 1` |
| [not emitted] `CREATE FULLTEXT INDEX … ON EACH [n.text]` | `SyntaxError`, same message | same |
| [not emitted] `REQUIRE n.e IS :: VECTOR<FLOAT32>(4)` | `SyntaxError`: unsupported type | same |
| [not emitted] `REQUIRE n.e IS :: LIST<FLOAT>` | `SyntaxError`: unsupported type | passes, declares `LIST` of `DOUBLE` |
| [not emitted] `CREATE DATABASE x` | `SyntaxError`: "Only CREATE CONSTRAINT, CREATE INDEX and CREATE USER are currently supported" | same |
| SQL `LSM_VECTOR` on the vector a `MERGE` wrote | `SchemaException`: "the property does not exist" | same |
| SQL `LSM_VECTOR` on the property the `LIST<FLOAT>` constraint above declares | fails: the constraint failed, so the property does not exist | passes on the declared, still empty property; Cypher writes into it were not measured |
| `vector.neighbors` over the vectors a `MERGE` wrote | "No vector index found" | same |

The parser code agrees: `CypherASTBuilder.handleCreateIndex` builds only `INDEX`, `RANGE` and
`TEXT`, and `executeCreateIndex` always builds an `LSM_TREE`. That holds at 26.10.1 and on `main`
as of 2026-10-23.

## 2. The connector end to end

**Rows.** Twenty rows went into one node table, with Aura's order: the typed `ARRAY_OF_FLOATS`
property, its `LSM_VECTOR` and the unique key created in SQL before any Bolt write. One row was then
written in SQL, outside cocoindex. Each phase is a new process on the same cocoindex state.

**Indexes.** In every phase of both releases, both versions kept `Probe[k]` UNIQUE and
`Probe[embedding]` `LSM_VECTOR`, the indexes created in SQL. A rebuild added the connector's
`Probe[k2]`. When the table was unmounted, 26.10.1 kept that index and 26.10.2 dropped it.

| Phase | cocoindex 1.0.25 | cocoindex 1.0.26 |
|---|---|---|
| load | 21 rows, the SQL row present, `vector.neighbors` top 3 r00 r01 r03, 0 errors | same |
| primary key `k` → `k2` (the table is rebuilt) | 21 rows, the SQL row still present | 20 rows: every node was deleted, the SQL row included, then the 20 were written again |
| table unmounted | 21 rows: nothing deleted, and nothing tracks them any more | 0 rows |
| `declare_vector_index` on a fresh table | `app.update()` raises `CypherSyntaxError`; the last snapshot says `running`, 0 errors; the 20 rows were written anyway, with no vector index | same |

Both server releases gave these same counts. A failed vector index is therefore an exception out of
`watch()`, not an error count. `services/ingest/app.py` never declares one.

## 3. Constraint names

A connector constraint, created and then dropped in a new session. `Own` has no prior index.
`Sql` has a unique index created in SQL first, which is Aura's order.

| | 26.10.1 | 26.10.2 |
|---|---|---|
| `Own`: `SHOW CONSTRAINTS` after the create | listed as `Own[x]`, not under its name | listed as `coco_uniq_Own__x` |
| `Own`: after `DROP CONSTRAINT coco_uniq_Own__x IF EXISTS` | `Own[x]` still there | `Own[x]` gone |
| `Sql`: after the create | no new constraint, only `Sql[x]` | same |
| `Sql`: after the drop | `Sql[x]` still there | same |

The named-constraint record came with ArcadeDB #9707 and #9732 (merged 2026-10-11), which are
in 26.10.2 and not in 26.10.1. Its release notes add one caveat: a named constraint created by an
older release has no record, so a `DROP CONSTRAINT <name>` without `IF EXISTS` raises for it. The
connector always sends `IF EXISTS`.

## 4. Write conflicts

Two Bolt transactions set the same node. The second to commit got
`Neo.TransientError.Transaction.DeadlockDetected` ("Slot rebase not possible … concurrent change to
the same record"), a `TransientError` on both releases, and the first write stayed.
- cocoindex 1.0.26's `_is_retryable` returns true for it, so 1.0.26 retries the batch.
- 1.0.25 has no retry: the batch fails and its components are retried by the engine.

## 5. Aura's live tier on 26.10.2

`go test -race -count=1 -tags arcadedb_integration ./internal/arcadedb/ ./internal/docfields/...`,
run against each throwaway server:

| | 26.10.1 | 26.10.2 |
|---|---|---|
| Top-level tests | 613 passed, 5 skipped | 613 passed, 5 skipped |
| Skipped | the 5 tests that need the embedder, which was not running | the same 5 |
| Wall time | 95 s | 108 s |
| `SEVER` lines in the server log | 63 | 63 |
| Distinct errors behind them | 8, all "Type/Index with name … was not found" from tests that read a missing type | the same 8 |

The release notes were read against Aura's statements:
- **`UPDATE … UPSERT` now needs a UNIQUE index on every property of its WHERE (#9339).** Every
  upsert in `internal/arcadedb` and `arcade.record_status` has one.
- **`memory_pole.go` upserts on a subtype, under the supertype's index.** It passes in the tier
  above.
- **`sum()` over an empty group is now NULL.** `rowInt` reads a NULL as 0, so `CorpusWork` is
  unchanged.
- **A zoned `datetime()` written into a `DATETIME` property is now stored as an instant.** The
  ingest writes `indexed_at` in UTC, so its wall clock was already UTC.
- **Not used by Aura:** `COLLATE ci`, the weighted path finders, `path.*`, `include()`/`exclude()`,
  a bare `ORDER BY @rid` on a SQL `MATCH`, and `SEARCH_FIELDS`.

## 6. The lab VM's data on 26.10.2

**The archives.** The newest backup of each tenant database came from the lab VM's
`aura_aura-arcadedb-backups` volume, read-only there:
- the system identity's database, from today;
- the operator's database, from 2026-10-13, for the reason in section 7.

**The run.** The archives were restored into an empty volume under 26.10.1. The same volume was then
opened by 26.10.2, and then by 26.10.1 again.

| Database | Types | Type indexes | Records | 26.10.2 against 26.10.1 | 26.10.1 again |
|---|---|---|---|---|---|
| `mem_448ddbe1…` (operator) | 25 | 40 | 3,211 | identical | identical |
| `mem_00000000…0039` (system) | 22 | 29 | 0 | identical | identical |
| `aura_memory` | 0 | 0 | 0 | identical | identical |

**The reads.** Each of the operator database's 11 single-field `LSM_VECTOR` and `FULL_TEXT` indexes
was read on each of the three opens:
- `vector.neighbors` k=5 for the vector of the first record;
- `SEARCH_INDEX` for a word of the first record's text.

All 33 answers were identical, and every vector probe found itself first. The server logged no
`SEVER` and no `WARNI` line on any of the three opens.

**Before the update, on the VM.** It ran 26.10.1, with 3 databases and no invalid index:
- the operator's database had 31 types and 53 type indexes, holding 82 passages, 8 documents,
  22 entities, 60 turns and 21 reasoning traces;
- the system database had 22 types and 29 type indexes.

## 7. Found on the way

- **The operator's database has had no backup since 2026-10-13.** The VM's ArcadeDB logs
  `AccessDeniedException` on `backups/mem_448ddbe1…/` every hour.
  - The directory and its one archive belong to `root:root`, and the JVM server runs as UID 1000.
  - The other two databases back up hourly.
  - The directory was last changed on 2026-10-14 at 09:19, the day the native image, which runs as
    UID 0 by default, was measured on that VM. That this is the cause is a hypothesis.
  - Not repaired here: it is a change on the VM by hand, so it is left to the operator.
- **ArcadeDB logs its levels as five letters, `SEVER` and `WARNI`.** A `grep SEVERE` counts nothing.

## Not shown here

- **The 5 embedder tests** on 26.10.2. CI's Agent Memory MRS job runs them with EmbeddingGemma.
- **The ingest sidecar's own suite** and the memory MCP's live tier on 26.10.2. CI runs both
  through compose, so they meet the new pin on its push.
- **The operator database's schema since 2026-10-13.** Its 6 newer types and 13 newer type indexes,
  among them the document fields, were not opened from 26.10.1 data. The live tier creates them on
  26.10.2 from scratch.
- **Data that 26.10.2 writes, read back by 26.10.1,** beyond opening and reading the restored copy.
- **Repeats, load and concurrency.** One run per configuration, at test scale.
- **The native image of 26.10.2**, which the spec of 2026-10-17 leaves to 26.11.1.
