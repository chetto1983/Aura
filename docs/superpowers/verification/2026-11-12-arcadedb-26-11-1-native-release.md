# ArcadeDB 26.11.1: the native release against the JVM release

Date: 2026-11-12. The operator asked for the native measurements on the release ("Fai le misure
con la native adesso", then "adesso è uscita la versione finale della 26.11.1"). This file
re-runs the release-day steps of
[the native spec](../specs/2026-10-17-arcadedb-26-11-native-design.md) that a cloud container can
run. Every earlier native figure came from the `26.11.1-SNAPSHOT` at `15958b48f`.

**The two images, both linux/amd64, both pushed on 2026-11-04:**
- **JVM:** `arcadedata/arcadedb:26.11.1@sha256:22a049898f34f01c256b56a1ba95fb01d32e5e14755d3c4367415c8e55ae5035` (index digest), Aura's pin.
- **Native:** `arcadedata/arcadedb:26.11.1-native@sha256:2a8460b1a02a802f4599147364d94b0b8ef8846c8e1471c0a342eb7e31af836d`.
- They report different builds of 26.11.1:
  - JVM: `a3a5d124572608412aa187f9c4b290e071699473`, branch `main`;
  - native: `0f3bb2238c3cfc79c4a1c2cf2d8c5e36489652c0`, branch `UNKNOWN`.
  - This file did not check which commits separate them.

**Verdict:**
- **Functionally the native release is a drop-in for Aura.** Aura's whole `arcadedb_integration`
  tier passes on it, 1709 of 1709, with the same server log as the JVM.
- **The two defects reported on 2026-10-17 are fixed.**
  - A new container on existing databases logs no SEVERE line.
  - The image runs as UID 1000, so a volume moves between JVM and native in both directions.
- **It still has no shell.** Compose's `wget` healthcheck cannot run in it.
- **Memory is the native's real advantage:**
  - under the same load, about 125 MiB against 2.3 GiB for the JVM on the image's own heap default;
  - against 690 MiB for a JVM capped at a 512 MiB heap.
- **Latency is mixed, and close.**
  - Dense search is about 45% slower on the native.
  - Full-text is about 25% faster on the native.
  - The shipped fused statement is the same within noise.
- **The image's own heap default is large.**
  - With `ARCADEDB_OPTS_MEMORY` empty, the image runs ZGC with `-XX:MaxRAMPercentage=75`, so
    under a 3 GB limit the heap can reach 2.25 GiB.
  - ZGC keeps it: the 2.2 GiB is the `/memfd:java_heap` mapping, all dirty pages. It is not cache
    the kernel can reclaim.
- **Correction, the same day: the JVM columns are not compose's configuration.**
  - Compose sets `ARCADEDB_OPTS_MEMORY` to `-Xms2G -Xmx2G` (`compose.yaml`, since 26.9.1). The
    scripts here did not pass it, so the JVM ran on the image default described above.
  - The heap of compose's own configuration is measured in
    [`2026-11-12-arcadedb-jvm-heap-cap.md`](2026-11-12-arcadedb-jvm-heap-cap.md): it idles at
    2.12 GiB and peaks at 2.23 GiB under the same tier and a 20,000-fact bench.

## Setup

- **Host.** A cloud container, not the lab VM. Docker 29.9.2, with no other load during the runs.
- **Servers.** Each ran in a new container with compose's settings, except the heap (see the
  correction above):
  - `--memory 3g`;
  - root password, `aura_memory` as default database, `restoreImportAllowLocalUrls`, and the Bolt
    plugin;
  - `docker/arcadedb/backup.json` mounted.
  - The JVM read them from `JAVA_OPTS`. The native took them as trailing `-D` arguments, its only
    way.
- **Aura.** `master` at `181be7694`.
- **Scripts.** They lived in this session's scratchpad, not in the repo.

## Image, start and idle memory

| | JVM 26.11.1 | native 26.11.1 |
|---|---|---|
| Image on disk (`docker image inspect`) | 864 MB | 1327 MB |
| Start to `/api/v1/ready` 204, three cold starts | 2.18 / 2.09 / 1.90 s | 0.67 / 0.50 / 0.53 s |
| `docker stats` 30 s after ready | 256 MiB | 51 MiB |
| Shell or `wget` in the image | yes | no (31 files, `FROM scratch`) |
| Image user | `arcadedb` | `1000:1000` |

- **The native image is now the bigger one.** The JVM image dropped from about 1100 MB at 26.10.1
  to 864 MB.
- **The JVM idle figure reflects the new ergonomic heap.**
  - It starts with 48 MiB (`InitialHeapSize`).
  - It is no longer the 2.09 GiB of the fixed 2 GB heap measured on 2026-10-14.

