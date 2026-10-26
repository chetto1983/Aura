# Weekly security scan: the first runs on GitHub

Date: 2026-10-26. The workflow is `.github/workflows/security-scan.yml`, landed in #180 on
2026-10-24 from the design in `docs/superpowers/specs/2026-10-24-weekly-security-scan-design.md`.
This ledger covers Tasks 8 and 9 of `docs/superpowers/plans/2026-10-24-weekly-security-scan.md`:
the two dispatched runs after the merge, the first scheduled run, and the dispatched run after the
fix it asked for.

**Verdict: the gate behaves as D2 and D3 say.**
- The first scheduled run went red on six fixable HIGHs in four images Aura builds, and it opened
  #189.
- After `baa95bb` the same images scanned clean, and the next run was green.
- With runners free, a run takes 1.5 to 2 minutes.
- GitHub fired the Monday cron 5 h 51 min late.
- Docker Hub answered every request from the hosted runners.

## The runs

| Run | Event | Commit | Started (UTC) | Wall time | Result |
|---|---|---|---|---|---|
| 40105988445 | dispatch | `23d4228` | 2026-10-24 15:13 | 26 min 36 s | cancelled, Craft red |
| 40108028947 | dispatch | `6a7a882` | 2026-10-24 15:40 | 36 min 43 s | green |
| 40308151675 | schedule | `420b512` | 2026-10-26 10:32 | 2 min 03 s | red, #189 opened |
| 40375348673 | dispatch | `baa95bb` | 2026-10-26 18:10 | 1 min 32 s | green |

- **40105988445**, the first run after the merge of #180.
  - The Craft job failed in 19 s with "can't open patch './docker/craft-web/pdfcraft.patch'". The
    workflow passes the checkout as ".", and `git -C <clone> apply` resolved the path inside the
    clone. `b68fe65` makes the paths absolute, and `craft_osv_test.sh` now runs the scan with
    relative paths.
  - Eight scan jobs had no result when the run was cancelled at 15:40:03. Seven had waited 22
    minutes since they were created, and `grafana/tempo` was 39 s into its scan. The jobs that ran
    took 11 to 75 s.
- **40108028947**, on `6a7a882`, which carries `b68fe65`: every job green. Each job took 10 to
  84 s. The 36 minutes were spent waiting: the jobs started between 15:42 and 16:16, while #180's
  merge and the Dependabot PRs ran CI.
- **40308151675**, the first scheduled run.
  - The cron is `41 4 * * 1`, and the run was created at 10:32:01, 5 h 51 min late.
  - Red on `aura-caddy`, `aura-egress`, `aura-ingest` and `aura-sandbox`. The report job opened #189
    in 12 s, with one row per failed job and its log link.
- **40375348673**, dispatched on `baa95bb` once the rebuilt `:edge` images were published: green,
  with the report job skipped.

## Time per job

Runs 40108028947, 40308151675 and 40375348673, from each job's `started_at` to `completed_at`.

| Job | 2026-10-24 | 2026-10-26 schedule | 2026-10-26 dispatch |
|---|---|---|---|
| `List the images` | 10 s | 10 s | 8 s |
| `Dependabot ran` | 10 s | 9 s | 12 s |
| `Go graph (govulncheck)` | 41 s | 51 s | 57 s |
| `Craft apps` | 46 s | 38 s | 52 s |
| `Open or update the issue` | skipped | 12 s | skipped |
| `arcadedata/arcadedb` | 59 s | 46 s | 62 s |
| `dxflrs/garage` | 29 s | 30 s | 31 s |
| `ghcr.io/chetto1983/aura` | 43 s | 42 s | 39 s |
| `ghcr.io/chetto1983/aura-arcadedb-mcp` | 26 s | 39 s | 46 s |
| `ghcr.io/chetto1983/aura-caddy` | 30 s | 34 s **red** | 34 s |
| `ghcr.io/chetto1983/aura-cloudflared` | 48 s | 25 s | 38 s |
| `ghcr.io/chetto1983/aura-egress` | 27 s | 32 s **red** | 34 s |
| `ghcr.io/chetto1983/aura-ingest` | 31 s | 41 s **red** | 41 s |
| `ghcr.io/chetto1983/aura-pim-mcp` | 29 s | 30 s | 29 s |
| `ghcr.io/chetto1983/aura-sandbox` | 52 s | 58 s **red** | 56 s |
| `ghcr.io/chetto1983/aura-whisper-server` | 48 s | 44 s | 39 s |
| `ghcr.io/chetto1983/whatsapp-mcp` | 37 s | 46 s | 42 s |
| `ghcr.io/ggml-org/llama.cpp` | 84 s | 90 s | 74 s |
| `ghcr.io/remsky/kokoro-fastapi-cpu` | 62 s | 55 s | 60 s |
| `ghcr.io/searxng/searxng` | 40 s | 28 s | 29 s |
| `grafana/grafana` | 42 s | 45 s | 42 s |
| `grafana/tempo` | 28 s | 29 s | 31 s |
| `postgres` | 49 s | 40 s | 45 s |
| `prom/prometheus` | 26 s | 31 s | 39 s |
| `tecnativa/docker-socket-proxy` | 40 s | 34 s | 36 s |

