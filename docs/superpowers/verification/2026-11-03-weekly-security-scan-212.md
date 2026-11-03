# Weekly security scan: closing #212

Date: 2026-11-03. The scheduled scan of 2026-11-02 (run 41448214954) went red in nine of its 24
jobs and opened #212. The fix is `e41737b40`, with its payload manifest in `5423b1ad1`.

**Verdict:** green. The scan dispatched on the pushed head (`3704edfb9`, which carries both commits)
passed all 24 jobs: 20 image scans, the image list, Craft, Go and Dependabot. The report job
was skipped.

What upstream had already fixed was moved. What only upstream can fix was accepted with a dated
reason, as the issue asks.

## What the red run found

| Job | Finding | Cause |
|---|---|---|
| Craft apps | zip 3.0.0: RUSTSEC-2026-0429 / GHSA-q7v9-x93c-xqv3 (7.5), GHSA-m665-v7pm-7grj (10.0) | DeckCraft, GridCraft and WordCraft lock zip 3.0.0; their newest tags (v0.14.1, v0.15.0, v0.14.0) still do |
| aura-cloudflared | go-jose/v4 4.1.4, CVE-2026-116587 (HIGH) | Cloudflare's cloudflared 2026.10.3 binary |
| aura-caddy | go-jose/v4 4.1.5, CVE-2026-116587 (HIGH) | Caddy v2.11.7, the newest release, requires 4.1.5 |
| aura, aura-sandbox | undici 6.29.0, CVE-2026-109383 and CVE-2026-87018 (HIGH) | the copy npm 11.21.1 bundles |
| aura-sandbox | 13 Go 1.26.4 and 2 x/text v0.38.0 CVEs (HIGH) | TypeScript 7.0.2's native `tsc` in the artifact template |
| arcadedb, postgres, grafana | brotli-libs 1.2.0-r1, CVE-2026-111497 (CRITICAL) | their Alpine bases |
| kokoro | libbrotli1 1.1.0-2+b7, CVE-2026-111497 (CRITICAL) | v0.9.0's Debian base |

## What upstream had already fixed

| Image | Old | New | Checked |
|---|---|---|---|
| aura-cloudflared | cloudflared 2026.10.3 | 2026.10.7 (`sha256:4955524a…`) | 2026.10.6 bumps go-jose/v4 to 4.1.6, 2026.10.4 klauspost/compress to 1.20.3 (`RELEASE_NOTES`, `go.mod` at the tag) |
| aura-caddy | go-jose/v4 4.1.5 | `--replace` to 4.1.6, beside the x/net one | `caddy build-info`: go-jose/v4 v4.1.5 => v4.1.6, x/net v0.59.0 => v0.60.0 |
| grafana | 13.2.4 (`sha256:dbbc38ea…`) | 13.2.5 (`sha256:9f7aa1e1…`) | published 2026-10-27 |
| aura-tts | Kokoro v0.9.0 | v0.9.2 | published 2026-10-30; v0.9.1 bumps anyio, urllib3, certifi |

The index digests come from `docker buildx imagetools inspect`, and match Docker Hub's tag API.

Six acceptances in `.trivyignore.yaml` had no finding left to hide after the move, and went:
- klauspost/compress for cloudflared 2026.10.2;
- perl-base (three), anyio and certifi for Kokoro v0.9.0.

## What only upstream can fix

Accepted until 2027-02-01, each held to its exact purl, and for Aura's own images to a path:
- **brotli-libs 1.2.0-r1** in ArcadeDB 26.10.2 (Alpine 3.24.2) and Postgres 18.6-alpine3.24
  (Alpine 3.24.3). Neither tag was rebuilt after 2026-10-26. ArcadeDB's only newer image is
  26.11.1-SNAPSHOT.
- **undici 6.29.0**, two more CVEs at the path the 2026-10-24 entry already names. npm 11.21.1 and
  12.2.1, the newest, still bundle 6.29.0 (checked with `npm pack`).
- **TypeScript 7.0.2's tsc**: 15 CVEs at the binary's path. 7.0.2 is the newest release (7.1 is a
  beta), and create-vite's react-ts template asks for `~7.0.2`.
- **zip 3.0.0**, in `.github/osv-scanner-craft.toml`: the three advisories that gate. They belong
  to the class accepted on 2026-10-24.
  - **What the advisories hit.** `ZipArchive::extract`'s symlink buffer and
    `ZipStreamReader::extract`.
  - **What the apps call.** At the pinned tags, with Aura's DeckCraft patch, the three apps use
    only `ZipArchive::new`, `by_name`, `by_index`/`by_index_raw` and `ZipWriter`. A search for
    `.extract(` and `ZipStreamReader` over all their Rust sources finds no call to either.
  - **What the target compiles.** zip 3.0.0 builds the symlink branch only when
    `cfg!(unix) || cfg!(windows)`, and `wasm32-unknown-unknown` has no filesystem to extract to.