## Aura's live tier

`go test -tags arcadedb_integration -p 1 -count=1` on `./internal/arcadedb/...`,
`./internal/docfields/...` and `./cmd/arcadedb-mcp/...`. The run had the EmbeddingGemma sidecar
on `:8081`, a migrated disposable Postgres, and the tenant secret. Each image started on a new
volume.

| | JVM | native |
|---|---|---|
| Tests passed / failed / skipped | 1709 / 0 / 0 | 1709 / 0 / 0 |
| SEVERE lines in the server log | 67 | 67 |
| of which `PostQueryHandler` / `PostCommandHandler` | 57 / 10 | 57 / 10 |
| Peak `docker stats` during the tier | 2.35 GiB | 149 MiB |

The SEVERE lines are command errors. Both images log the same ones on the same tier, so they come
from statements the tests send, not from the engine.

## The two defects of 2026-10-17

- **Defect 1, the users file. Fixed.**
  - A new native container on the tier's volume, which holds `aura_memory`, logged 0 SEVERE lines.
  - All 68 indexes of `aura_memory` loaded, the same count as before the restart.
- **Defect 2, root-owned files. Fixed.** The databases the native wrote are owned by `1000:1000`.

**The volume moves in both directions:**

| Open | Indexes | A DDL write | SEVERE | `AccessDenied` |
|---|---|---|---|---|
| native on a volume the JVM wrote | 68 | 200 | 0 | 0 |
| JVM on a volume the native wrote | 68 | 200 | 0 | 0 |

## Latency and memory under load (synthetic)

**The workload.**
- One database holding 300 `Entity` vertices and 3000 `FACT` edges.
- Each edge has a 12-word statement, a random unit 768-float embedding, `valid_from` and
  `embed_space`.
- The indexes are Aura's: `LSM_VECTOR` with COSINE and no quantization, and `FULL_TEXT` with
  `EnglishAnalyzer`.
- The data is generated with seed 42, so it is identical for every server.

**The queries.**
- Aura's three statement shapes:
  - `vector.neighbors` alone;
  - the `SEARCH_INDEX` lexical leg;
  - the shipped fused statement of `memory_vector.go` (RID filter, `vector.fuse` RRF,
    `vector.rerank`, relevance floor).
- 40 probes each, with `candidates` 40. The first 5 probes are warm-up; the median is over the
  other 35.
- Times are client wall time over HTTP on localhost.
- Each round started on a new volume.

| Median ms (round 1 / round 2) | JVM, image heap default | JVM, `-Xms512M -Xmx512M` | native |
|---|---|---|---|
| 3000 edge writes, total s | 13.8 / 13.6 | 11.9 / 12.9 | 12.9 / 12.9 |
| `vector.neighbors` | 14.7 / 14.0 | 16.6 / 13.3 | 21.0 / 20.5 |
| `SEARCH_INDEX` | 9.5 / 11.2 | 9.8 / 7.8 | 7.4 / 7.3 |
| fused (shipped) | 180.1 / 151.5 | 149.7 / 157.6 | 175.1 / 162.3 |
| Peak `docker stats` | 2.33 / 2.34 GiB | 691 / 693 MiB | 113 / 125 MiB |

**Where the memory is, read from the container's cgroup after one bench run:**
- **JVM:** RSS 151 MiB and `mapped_file` 2155 MiB.
  - `/proc/<pid>/smaps` puts 2198 MiB, all dirty, in `/memfd:java_heap`: the ZGC heap.
- **Native:** RSS 118 MiB and 42 MiB of inactive file cache.

**What the row counts show.** Every arm returned the same row counts on every image, for example
1400 rows over the 35 timed probes. Only the counts were compared, not the ranked answers.

## What this does not prove

- **Not the lab VM,** and not the real library. The 7,798-passage figures of 2026-10-17 and the
  108 gold questions were not re-run. Recall on real data is still the snapshot's.
- **Synthetic latency only.**
  - Random vectors and a 30-word vocabulary are not Aura's data.
  - The fused statement's cost here is mostly its filter subquery over all 3000 edges.
  - Read the columns against each other, not as Aura's production latency.
- **No `where` arm.** The `where` option of `vector.neighbors`, which the spec adopts, was not
  timed on the release.
- **Not exercised:**
  - Bolt and CocoIndex's writes;
  - an actual backup archive written by the native;
  - arm64;
  - concurrent load;
  - behaviour when the 3 GB limit is reached.
- **The healthcheck.** It is still to be designed, since the native image has no shell.