## Docker Hub's answers

- **What each scan job asks Hub for.**
  - It pulls `aquasec/trivy@sha256:be1190af…` from Docker Hub with `docker run`.
  - Trivy reads the scanned image from its own registry with `--image-src remote`, once for the
    SARIF and once for the gate.
  - The vulnerability database comes from `mirror.gcr.io/aquasec/trivy-db:2`, not Hub.
- **What Hub answered.** In runs 40108028947, 40308151675 and 40375348673, none of the 21 logs of
  the seven jobs that scan a Hub image (`postgres`, `arcadedb`, `garage`, `prometheus`, `grafana`,
  `tempo`, `docker-socket-proxy`) has a 429, `toomanyrequests` or rate-limit line. All 60 scan jobs
  of those three runs pulled the Trivy image.
- **Unlike the cloud container.** The baseline of 2026-10-24 got 429 from Hub there and had to go
  through `mirror.gcr.io`.

## What the red run found, and the fix

#189 listed six HIGHs, all with a fixed version. They had three causes:
- **A cached apt layer.** `aura-egress` and `aura-sandbox` kept libssl3/openssl 3.0.22-1~deb12u1
  in an apt layer cached since 2026-10-24, while bookworm-security had deb12u2 (CVE-2026-84782).
  Neither image had `APT_WEEK`.
- **A package in the base image.**
  - `aura-ingest` rebuilt its apt layer that morning and still had deb12u1: libssl3 is in the
    python base, and `apt-get install` leaves a package the base ships alone.
  - `aura-caddy`'s curl 8.22.0-r0 (CVE-2026-92392) sits in caddy:2's Alpine base the same way.
- **pip's own copies.** `aura-ingest`'s msgpack 1.1.2, urllib3 2.7.0 and setuptools 70.3.0 are pip
  26.2.2's vendored copies (`pip/_vendor/vendor.txt`), and 26.2.2 is the newest pip. Trivy reports
  them with no file path, so an acceptance in `.trivyignore.yaml` could not have been held to pip.

`baa95bb` fixes all three:
- every image Aura builds with OS packages runs its package layer again each week (`APT_WEEK`),
  and that layer upgrades what the base ships;
- `aura-ingest` uninstalls pip after the requirements.

The rebuilt `:edge` images of caddy, egress, ingest, sandbox and aura had no fixable HIGH or
CRITICAL in Trivy 0.70.0 with `.trivyignore.yaml`. #189 was closed with these measurements.

## What this does not show

- **The cron's lateness.** One scheduled run, 5 h 51 min late, is not a distribution. GitHub
  documents that scheduled runs can be delayed under load.
- **Hub's limits.** Each scan job ran on its own runner. More pulls from one address were not tried.
- **The other red paths on GitHub.** The Go, Craft and Dependabot jobs went red only through
  `b68fe65`'s script bug or not at all. Their red verdicts on a real finding are shown by the
  scripts' tests, not by a run.
- **Updating an issue already open.** A second red scheduled run while #189 was open did not happen.
- **Release images.** As D4 says, they are not scanned.