## Measured locally

Trivy 0.70.0 and OSV-Scanner 2.7.0 were used, the same binaries as the workflow, in WSL with
Docker 29.9.2.

- **Aura's two rebuilt images.** `aura-cloudflared` and `aura-caddy` were built from this tree,
  caddy with `APT_WEEK=2026-W45`. Each has 0 fixable HIGH or CRITICAL with `.trivyignore.yaml`.
  - `cloudflared_image_contract_test.sh` passes, and reports `cloudflared version 2026.10.7`.
  - `cloudflare_caddy_origin_test.sh` passes for both Caddyfiles, and `dns.providers.desec` is
    still compiled in.
- **The new upstream images, without the ignore file.**
  - `cloudflare/cloudflared:2026.10.7` has 0 HIGH or CRITICAL.
  - Grafana 13.2.5 and Kokoro v0.9.2 have 0 CRITICAL.
- **The images that stay.** ArcadeDB 26.10.2 and Postgres 18.6-alpine3.24 reproduce the CI's
  brotli finding without the ignore file, and with it have none. Their 2026-10-24 entries,
  netty-handler and gosu, still hide a live finding each.
- **The published `aura` and `aura-sandbox` `:edge` images** have 0 with the new ignore file.
  - **The negative check.** A copy of the file with the `tsc` path changed brings back exactly
    the 15 `tsc` findings, and nothing else. The YAML anchors (`&tsc`, `*tsc`) therefore bind
    each entry to that path, and do not widen it to the whole image.
- **OSV-Scanner on the three Cargo.lock files.** The old config reports five advisories on
  zip 3.0.0. The new one leaves two: GHSA-8hxf-qr8p-62x4 (6.5) and RUSTSEC-2026-0368 (no score),
  both below the 7.0 gate.
- **Grafana 13.2.4 to 13.2.5.**
  - `verify-observability.ps1` passes on 13.2.5, runtime smoke included: 4 dashboards, 20
    alerts, 87 queries.
  - On one named volume, 13.2.4 provisioned it and then 13.2.5 opened it. Both report
    `database: ok`, the same 4 dashboards (`aura-agents`, `aura-data-retention`,
    `aura-overview`, `aura-tools-mcp`), the same 2 datasources, and no error line in the log.
- **Kokoro v0.9.0 to v0.9.2.** Both versions got Aura's request (`voice: if_sara`) for one Italian
  sentence with a time, a decimal comma and an ellipsis.

  | Format | v0.9.0 | v0.9.2 |
  |---|---|---|
  | opus | 7.245 s of audio, 132,288 bytes, 4.36 s | 7.245 s, 132,216 bytes, 3.14 s |
  | mp3 | 7.296 s, 116,780 bytes, 3.29 s | 7.296 s, 116,780 bytes, 2.92 s |

  v0.9.2's new body cap is 32 MB (`MAX_REQUEST_BODY_MB`). Aura sends at most
  `AURA_TTS_MAX_CHARS` (4096) characters.

## On GitHub

| Run | Event | Commit | Started (UTC) | Wall time | Result |
|---|---|---|---|---|---|
| 41448214954 | schedule | `aee0fa711` | 2026-11-02 12:52 | — | red in 9 jobs, #212 opened |
| 41602419949 | dispatch | `3704edfb9` | 2026-11-03 08:19 | 7 min 36 s | green |

- **The images it read.** It ran after `Publish Aura edge image` (41600067159) had succeeded on
  `3704edfb9`. At that point `aura-caddy:edge` and `aura-cloudflared:edge` had the same digests
  as their `master-3704edfb9…` tags: `sha256:b192fdb9…` and `sha256:08ff8f90…`.
- **The Craft job ran the full gate.** It built each app's wasm32 graph with cargo, which the
  local check above could not do (WSL has no cargo), and passed.
- **The same push's CI was red in one job**, `Sandbox docker_integration tier`. The sign-in
  desktop tests of `621355ba5`, a parallel session's commit, fail on `set AURA_SIGNIN_BROWSER_IMAGE
  to the measured desktop digest`. None of these files is involved.

## What this does not show

- **The appliance.** No lab VM or appliance ran the new images. The tunnel's cloudflared was not
  connected to Cloudflare's edge: the contract test runs it idle, with no network.
- **Kokoro's voice.** The check compares audio length and size, not how the voice sounds.
- **Grafana with live data.** Its smoke used synthetic series and one synthetic trace.
- **The accepted findings' reachability.** It was argued from where each package sits, not
  exercised. The one exception is zip: there, the absence of the calls was checked in the
  source.
- **Dependabot's #207.** It still proposes cloudflared 2026.10.6 and five base-image digests; it
  was neither merged nor measured here.
