# Cypher `CREATE VECTOR INDEX` on ArcadeDB 26.11.1-SNAPSHOT, through cocoindex

Date: 2026-10-28. ArcadeDB closed #10678 (our issue) today with #10864. On `main`, Cypher now accepts
`CREATE VECTOR INDEX` and `CREATE FULLTEXT INDEX`. The operator asked whether this lets us remove parts
of the ingest sidecar ("se possiamo togliere pezzi da ingest sidecar"). The only candidate is the vector
part of `services/ingest/arcade.py`: two `CREATE PROPERTY … embedding ARRAY_OF_FLOATS` statements and
two `LSM_VECTOR` indexes. cocoindex's own `declare_vector_index` would replace them. Before deciding,
the operator asked to measure two things on a `main` snapshot ("misura i punti 1 e 3 su snapshot main"):

1. **The index name.** cocoindex can only produce names that match `[a-zA-Z_][a-zA-Z0-9_]*`, but the Go
   reader asks for `Passage[embedding]`.
2. **The order.** When cocoindex creates the index, does it reach the server before or after the
   first row?

Follows
[2026-10-23-cocoindex-neo4j-connector-and-arcadedb-26-10-2.md](2026-10-23-cocoindex-neo4j-connector-and-arcadedb-26-10-2.md).

**Verdict: keep the SQL DDL in `arcade.py`. Do not move the vector index to cocoindex.**
- **The name is not a problem.** The Go reader keeps working whatever cocoindex calls the index.
- **The order is a problem.** cocoindex does not order the index before the rows. In the sidecar's
  shape, the rows reached the server first in 3 runs out of 3. Those rows are stored as `LIST` under
  a property declared `ARRAY_OF_FLOATS`. Every read measured here still works on them, but the drift
  is permanent: a memoized row that never changes is never rewritten. Today's
  `ensure_schema`-before-Bolt rule exists to prevent exactly this.
- **The gain would be four DDL statements.** The HTTP channel stays for the database, `embed_space`
  (`NULL_STRATEGY INDEX`), the full-text indexes (cocoindex does not emit them), the status UPSERT,
  `remove_document`, the audit and the readiness wait.

## Setup (cloud session container, Docker 29.9.2)

- **The server.** `arcadedata/arcadedb:26.11.1-SNAPSHOT`,
  `sha256:5e6e675bd576e23c44a610318409e4e2cafd6dd47b2fc9ec6ee4213049ca9ff1`, image created
  2026-10-28T18:43:38Z.
  - No alpha or RC tag exists. The newest release is still 26.10.2.
  - The image contains `CypherIndexOptions`, the class #10864 added.
  - Settings: root password, the Bolt plugin and `-Xms2G -Xmx2G`.
- **The writer.** `cocoindex==1.0.26` and `neo4j==6.4.1`, the pins in
  `docker/aura-ingest/requirements.txt`.
  - The raw statements are the strings the connector's own `_cypher` builders return.
  - SQL goes over HTTP through `services/ingest/arcade.py`'s helpers.
  - The statement order is the neo4j driver's own `C: RUN` debug log.
- **Scripts.** They live in the session scratchpad only, as on 2026-10-23.
- **Vectors.** Dimension 4, cosine, five rows.

## 1. The index name

| Case | Result |
|---|---|
| Empty database, cocoindex's `CREATE VECTOR INDEX` | creates `Passage`, declares `embedding ARRAY_OF_FLOATS`, builds an `LSM_VECTOR` index under the given name |
| `vector.neighbors('Passage[embedding]', …)` on that index | finds all 5 rows |
| The Go reader's shape (`filter` + `maxDistance`) on `Passage[embedding]` | finds all 5 rows |
| `vector.rerank(…, 'embedding', …)` | scores identical to an index built in SQL |
| Database built by `arcade.ensure_schema`, then cocoindex's `DROP` + `CREATE` | both no-ops: `Passage[embedding]` stays the only vector index |
| …then cocoindex's unmount `DROP INDEX … IF EXISTS` | no-op: `Passage[embedding]` survives and still answers |
| `vector.neighbors('vec_Passage__embedding', …)` on that database | `SchemaException`: no index with that name |

**Why `Passage[embedding]` resolves.** `SQLFunctionVectorNeighbors.execute` (on `main`) parses
`TYPE[property]` and looks the index up with `getPolymorphicIndexByProperties`, by property and not
by name. Only a bare name goes through `getIndexByName`.

**The index name is `coco_vec_Passage__embedding`.** cocoindex 1.0.26 adds the `coco_` prefix when it
sends the statement.

## 2. The order

Each run uses a fresh database and a fresh cocoindex state. The column shows the order the driver
logged.

| Mode | Run 1 | Run 2 | Run 3 |
|---|---|---|---|
| `declare_vector_index` before `declare_record`, both in `app_main` | rows first → `LIST` | index first → `ARRAY_OF_FLOATS` | rows first → `LIST` |
| `declare_record` before `declare_vector_index` | rows first | index first | rows first |
| app.py's shape: index in `app_main`, records in `mount_each` children | rows first | rows first | rows first |

The order in the code does not decide it. In every run, the unique constraint went first, then the
rows and the index in either order.

**What a row written before its index looks like:**
- The property is declared `ARRAY_OF_FLOATS`, but the record stores `embedding.type() = LIST`.
- The index built at `CREATE` time indexes it.
- `vector.neighbors` and `vector.rerank` find it and score it correctly, before and after a server
  restart and after `REBUILD INDEX`.
- Rows written after the index are `ARRAY_OF_FLOATS`.
- A second update rewrote only the changed rows. Unchanged `LIST` rows were skipped by the memo and
  stayed `LIST`.

**Raw statements, same result.** `MERGE` three rows and then `CREATE VECTOR INDEX`: the statement
succeeds, the three rows stay `LIST`, and the two written after are `ARRAY_OF_FLOATS`.

## The server log

Over the whole session, the restart and the `REBUILD INDEX` included, the server logged 1 `SEVER`
line: the `SchemaException` "Index with name 'vec_Passage__embedding' was not found". It is the
lookup by name that section 1 provoked on purpose. Nothing else was logged at `SEVER`. The `WARNING`
lines are the JVM's own, about `jdk.incubator.vector` and `sun.misc.Unsafe` from undertow.

## What this does not show

- **Only the operations listed above.** The `vector.fuse` + `groupBy` pipeline of
  `document_retrieval.go`, quantized indexes, 768 dimensions and corpora larger than five rows were
  not tested on `LIST` rows.
- **A snapshot, not a release.** 26.11.1 can still change before it ships.
- **`CREATE FULLTEXT INDEX` was not measured.** cocoindex does not emit it, so moving the full-text
  indexes would only change the transport, from HTTP to Bolt.
- **Not on the lab VM, and not with the real agent.** This is a measurement of the connector and the
  engine, not an E2E acceptance.
