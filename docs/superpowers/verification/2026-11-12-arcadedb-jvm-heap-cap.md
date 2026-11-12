# ArcadeDB 26.11.1 on the JVM: capping the heap

Date: 2026-11-12. The operator asked whether to move to the native image ("Passare alla native
sarebbe una buona idea?"). The recommendation was to cap the JVM heap first, and the operator
approved it ("Sì, prepara il punto 1").

**Verdict: compose's default becomes `-Xms256M -Xmx768M`.**
- **Nothing breaks.** Aura's whole `arcadedb_integration` tier passes on all four settings
  measured. A 20,000-fact write-and-query bench ends with no `OutOfMemoryError`, no restart and
  no OOM kill.
- **Latency does not move.** Dense, full-text and the shipped fused statement are equal to the
  2 GB heap within the spread of the runs.
- **Memory falls.**
  - Idle drops from 2.12 GiB to 359 MiB.
  - The peak under this workload drops from 2.23 GiB to 970 MiB.

## Why the old default holds 2 GiB

- **Compose pinned `-Xms2G -Xmx2G`** through `ARCADEDB_OPTS_MEMORY`.
- **Since 26.11.1 the image runs ZGC.** `bin/server.sh` adds `-XX:+UseZGC` and
  `-XX:MaxRAMPercentage=75`.
- **With `Xms` equal to `Xmx`, ZGC commits the whole heap from the start.** The server therefore
  idles at 2.12 GiB.
  - On 2026-11-12 the native-release file read a JVM heap from `/proc/<pid>/smaps`: it is the
    `/memfd:java_heap` mapping, all dirty. That JVM ran on the image default, not on compose's
    pin, but ZGC maps its heap the same way under either setting.
  - None of that memory is cache the kernel can reclaim.
- **`server.sh`'s own comment** advises a small initial heap for low-footprint setups
  ("prefer a small initial heap such as `-Xms64M -Xmx256M`"). It warns that pinning `Xms=Xmx`
  tells the JVM "you have 512MB, fill it".

## Setup

- **Host.** A cloud container, not the lab VM. Docker 29.9.2.
- **Image.** `arcadedata/arcadedb:26.11.1@sha256:22a049898f34f01c256b56a1ba95fb01d32e5e14755d3c4367415c8e55ae5035`,
  Aura's pin.
- **Container.** Compose's settings: `--memory 3g`, its `JAVA_OPTS` (root password,
  `aura_memory`, local restore, Bolt), and `docker/arcadedb/backup.json`. Only
  `ARCADEDB_OPTS_MEMORY` varied.
- **Order of each run, on a new volume:**
  1. start, then idle measured 20 s after `/api/v1/ready`;
  2. the tier;
  3. the bench on the same server.
- **The tier.** `go test -tags arcadedb_integration -p 1 -count=1` on `./internal/arcadedb/...`,
  `./internal/docfields/...` and `./cmd/arcadedb-mcp/...`. It ran with the EmbeddingGemma
  sidecar, a migrated disposable Postgres and the tenant secret.
- **The bench.** 20,000 `FACT` edges between 300 entities, written one statement at a time.
  - Each edge has a 12-word statement, a random unit 768-float embedding, `valid_from` and
    `embed_space`.
  - The indexes are Aura's `LSM_VECTOR` (COSINE, no quantization) and `FULL_TEXT` with
    `EnglishAnalyzer`.
  - Then three statement shapes: `vector.neighbors`; the `SEARCH_INDEX` leg; and
    `memory_vector.go`'s shipped fused statement (RID filter, `vector.fuse` RRF, `vector.rerank`,
    relevance floor).
  - 40 probes per shape with `candidates` 40, 5 of them warm-up. The medians are over the other 35.
  - The data comes from seed 42, so it is identical for every setting.
- **Memory.** `docker stats` sampled every 2 s.
- **Scripts.** They lived in this session's scratchpad.

## Results

| `ARCADEDB_OPTS_MEMORY` | Idle | Peak | Tier | 20k writes | Dense | Full-text | Fused | OOM / restarts |
|---|---|---|---|---|---|---|---|---|
| A `-Xms2G -Xmx2G` (the old default) | 2.12 GiB | 2.23 GiB | 1709/1709 | 66.0 s | 16.7 ms | 26.3 ms | 986 ms | 0 / 0 |
| B `-Xms256M -Xmx1G` | 361 MiB | 1.21 GiB | 1709/1709 | 69.0 s | 16.4 ms | 29.7 ms | 1066 ms | 0 / 0 |
| **C `-Xms256M -Xmx768M` (new default)** | **359 MiB** | **970 MiB** | **1709/1709** | **64.1 s** | **16.2 ms** | **29.5 ms** | **957 ms** | **0 / 0** |
| D `-Xms256M -Xmx512M` | 358 MiB | 724 MiB | 1709/1709 | 68.1 s | 18.3 ms | 25.0 ms | 1070 ms | 0 / 0 |

- **Every setting logged the same 67 SEVERE command errors.** The native-release file shows that
  both images log the same lines on the same tier, so they come from the tier's statements.
- **Every arm returned 1400 rows** over the 35 timed probes, on every setting.
- **The fused statement costs about 1 s at 20,000 facts on every setting.** Its RID-filter
  subquery reads every fact on every call. The `where` option in
  [the native spec](../specs/2026-10-17-arcadedb-26-11-native-design.md) removes that subquery,
  and the heap does not change it.

**Why C and not D.**
- D also passed, with the lowest peak.
- But C keeps 50% more headroom than the only real-library run of a 512 MiB JVM. That run, on
  2026-10-17, read the 7,798-passage library at a 685 MiB peak, reads only.
- Real ingest of large documents and many identities has not been measured at any setting.

## What this does not prove

- **Not the lab VM.** It does not prove the real per-identity databases, the 7,798-passage
  library or the gold sets.
- **Not a heavier working set.** A memory larger than these 20,000 facts, or more open tenant
  databases at once, could need a larger heap. The sign would be an `OutOfMemoryError` in the
  server log, and `ARCADEDB_OPTS_MEMORY` in `.env` overrides the default.
- **Untested paths:**
  - Bolt and CocoIndex's ingest;
  - the backup plugin writing an archive under the smaller heap;
  - concurrent load.
- **`mem_limit` stays 3 GB.** This file does not measure the container's off-heap needs, so it
  does not lower the limit.
