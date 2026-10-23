# ArcadeDB 26.11.1: native image and the `where` filter

Proposed on 2026-10-17. The operator asked for it ("Sì, scrivi la spec") after choosing the
native image for its memory ("Io passerei a native: è più lenta ma di poco, ma consuma molta meno
RAM").

**Status (2026-10-17): parked until ArcadeDB 26.11.1 ships.** The operator: "ora aspettiamo che
esca la nuova release di ArcadeDB nelle prossime 2 settimane". It is not yet approved.
- **One question is open, the operator's to answer.** How the Aura-built ArcadeDB image (phase
  B) reaches an appliance.
  - **Proposed:** published like the other repo-built images (`AURA_ARCADEDB_IMAGE`,
    `AURA_ARCADEDB_PULL_POLICY`), on a fixed tag tied to the ArcadeDB version, never on `edge`.
    The database then changes only when the pin does, as a third-party image does today.
  - **Alternative:** the moving `edge` tag, refreshed by the updater like the sidecars.
  - The operator did not choose ("Non so") and asked to wait for the release.
- **On the release, before the plan:**
  - re-run `docs/superpowers/verification/2026-10-17-arcadedb-native-image-defects.md` and the
    latency and memory files on `26.11.1` and `26.11.1-native`;
  - answer the question above;
  - then approve the spec.
- **2026-10-23: the JVM pin moved to 26.10.2**
  ([verification](../verification/2026-10-23-cocoindex-neo4j-connector-and-arcadedb-26-10-2.md)),
  with the floor still at 26.10.1. Where this spec starts from 26.10.1 (phase A's revert,
  acceptance 8), it now starts from 26.10.2.

**This spec waits for ArcadeDB 26.11.1.**
- Roberto Franchini said on 2026-10-17 that it ships "un paio di settimane" later.
- Nothing here lands on a snapshot. Every number below is from `26.11.1-SNAPSHOT` at `15958b48f`,
  and acceptance re-measures it on the release.

