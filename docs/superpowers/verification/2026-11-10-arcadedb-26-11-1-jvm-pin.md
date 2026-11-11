# ArcadeDB 26.11.1 on the JVM

Date: 2026-11-10. The operator saw that ArcadeDB had shipped a new release ("Guarda anche ArcadeDb
sono usciti con una nuova release") and asked for the pull request ("Procedi con la PR di ArcadeDB").
The scope is the JVM pin, as on 2026-10-23. The native image and the `where` filter stay with
[the 2026-10-17 spec](../specs/2026-10-17-arcadedb-26-11-native-design.md), which still has an open
question for the operator.

**Verdict: the pin moves to 26.11.1. Nothing measured changes, and the floor stays 26.10.1.**
- Aura's live tiers give the same results on 26.10.2 and on 26.11.1, test by test.
- A memory database written by 26.10.2 opens on 26.11.1 with the same records, vector neighbours,
  distances and full-text answers. Aura's own schema pass and live tier then run on it unchanged.
- The image drops brotli-libs, libexpat, musl and guava-android. netty-handler 4.1.136.Final
  (CRITICAL CVE-2026-75595) is still shaded into `arcadedb-ha-raft`, so its `.trivyignore.yaml`
  entry stays.
- **One-way.** Once 26.11.1 has written, 26.10.2 cannot read its vectors. Going back means restoring
  the last backup 26.10.2 took (section "Back to 26.10.2").

## Setup

- **The engines.** The release tarballs `arcadedb-26.10.2.tar.gz` and `arcadedb-26.11.1.tar.gz`, on
  JDK 25.0.5, with compose's `JAVA_OPTS` (root password, `aura_memory` default database, Bolt plugin)
  and a 1 GiB heap. One at a time on `127.0.0.1:2480`.
- **The image.** `arcadedata/arcadedb:26.11.1@sha256:22a049898f34f01c256b56a1ba95fb01d32e5e14755d3c4367415c8e55ae5035`,
  the multi-arch index (amd64, arm64), read from Docker Hub. Its user is still `arcadedb`, UID and
  GID 1000, as in 26.10.2, so the `aura-arcadedb` volume keeps its owner.
- **The code.** `master` at the branch point of this pull request. `CI` unset.

## Results

| Tier | 26.10.2 | 26.11.1 |
|---|---|---|
| `arcadedb_integration` on `internal/arcadedb`, `internal/docfields` | 1500 passed, 5 skipped | 1500 passed, 5 skipped |
| `arcadedb_integration` on `cmd/arcadedb-mcp`, with a tenant secret | 188 passed, 4 failed, 5 skipped | 188 passed, 4 failed, 5 skipped |
| Server log, `SEVERE` or `ERROR` | 0 | 0 |

- The 5 skips of the first tier are the same 5 embedder tests on both.
- The second tier's results are identical name by name, subtests included. Its 4 failures and 5
  skips all report `EmbeddingGemma is unreachable` on `127.0.0.1:8081`: no embedder ran here. CI's
  `arcadedb_integration` job brings the embedder up, so it runs them on the new pin.

## A 26.10.2 database opened by 26.11.1

The `aura_memory` database left by the live tier on 26.10.2 holds Aura's full memory schema, with
its `LSM_VECTOR` indexes (768 dimensions, COSINE, no quantization) and full-text indexes. A script
added deterministic data: 200 `Entity` vertices, 500 `FACT` edges with a statement, a 768-float
embedding and a `valid_from`, and 300 `ConversationTurn` vertices with content and an embedding.
The server was stopped, the database directory copied into the 26.11.1 distribution, and 26.11.1
started on it.

| Probe | Result |
|---|---|
| Record counts of the three types | same |
| `vector.neighbors` top 10 on `FACT[embedding]` and `ConversationTurn[embedding]`, keys and distances to 6 decimals | same |
| `SEARCH_INDEX` counts on `FACT[statement]` and `ConversationTurn[content]` | same (165, 156) |
| Every index's name and `valid` flag, every type's name | same |
| A `DATETIME` read back | same (`2026-11-01 10:00:00.000`) |

The 26.11.1 log loaded the vector graph from disk (`500 nodes`). The live tier then ran on this
database with 26.11.1: 1500 passed, 5 skipped, 0 failed, and 0 `SEVERE` lines. Its schema pass
re-applies every `IF NOT EXISTS` statement to a schema created by the older engine.

## Back to 26.10.2 after 26.11.1 has written: not safe

CodeRabbit's review of #235 claimed that 26.10.2 cannot read float arrays written by 26.11.1. Measured:
the same database, opened by 26.11.1, which added 100 entities, 300 `FACT` edges and 100
`ConversationTurn` vertices with new embeddings and rewrote the embedding of 50 existing facts. The
server was stopped, and the directory opened by 26.10.2.

| Probe | 26.11.1 | 26.10.2 on the same files |
|---|---|---|
| Record counts, full-text counts, `DATETIME`, index validity | 300 / 800 / 400, 165, 256 | same |
| `vector.neighbors` top 10 on `FACT[embedding]` | includes `n238` and `n11`, written by 26.11.1 | both missing, replaced by older facts |
| `vector.neighbors` top 10 on `ConversationTurn[embedding]` | 10 rows | last 2 differ |
| Server log | 0 errors | 448 `SerializationException: Error on deserializing value of unknown type 36` |

- **The upgrade is one-way once 26.11.1 writes.** 26.11.1 stores a float array under a type that
  26.10.2 does not know. On 26.10.2 those records still count, but their vectors cannot be read, so
  vector recall silently loses every fact, turn or trace written or re-embedded since the upgrade,
  and the log fills with exceptions.
- **The way back is the last backup 26.10.2 took.** `docker/arcadedb/backup.json` backs up every
  database every 60 minutes, but every backup after the upgrade is written by 26.11.1. Going back
  means restoring the last pre-upgrade archive into 26.10.2, and losing what was written since.
- **What this does not show:** whether 26.10.2 can open a 26.11.1 backup (assumed not, not tried),
  and which other types 26.11.1 writes differently.

## Release notes read against Aura

- **401 instead of 403** for a bad password (#10180). `CredentialAccepted` already treats both as a
  refusal; the 2026-10-31 harness measured it.
- **`DATETIME` offsets are converted to UTC.** Aura writes every `DATETIME` already in UTC
  (`.UTC().Format(time.RFC3339…)` in `internal/arcadedb`), and the live tier's bitemporal tests pass.
- **Statement savepoints, Unicode `length()`, `PRODUCT` and `BINARY` rerank.** No Aura statement
  depends on them. Aura's vector indexes use quantization `NONE`.
- **#11040,** the concurrent `CREATE PROPERTY` bug that `tenantOpenSlots` works around, was closed
  upstream on 2026-11-02. Whether 26.11.1 carries the fix, and whether `tenantOpenSlots` can go, was
  not measured. It is a separate change.

## What this does not show

- **Not the Docker image's runtime.** The tiers ran on the release tarballs. The image was only read
  (user, layers, Trivy). CI's jobs run the image from `compose.yaml`.
- **No embedder.** The embedder-dependent tests ran on neither version here. CI runs them.
- **Not the lab VM,** and not the operator's data. The upgraded database held synthetic records in
  Aura's schema, not a real memory or the document schema of `services/ingest`.
- **No load,** and no cold graph build on a large memory against the 500 ms recall budget.
- **No real model.** No agent turn was scored, so this is not the E2E of the Definition of Done.
- **The native image** was not tried.
