# Compose pin refresh: Postgres, SearXNG, whisper, Prometheus, Tempo, Grafana

Date: 2026-10-24. The operator asked for the sidecars' updates, then for the `compose.yaml`
pins to move ("Ora aggiorna i pin di compose.yaml").

**Verdict: every new pin behaves as the old one did on the checks below, within the limits
under "What this does not establish".**

| Service | Old pin | New pin |
|---|---|---|
| postgres | `postgres:18.4-alpine3.24` | `postgres:18.6-alpine3.24` |
| searxng | `ghcr.io/searxng/searxng:2026.10.7-6671d89be` | `2026.10.23-629368e40` (`latest`, `sha256:aecad882…`) |
| aura-stt | `aura-whisper-server:2026.10.08.1056@sha256:c6c6311a…` | `2026.10.21.1401@sha256:6781a5d7…` |
| prometheus | `prom/prometheus:v3.13.4@sha256:87861b8c…` | `v3.13.5@sha256:aafa9d69…` |
| tempo | `grafana/tempo:3.1.0@sha256:3076b8dc…` | `3.1.1@sha256:64e2fbd5…` |
| grafana | `grafana/grafana:13.2.3@sha256:b28bae15…` | `13.2.4@sha256:dbbc38ea…` |

Everything else was already on its newest release: ArcadeDB 26.10.2 (26.11.1 is a snapshot),
llama.cpp `v0.5.0`, Garage v2.4.1, docker-socket-proxy v0.5.0, Kokoro v0.9.0. Prometheus stays on
the 3.13 line, at its patch release; 3.15.1 was not measured.

**Where the digests come from.** Docker Hub's tag API and ghcr's registry `HEAD` return the
index digest. Asked for the old tags, both return exactly the digests `compose.yaml` pinned.

## Setup

- A cloud container with Docker 29.9.2. Docker Hub answered 429 to anonymous pulls, so the daemon
  pulled through `mirror.gcr.io`. Digest pins were checked on pull.
- Each service ran from the real `compose.yaml` in a project of its own (`-p aura-pinlab`) with
  `--no-deps`. It ran first on the old pin, and after the edit on the new one, over the same
  named volumes. The env file held placeholders for every `${VAR:?}` the file declares.
- The aura daemon, ArcadeDB, Garage and the models were not part of it.
- Overrides that only this container needed, outside the repository:
  - SearXNG: `GRANIAN_HOST=0.0.0.0`, because the kernel has no IPv6 and granian's `::` bind
    failed with `os error 97` on both builds.
  - SearXNG and whisper: the egress proxy's CA, mounted so outbound TLS validates.

## Results

### Postgres 18.4 → 18.6

- **The baseline.** 18.4 started on a fresh volume and `go run ./cmd/aura db migrate` applied 117
  migrations, to 0142. The extensions landed in `public`, and a probe table got 10,000 rows.
- **The upgrade.** 18.6 opened that data directory:
  - `server_version` reads 18.6;
  - the probe rows return the same count and the same md5;
  - `schema_migrations` is at 142 and not dirty, and `db migrate` reports nothing pending;
  - `aura.searchable_text('Città Perché')` still unaccents.
- **The schema.** It was compared through one `pg_dump` 18.6, with the `\restrict` key lines
  removed because the key is random in every dump. The upgraded database's schema equals a fresh
  18.4 database's after the same two `db migrate` runs. After one run it differs by one line,
  see the findings below.

### Prometheus 3.13.4 → 3.13.5

- **The rules.** 5 groups and 47 rules load before and after, all `health: ok`.
- **The samples.** `count_over_time(prometheus_build_info[30m])` returned both series: 51 samples
  from 3.13.4 and the first 6 from 3.13.5.
- **The static gate.** `verify-observability.ps1 -StaticOnly` runs `promtool check config` and
  `promtool test rules` in the new image, and passes.

### Tempo 3.1.0 → 3.1.1

- **The baseline.** 3.1.0 took an OTLP span (HTTP 200) and flushed a vParquet5 block to the
  volume.
- **The upgrade.** 3.1.1 found that trace by ID (HTTP 200) and by TraceQL (1 hit), and took new
  spans. It reports `block_retention: 336h0m0s`. Its `-config.verify` healthcheck passes on the
  unchanged `tempo.yml`.
- **An empty meta still fails the poll.** With one empty `meta.json` added, the next poll failed
  with `failed reading unknown blocks: unexpected end of JSON input`.
  `tempodb_blocklist_tenant_index_errors_total` rose by 2, and no
  `tempodb_blocklist_corrupt_block_meta_total` was exported. grafana/tempo#8058 is not in 3.1.1:
  `git log v3.1.0..v3.1.1` holds six commits, all security and dependency fixes.
  `observability/runbooks/aura-tempo-blocks.md` says so.
- **Its error lines.** They are `error calling scheduler … no jobs found`. A throwaway 3.1.0 on
  the same config logs them as well.

### Grafana 13.2.3 → 13.2.4

- **The upgrade.** 13.2.4 opened the volume 13.2.3 initialised, with the same four dashboards:
  `aura-agents`, `aura-data-retention`, `aura-overview` and `aura-tools-mcp`.
- **The datasources.** `aura-prometheus` and `aura-tempo` both answer `OK` on their health
  endpoints.
- **Memory.** 234 MiB of its 384 MiB limit.

### SearXNG 2026.10.7 → 2026.10.23

- **Health.** `/healthz` returns 200 on both builds.
- **Searches.** `postgresql` and `arcadedb` each returned 27 results from duckduckgo and google
  cse on both builds. brave answered "too many requests" on both.

### Whisper mirror 2026.10.08.1056 → 2026.10.21.1401

- **The test.** `large-v3-turbo` int8 on CPU, as compose runs it, read the model from the same
  volume. The clip was a 5.2 s Italian sentence made with espeak-ng, posted to
  `/v1/audio/transcriptions` with `language=it` the way `internal/multimodal/stt.go` posts it.
- **The result.** Both builds returned "Domani alle 9 ho una riunione con il fornitore per
  discutere il preventivo." The old build took 9.0 s and 8.1 s, the new one 8.3 s and 7.9 s.

## Findings on the way

- **`verify-observability.ps1 -StaticOnly` failed every local run.** Its list of placeholders for
  `${VAR:?}` variables held 8 names while `compose.yaml` declares 13. CI passed because `ci.yml`
  sets all 13 at workflow level. It now reads the names from `compose.yaml`. The script and its
  fixture suite pass, here and against the new pins.
- **`db migrate` changes the schema on its second run.** A fresh database's first run leaves out
  `ALTER DEFAULT PRIVILEGES FOR ROLE aura_app IN SCHEMA authula GRANT SELECT ON TABLES TO
  aura_migrate`, and the second run adds it. This holds on 18.4 and on 18.6 alike, so it is not
  part of this change. It is not fixed here.

## What this does not establish

- **The application.** The aura daemon, the agent, the ingest pipeline and the cockpit did not run
  against the new images.
- **The lab VM and the updater.** Nothing ran on the lab VM, and no appliance applied the pins
  through its updater.
- **Load.** Postgres was not measured under load, nor Tempo, Prometheus or Grafana over hours.
- **The two builds that change most.** For SearXNG and whisper, only the checks above were
  compared: two queries and one clip.
