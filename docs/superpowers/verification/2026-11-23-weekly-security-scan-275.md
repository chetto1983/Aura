# Weekly security scan: closing #275, and Trivy 0.70.0 to 0.76.2

Date: 2026-11-23. The scheduled scan of 2026-11-23 (run 45204704065) went red in one job,
`Scan ghcr.io/chetto1983/whatsapp-mcp`, and opened #275.

## What the red run found

56 fixable HIGH, all in one Debian package: krb5 (`libgssapi-krb5-2`, `libk5crypto3`,
`libkrb5-3`, `libkrb5support0`) at `1.21.3-5+deb13u1`, fixed in `+deb13u2`.

**The cause.** whatsapp-mcp's Dockerfile has run `apt-get upgrade` since chetto1983/whatsapp-mcp#13,
and `APT_WEEK` takes that layer out of the cache once per ISO week. A new week only counts if a
build runs in it, though. `publish-image.yml` there runs on a push to main, a tag or a dispatch,
and main had no push from 2026-11-10 to 2026-11-23. `:latest` stayed on the 2026-11-10 apt layer.

**The fix.** On 2026-11-23 at 15:36 UTC, the merge of release 0.3.0 (`0ad30d0`) republished
`:latest` with `APT_WEEK=2026-W48`. Publish runs 45207065866 (push) and 45207158066 (tag dispatch)
both succeeded. That same day, `python:3.11-slim` offered `libkrb5-3 1.21.3-5+deb13u2` as the
candidate (`apt-cache policy`).

## Trivy 0.76.2

The pin had been 0.70.0 since 2026-10-24, and nothing moves it automatically: Dependabot does not
read an image reference in a workflow `env`.

- **The image.** `aquasec/trivy:0.76.2` has index digest `sha256:8f6bb947…` on Docker Hub, and
  `ghcr.io/aquasecurity/trivy:0.76.2` has the same one (`docker buildx imagetools inspect`).
- **Its signature.** `cosign verify` was run on the digest with cosign v3.1.4, with this
  certificate identity:
  `^https://github.com/aquasecurity/trivy/\.github/workflows/.+@refs/tags/v0\.76\.2$`. It
  passed against the OIDC issuer `token.actions.githubusercontent.com`. cosign v2.6.1 reports
  "no signatures found" on the same digest.
- **The changelog, 0.71.0 to 0.76.2.** It has two breaking changes:
  - `getHostByName` was removed from report templates;
  - VEX documents now load only from within the repository directory.

  The scan uses neither, since its formats are `table` and `sarif` and it has no VEX. 0.76.1 and
  0.76.2 are rebuilds of Trivy's own image for its own CVEs.

## Measured locally

Trivy 0.70.0 and 0.76.2 were run in WSL with Docker 29.9.2, both on one cache: vulnerability DB
v2 updated 2026-11-23 13:07 UTC, Java DB updated 2026-11-23 00:59 UTC.

- **Raw report.** Run on `cloudflare/cloudflared:2026.10.7` (HIGH and CRITICAL,
  `--ignore-unfixed`, no ignore file), both versions return the same two findings:
  CVE-2026-78661 in x/net v0.61.0 and CVE-2026-85091 in zlib1g.
- **What `.trivyignore.yaml` accepts.** Each case used a scratch ignore file on that same image.
  The two versions agree on every case:

  | Case | Expected | 0.70.0 | 0.76.2 |
  |---|---|---|---|
  | exact purl, x/net with its binary's path, `expired_at` 2027 | 0 | 0 | 0 |
  | both purls at another version | 2 | 2 | 2 |
  | exact purls, `expired_at` 2026-01-01 | 2 | 2 | 2 |
  | x/net with the path `usr/local/bin/other` | 1 (x/net) | 1 | 1 |

- **The gate, with the repository's `.trivyignore.yaml`.** It was run with the severity the
  workflow gives each image kind. Both versions return exit code 0 and no finding on the first 12
  images of the matrix:
  - arcadedb and garage;
  - aura-arcadedb-mcp, aura-caddy, aura-cloudflared, aura-egress, aura-ingest and aura-pim-mcp;
  - aura-sandbox and aura-whisper-server;
  - `aura:edge` and `whatsapp-mcp:latest`.

  The other 8, from llama.cpp CUDA to docker-socket-proxy, were stopped there and left to CI.
  The run was anonymous, so it read only public images.

## On GitHub

| Run | Event | Commit | Started (UTC) | Wall time | Result |
|---|---|---|---|---|---|
| 45204704065 | schedule | — | 2026-11-23 | — | red in 1 job, #275 opened |
| 45216611130 | dispatch | `d224f93ee` | 2026-11-23 16:26 | 12 min 22 s | green |

**Verdict:** green. `d224f93ee` carries the pin (`8bb1017a5`), and the run ran Trivy
`0.76.2@sha256:8f6bb947`. 24 of its 25 jobs passed: 20 image scans, the image list, Craft, Go and
Dependabot. The report job was skipped, as it is on every dispatched run.

## What this does not show

- **The 8 images left to CI.** They were measured only in the CI run's gate, not compared with
  0.70.0.
- **Every ignore entry.** The semantics were measured on one OS package and one Go binary. The
  npm, maven and apk entries were not exercised one by one under 0.76.2.
- **The next stale week.** The cause is still open in whatsapp-mcp: its publish workflow has no
  schedule.