**It supersedes arms P and R** of `2026-10-17-fused-query-filter-after-rerank-design.md`. That
spec moved the space filter after the rerank, and accepted fewer results while two embedding
spaces coexist. The `where` option (ArcadeDB #10169) costs the same, and removes that
compromise.

Risk tier: **medium.** It swaps the runtime of the store that holds every identity's long-term
memory. One SQL statement changes. There is no schema change and no migration.

## The problem, measured

**1. The document query's cost is the space filter subquery.**
- `vector.neighbors` receives `{ filter: (SELECT @rid FROM Passage WHERE … embed_space = :space).@rid }`,
  which loads every passage of the space on every call.
- On 7,798 real passages, on the JVM at the snapshot's commit:
  - the shipped statement takes 43.7 to 51.3 ms;
  - `{ where: { embed_space: :space } }` takes 14.5 ms, with 16 of 16 answers identical;
  - the post-filter arm takes 14.3 ms (`2026-10-17-fused-query-arcadedb-latest-native.md`).
- On the 108 gold questions (office, complex, code in English and Italian, find-by-name) `where`
  gives identical answers and identical r@1, r@3, r@10 and MRR, on the native snapshot.
- **The predicate is applied:** `k` 10 returns 10 rows for the real space and 0 for a space that
  does not exist.
- **26.10.1 refuses the option** with `Unknown option 'where' for function 'vector.neighbors'`.

**2. The JVM holds memory the native image does not.**
- On the same library under the variant bench (reads only, `2026-10-17-arcadedb-native-memory.md`):

  | Runtime | Idle | Peak |
  |---|---|---|
  | compose's JVM, `-Xms2G -Xmx2G` | 2.10 GiB | 2.18 GiB |
  | native snapshot, default heap | 30 MiB | 610 MiB |

- On the lab VM on 2026-10-14 the native snapshot idled at 17 MiB and was ready in about 1 s,
  against 2.09 GiB and 5 to 28 s for the JVM (`2026-10-14-arcadedb-native-image-and-go-driver.md`).
- The price is speed: the native image is 50 to 65% slower on the statements that matter. That
  is about 10 ms on the `where` statement (25 against 14.5 ms).

**3. A native image that lacks a class destroys a schema, with one log line.**
- 26.10.1-native has no Lucene `EnglishAnalyzer`, which every memory full-text index uses
  (`memory.go`, `memory_conversation.go`, `memory_reasoning.go`).
- It logs one SEVERE line, keeps 49 of an identity database's 86 indexes, and rewrites
  `schema.json` on disk without the rest (18,047 bytes to 8,739).
- 26.10.2-native and the snapshot load all 86.
- ArcadeDB's own documentation calls the native image "an experimental add-on". It also says a
  plugin missing from the image "is skipped without a log line".
- So this spec treats every native gap as one that would be silent, and makes it fail loudly.

**4. The native image cannot run compose's service definition as it is.**
- It holds only the binary and `config/`: no shell, no `wget`. Compose's healthcheck cannot run,
  and `aura`, `arcadedb-mcp` and `aura-ingest` wait on `service_healthy` for ArcadeDB.
- It reads neither `JAVA_OPTS` nor `ARCADEDB_OPTS_MEMORY`. Every `-D` setting is a trailing
  argument to the entrypoint.
- **The amd64 image is `FROM scratch` and runs as UID 0.** The JVM image runs as `arcadedb`
  (1000:1000).
  - A native server leaves root-owned files with mode 600.
  - A JVM started afterwards on the same volume cannot write `schema.json`.

## Goal

ArcadeDB runs as the 26.11.1 native image, with the same data, users, plugins, backups and
restore as today.
- An idle appliance holds tens of MiB for it, not 2 GiB.
- The document query filters by embedding space inside the ANN search: as fast as the
  post-filter arm, and with the full `k` from the caller's own space while two spaces coexist.
- A native build with a gap of the kind measured in problem 3 never reaches an identity's volume: it is caught on a copy before the pin lands.
- Going back to the JVM is a revert, not a recovery.

## Non-goals

- **The other six `vector.neighbors` filters:** memory recall (`memory_recall.go`, two),
  `memory_vector.go`, `turn_recall.go`, `memory_reasoning_statements.go`, and
  `document_cards.go`.
  - They filter by identity, as-of time and space, not by space alone, and none was measured with
    `where`.
  - `where` degrades to testing every indexed vector when the predicate keeps few records
    (`RecordPredicateRidSet.java` at `15958b48f`), so a selective as-of filter could get slower.
  - Each moves only after its own measurement.
- **Scoped document searches.** When the caller names documents or sources, the statement stays
  as shipped. That case was never measured with `where`. Acceptance 4 records it, and this spec
  does not change it.
- **The full-text `LIMIT` and fuse's `limit`.** The `LIMIT` changes 2 of 16 ungolded answers, and
  fuse's `limit` buys nothing on top of `where` (15.1 against 14.5 ms).
- **The Go driver.** The decision of 2026-10-17 stands: keep Aura's transport.
- **Lowering `mem_limit`.** The only memory measured is under reads. It stays at 3 GB until
  acceptance 7 measures an ingest.

## Anti-goals

The slice has failed, even if the stack comes up, if any of these happens:
- the server loads fewer indexes or types than the JVM on the same volume;
- its log carries a `SEVERE` that the JVM of the same release does not log on the same start, a
  `ClassNotFoundException` or a `MissingReflectionRegistrationError`;
- a document search returns fewer results than before in a library holding one space;
- a file under the ArcadeDB volumes ends up owned by root.

## Decisions

### 1. Two phases, each one release and one revert

**Phase A: the JVM at 26.11.1.**
- **The pin.** `compose.yaml` pins `arcadedata/arcadedb:26.11.1` by its multi-arch index digest,
  as today.
- **The floor.** `minSecureVersion` (`internal/arcadedb/admin.go`) becomes 26.11.1. The refusal
  names #10169, since the `where` option does not exist before it. It also makes an older server
  fail at `TenantClients.For`, before the first tenant is served, instead of on the first
  document search.
- **The query.** It switches to `where` (decision 4).

**Phase B: the native image of the same release,** with decisions 2 and 3.

**Why two.**
- A revert of B lands on A, a JVM that the floor accepts. A single slice would revert to
  26.10.1, which the new floor refuses.
- The floor reaches the code only after the pin reaches compose. `aura-image-update.sh` brings
  `aura` up before it re-applies the stack (lines 498 to 515), so the release that raises the
  floor must find every host already able to run 26.11.1.
- The order inside phase A is the builder's, under one constraint: no commit has a floor above
  the image compose pins.

### 2. Phase B: Aura builds its own ArcadeDB image

`docker/arcadedb/Dockerfile` does what the spike of 2026-10-17 proved. It is in "Measured while
writing" below.
- **Base.** A first stage is the pinned native image (`FROM arcadedata/arcadedb:26.11.1-native@sha256:…`).
  The digest lives here and only here: compose names the built image.
  `cmd/aura/container_artifacts_test.go` and `.github/dependabot.yml` follow it to
  `/docker/arcadedb`.
- **Re-owned from scratch.** The final stage is `FROM scratch`, with:
  - `/etc/ssl`, as it is;
  - `/tmp` and `/home/arcadedb`, `COPY --chown=1000:1000`;
  - the upstream `WORKDIR`, `VOLUME`s and `ENTRYPOINT`, re-declared;
  - `USER 1000:1000`, the JVM image's `arcadedb` user.

  Setting `USER` on the upstream image is not enough: its five `VOLUME` directories are
  root-owned, so the anonymous `log/` and `config/` volumes come up unwritable and the server
  exits (measured).
- **No upstream `server-users.jsonl`.** The native image ships a pretty-printed, multi-line one,
  and the JSONL loader rejects it. That gives one SEVERE whenever a new container meets an existing
  database, misnamed as `server-groups.json`, then a fallback to the defaults. The JVM image ships none. The Dockerfile leaves it out.
- **The probe.** A static Go binary built in a first stage, whose `healthcheck <url>` exits 0 on
  any 2xx.
  - `/api/v1/ready` answers 204, and `cloudflaresupervisor.Healthcheck` accepts only 200
    (`health.go:47`).
  - It moves to a shared package that accepts 2xx. Cloudflared's `/healthz` answers 200, so its
    behaviour is unchanged.
  - The image declares `HEALTHCHECK`, and compose keeps `service_healthy` for its three
    dependents.
- **The server root stays `/home/arcadedb`,** as in the JVM image. Compose's mounts, the contract
  test's mount line and `scripts/restore_drill.sh`'s `file:///home/arcadedb/…` stay valid.
- **Distribution.** This is the operator's question, below.

### 3. Phase B: settings move to the command line

- **Settings.** Compose's `JAVA_OPTS` becomes the service's `command:`, the same four `-D`
  settings in the same order. Each comment moves with its setting:
  - `rootPassword`;
  - `defaultDatabases=…[]`, with its brackets;
  - `restoreImportAllowLocalUrls`;
  - `plugins=Bolt:…`.
- **No `ARCADEDB_OPTS_MEMORY`.**
  - The native heap sizes itself from `mem_limit`: the snapshot reported `MAXRAM=2.40GB` under a
    3 GB limit.
  - The key joins `RETIRED_ENV_KEYS` (`deploy/aura-image-update.sh:48`), so a host's `.env` does
    not keep a dead value.
- **The root password.** `scripts/ingest_reconcile_e2e.sh:72` and `scripts/ingest_media_e2e.sh:35`
  read it from the container's `JAVA_OPTS`. They read `ARCADEDB_PASSWORD` instead, the variable
  compose itself takes it from.
- **Unknown settings.** The binary reads every trailing `-D` as a system property, so a typo is
  not rejected. Acceptance 6 checks that each setting took effect, not only that the server
  started.

### 4. Phase A: the document query filters the space with `where`

For an unscoped search, `fusedStatement` gives `vector.neighbors`
`{ where: { embed_space: :space }, maxDistance: :max_distance }`, and no `filter`.
- **What stays.** The full-text leg keeps `embed_space = :space` in its own WHERE. So do the
  rerank, `:candidates`, `fusedDenseNeighbours`, the floors and the group size.
- **The bench.** In `document_query_variants_bench_test.go`, "shipped" becomes this statement, and
  the old one stays as the variant "dense filter subquery". The `where` variants of 2026-10-17 were
  never committed, so they are written there. Their statements are in
  `2026-10-17-fused-query-arcadedb-latest-native.md`.
- **The isolation test changes, with its justification.** `document_retrieval_floor_test.go:56`
  requires `denseSpaceFilter` twice in the statement, and the `where` form carries it once, in
  the full-text leg. The test is rewritten to check that both legs are bound to `:space`: the
  `where` map and the full-text WHERE. Its commit message says why (CLAUDE.md, never modify a
  test to pass).
- **The doc comment** cites #10169, this spec, and the measured costs.

### 5. No runtime guard in Aura: the floor and the release gate do the work

A guard at tenant open that refuses a missing index was considered and dropped.
- **It would break schema evolution.** `EnsureMemorySchema` (`memory.go`) runs
  `CREATE INDEX IF NOT EXISTS` on every open. That is how a new index reaches an existing
  database, so "an index is missing" is the normal state after an upgrade, not a fault.
- **It would come too late.** 26.10.1-native reset the schema while the server loaded the
  database, before any client connected. No check in Aura could have prevented it.
- **The defence is therefore upstream of Aura:**
  - the floor (decision 1) refuses every release older than 26.11.1;
  - acceptance 5 compares a real volume under both runtimes, with the release image, before the
    pin lands;
  - a native gap that reaches a statement already fails loudly. On 26.10.1-native, the analyzer's
    `CREATE INDEX` failed with `IndexException` (2026-10-14 smoke), so `EnsureMemorySchema`
    returns an error.

### 6. Going back is a revert

- **Reverting phase B** restores the JVM 26.11.1 service of phase A, with its `wget` healthcheck.
  Both runtimes run as 1000:1000, so neither leaves a file the other cannot write.
- **Reverting phase A** restores 26.10.1 and the old floor. Opening data that 26.11.1 has written
  with 26.10.1 is unmeasured.
- Acceptance 8 runs both reverts.

## Measured while writing

**Native as 1000:1000.** On `latest-native` (26.11.1-SNAPSHOT `15958b48f`), 2026-10-17.
- **`--user 1000:1000` on the upstream image:** exit 1, `AccessDeniedException:
  ./log/arcadedb.log.0.lck`. The anonymous volumes are created root-owned from the image.
- **Re-owning in a `FROM upstream` stage** with `COPY --chown`: the server still exits, unable to
  write `config/server-users.jsonl`. The existing root-owned directory keeps its owner.
- **Re-owning in a `FROM scratch` stage**, as decision 2 does:
  - ready in 2 s;
  - it reads the 7,798-passage library, `create database` answers `ok`, and Bolt listens;
  - after a stop, 0 files under the databases mount belong to anyone but 1000.
- **The SEVERE at boot.** It appears on the upstream native image too, run as root, when a new
  container starts on a databases directory that already holds a database, with or without
  `defaultDatabases`. The 26.10.1 JVM logs nothing under the same conditions. The stack trace is
  `SecurityUserFileRepository.load`, the users loader, failing on line 1 (`{`) of the image's
  multi-line `server-users.jsonl`. The reproduction is
  `docs/superpowers/verification/2026-10-17-arcadedb-native-image-defects.md`.

## Shape

**Phase A**

| File | Change |
|---|---|
| `prd.md` §10, where the ArcadeDB floor is written up (around line 1229) | the amendment, before the code: the two phases, the floor, `where`, and what is not proved |
| `compose.yaml` | the JVM pin at 26.11.1 |
| `cmd/aura/container_artifacts_test.go` | its pinned image line |
| `internal/arcadedb/admin.go`, `version_test.go`, `client_admin_test.go`, `tenant_clients_test.go` | the 26.11.1 floor and its message |
| `internal/arcadedb/document_retrieval.go`, `document_retrieval_test.go`, `document_retrieval_floor_test.go` | decision 4 |
| `internal/arcadedb/document_query_variants_bench_test.go` | decision 4's bench |
| `docs/superpowers/specs/2026-10-17-fused-query-filter-after-rerank-design.md` | marked superseded, pointing here |

**Phase B**

| File | Change |
|---|---|
| `prd.md` §10 | the native image's part of the amendment |
| `docker/arcadedb/Dockerfile` (new) and `.dockerignore` | decision 2. `.dockerignore` lets the probe's source through, and `container_artifacts_test.go` checks it |
| the probe's shared package, `internal/cloudflaresupervisor/health.go` and `cmd/` | decision 2's probe, accepting 2xx |
| `compose.yaml` | the `arcadedb` service builds or names the image (per the operator's answer); `command:`; no `JAVA_OPTS` and no `ARCADEDB_OPTS_MEMORY`; the comments rewritten for native |
| `cmd/aura/container_artifacts_test.go`, `.github/dependabot.yml` | the pin now in `/docker/arcadedb` |
| `deploy/aura-image-update.sh` | `ARCADEDB_OPTS_MEMORY` retired, plus whatever the distribution answer needs |
| `scripts/ingest_reconcile_e2e.sh`, `scripts/ingest_media_e2e.sh` | the root password from `ARCADEDB_PASSWORD` |
| `.github/workflows/ci.yml` | the comments describing `JAVA_OPTS` (lines 457 and 816-820) |

Every file stays under 600 lines, and every file touched is refactored on touch.

### New env vars

- **Phase A:** none.
- **Phase B:** none, unless the distribution answer is a published image. That needs
  `AURA_ARCADEDB_IMAGE` and `AURA_ARCADEDB_PULL_POLICY`, like `arcadedb-mcp`'s pair, catalogued
  in the PRD. `ARCADEDB_OPTS_MEMORY` is removed and retired.

## Acceptance

Each run is on the 26.11.1 release, never the snapshot. Items 1 to 4 close phase A, and items 5
to 8 close phase B. Items 9 and 10 close each phase.
1. **Unit, Go.**
   - `go test -race ./internal/arcadedb/`: the statement clause by clause, both legs bound to
     `:space`, the floor at 26.11.1, and a 26.10.2 server refused with #10169 named.
   - The package keeps its coverage policy, and the repo's `make quality` is green.
2. **Answers.** `TestFusedQueryVariants` on the 7,798-passage library, office, complex, and code in
   English, Italian and find-by-name.
   - The new statement gives the same answers as the old one on every gold question, and on 16 of
     16 questions of the large library.
   - Any difference stops the slice until it is explained.
   - The medians go in the verification file.
3. **Two spaces.** An integration test builds a library with passages in two spaces.
   - A search in either space returns only its own passages.
   - It returns as many as its limit when the space has that many.
   - It also records the latency of a 50-passage space beside a 5,000-passage one. That is the
     case where `where` scans every vector, so the slice records it rather than gating on it.
4. **Scope.** An integration test runs a scoped search (the `DocumentIDs` of two documents) and an
   unscoped one over the same library.
   - The scoped one returns only those documents.
   - Its statement is the shipped one, unchanged.
5. **No silent gap.**
   - Start the native server on a copy of the lab VM's `aura-arcadedb` volume. Every database
     lists the same indexes and types as under the JVM 26.11.1.
   - The log fails none of the anti-goals.
   - Users are not on that volume: `config/` is an anonymous volume that Aura reprovisions at
     boot (`prd.md` around line 3990). So users are compared after `aura serve` has run once
     against each runtime, by `ListUsers` names and their database grants.
6. **Every setting took effect** on the compose stack.
   - Root logs in with `ARCADEDB_PASSWORD`, and `aura_memory` exists.
   - Bolt answers on 7687, and an ingest catch-up writes through it.
   - A file-URL restore runs in CI's DR drill.
   - The healthcheck turns healthy, and its three dependents start.
   - The `AutoBackupSchedulerPlugin` writes an archive under `backups/`.
7. **Memory under load,** on the lab VM: one full ingest catch-up of a real library, with
   `docker stats` sampled throughout.
   - Record idle, peak, and whether any `OutOfMemoryError` occurred.
   - The verification file says whether 3 GB can come down, and this spec does not lower it.
8. **Both reverts.**
   - Upgrade a copy of the lab VM's volumes: JVM 26.10.1, then phase A, then phase B.
   - Then revert B. Then revert A, recording what 26.10.1 does with data 26.11.1 wrote.
   - Every step starts clean and passes item 5's comparison.
   - No file under any volume is owned by root.
9. **E2E** on the lab VM, driven by the real agent, at a score above 9.8 (Definition of Done). It
   covers:
   - document search with citations;
   - memory write and recall through `cmd/arcadedb-mcp`;
   - a conversation turn recall;
   - a restart of the ArcadeDB container mid-session.

   Each run leaves its dated file in `docs/superpowers/verification/`.
10. **CI green** on the push: every `ci.yml` job that brings ArcadeDB up through compose, the DR
    drill and Agent Memory MRS included.

## Open questions

1. **The appliance's CPU and architecture.** The lab VM and this container are amd64. If the
   appliance is arm64, its image is the distroless glibc one. That base already has a non-root
   user (65532), so decision 2's `USER` applies to it unchanged, but nobody measured it.
2. **Whether 26.11.1's native image matches the snapshot.** Every native number here is from a
   build of main. Acceptance 2, 5 and 7 re-measure on the release.
3. **When the other six filters move.** Each needs the measurement this spec ran for the document
   query.

## What this spec does not prove

- **Memory or latency under writes,** until acceptance 7. Every memory number here is from reads.
- **Recall on a real long-term memory** under the native image: thousands of facts with as-of
  filters. The memory queries keep their `filter` subqueries, but their runtime changes.
- **That acceptance 5 catches every native gap.** It compares schemas and runs the E2E paths. A
  missing function or reflection entry on a path nothing exercised fails the statement that needs
  it: loudly, but at query time.
- **The appliance,** whose CPU and RAM were not measured.
