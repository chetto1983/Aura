# Weekly security scan: every image the stack runs, every week, on GitHub

Proposed on 2026-10-24, at `e7c3d1a`. **The operator asked:** automate the checks on every
sidecar weekly, so that no vulnerability is left behind, the way senior teams do it. **The
operator decided:** it runs on GitHub only, with no fallback job on the GitLab mirror.

The five decisions below are proposals, open until the operator confirms them. The measurement
is `docs/superpowers/verification/2026-10-24-image-vulnerability-baseline.md`.

## What exists, read in the tree and on GitHub on 2026-10-24

1. **Dependabot keeps most manifests current.** `.github/dependabot.yml` has six entries:
   - gomod, npm in `/web` and GitHub Actions, weekly;
   - seven Dockerfiles, weekly, digest refresh included (since 2026-10-08);
   - `compose.yaml`, daily, `allow`-limited to `aura-whisper-server` (since 2026-10-08). The other
     third-party images are pinned by hand.
2. **Dependabot security updates are on.** They reach directories the config does not list:
   - #157: pip in `/docker/aura-ingest`;
   - #153 and #154: npm in two spike directories.
3. **No Dependabot version update has run since 2026-10-09 08:31 UTC.** This is measured from
   the runs of the `Dependabot Updates` dynamic workflow.
   - *What still runs.* Security updates do (2026-10-21 and 2026-10-22), and so do dependency-graph
     updates (2026-10-21 and 2026-10-23).
   - *What stopped.* None of the six configured entries has run since. The daily compose entry
     missed 15 days, and the weekly entries missed the Mondays of 2026-10-12 and 2026-10-19.
   - *The account outage explains only part of it.* The repository's own scheduled workflows ran
     until 2026-10-11 13:02 and again from 2026-10-21 17:23. Dependabot missed 2026-10-10 and
     2026-10-11 before that gap, and every day since it closed.
   - *The config is not the cause.* The compose entry's last run (2026-10-09 03:25) read
     `chetto1983/aura-whisper-server 2026.10.08.1056`, listed its GHCR tags, and answered "No
     update needed", which was correct that day.
   - *The documented auto-pause does not apply.* It needs a Dependabot PR open for 90 days, and
     none is open.
   - *A second silent failure.* The security update for `@vitest/mocker` in `/packages/create-aura`
     failed on 2026-10-22 with `security_update_not_possible`: vitest 4.1.11 holds it at 4.1.11,
     and the fix is 4.1.12. The run is red in Actions, and nothing reports it.
4. **Three manifests get no version updates at all:**
   - `docker/aura-ingest/requirements.txt` (pip). Security updates reach it, version updates do not.
   - `packages/create-aura/package-lock.json`, the published `create-aura-appliance` installer.
   - `docker/aura-craft-web/Dockerfile`.
5. **`govulncheck` runs only when code changes.** It runs on pull requests and pushes (`ci.yml`,
   v1.6.0). A Go advisory published on a quiet week waits for the next push. CodeQL is weekly.
6. **Nothing scans what is inside an image.** Of the 20 images the stack runs, 14 carry fixable
   HIGH or CRITICAL findings, six of them in images Aura builds:
   - `aura-sandbox`: minimist 0.0.10 (CVE-2021-44906, CRITICAL).
   - `aura-egress`: perl-base, 3 CRITICAL.
   - `aura-ingest`: pypdf 6.21.1.
   No manifest scanner sees these, and the same was true of the two CRITICAL and eight HIGH that
   chetto1983/whatsapp-mcp#11 and #12 removed today.
7. **The forks:**
   - `whatsapp-mcp` has a weekly `security.yml`. Every audit job in it carries
     `continue-on-error: true`, so the workflow cannot go red.
   - `aura-pim-mcp` has no `dependabot.yml` and no scheduled scan. OSV-Scanner reads only its
     direct `.csproj` references; the transitive NuGet graph needs
     `dotnet list package --vulnerable --include-transitive`.
   Both images are in `compose.yaml`, so a scan from Aura covers what ships.

## Decisions (proposed)

**D1. Dependabot stays, and Renovate is not added.** This reverses the recommendation of earlier
the same day, which was made before the inventory above.
- *Reasons to stay.* Dependabot is configured, its security updates work, and the steward skill's
  conventions are built on it.
- *What Renovate would cost.* A second bot would contend for the same manifests. A token is also
  needed: a pull request opened with the workflow's `GITHUB_TOKEN` does not start CI, so Renovate
  would need a PAT or an App credential.
- *What Renovate would buy.* Regex managers for the pins repeated across files. For most of them
  a check repeats the pin, so a Dependabot PR that moves only `compose.yaml` goes red and names
  the file it left behind:
  - prometheus, tempo and grafana: `verify-observability.ps1` fails;
  - arcadedb and garage: `cmd/aura/container_artifacts_test.go` fails. garage is also in
    `docker/aura/Dockerfile`;
  - llama.cpp: in four files, one outside Dependabot's directory (`9345ce4`).
- *Why that does not justify it.* A red PR is a prompt to finish the move, not a silent miss.
  The one silent case is postgres. `scripts/lib/disposable_stack.sh` and
  `scripts/coverage_docker.sh` repeat its tag and no check compares them. Those two scripts
  should read the tag from `compose.yaml`, as `verify-observability.ps1` now reads its variables,
  which is cheaper than a second bot.

**D2. A run goes red on what Aura can fix, plus third-party CRITICALs.** Every run reports what has
a fixed version, at HIGH and above. The two kinds of image differ in what turns the run red.

| Images | Red on | Why |
|---|---|---|
| Built by Aura or its forks: `aura`, `aura-caddy`, `aura-cloudflared`, `aura-ingest`, `aura-sandbox`, `aura-egress`, `aura-arcadedb-mcp`, `whatsapp-mcp`, `aura-pim-mcp` | fixable HIGH or CRITICAL | A rebuild or a bump fixes it |
| Third-party, the whisper mirror included | fixable CRITICAL | Aura can only move the pin |

- *Third-party HIGHs never fail a run.* The baseline has 76 fixable HIGH in grafana, 53 of them in
  its Go standard library. A gate on those would be red every week, and a red that never clears
  is ignored.
- *Accepting a finding.* It is accepted in `.trivyignore.yaml`, Trivy's own format. Each entry
  needs a `statement` and an `expired_at` at most 90 days away. An entry past its `expired_at`
  stops applying (Trivy docs, *Filtering*). Our convention is the statement and the 90 days.
- *The baseline's acceptances.* All three third-party CRITICALs are upstream's to fix:
  - postgres's `gosu`, built with Go 1.24.6;
  - ArcadeDB 26.10.2's netty-handler, on the newest release;
  - kokoro v0.9.0, on the newest release.

**D3. A red scheduled run opens an issue.** GitHub sends the notification for a scheduled run to
"the user who last modified the cron syntax in the workflow file" (GitHub Actions docs, *Events
that trigger workflows*, `schedule`).
- *The risk.* Commits in this repository are authored as `Claude <noreply@anthropic.com>`, so that
  user may not be the operator.
- *The fix.* On failure, the workflow opens an issue labelled `security-scan`, or comments on the
  one already open, with the run's link and its table. The operator watches the repository and is
  notified either way.

**D4. Aura's own images are scanned at `:edge`, what master ships.**
- Release tags are not scanned.
- An image unbuilt for a week still shows its new findings, because the database moves even when
  the image does not.

**D5. The weekly run also checks that Dependabot itself ran.** It reads the `Dependabot Updates`
runs of the last eight days with `gh api`. Two conditions turn it red:
- an ecosystem in `dependabot.yml` has no run in that window;
- any run in it failed.

Finding 3 shows why: Dependabot stopped for 15 days, and nothing said so. The check cannot tell a
version update from a security update by its run's name, so a security update can mask a stopped
schedule. For `docker` and `docker-compose`, which get no security updates, the check is exact.

## Design

### `.github/workflows/security-scan.yml`

**Triggers.** `schedule` weekly, plus `workflow_dispatch`. The cron is Monday at 04:41 UTC, after
CodeQL's 03:27.

**Permissions.** `contents: read`, `security-events: write`, `issues: write`, `actions: read`.
Every action is pinned by commit SHA, as in the other workflows.

**Jobs.**
1. **`images`** writes the image list:
   - `docker compose --profile '*' config --images` resolves every default in `compose.yaml`;
   - the `${VAR:?}` variables get placeholders read from the file, as
     `verify-observability.ps1` does since #160;
   - the five local defaults map to their published `:edge` refs through the variables compose
     already reads: `AURA_IMAGE`, `AURA_CADDY_IMAGE`, `AURA_CLOUDFLARED_IMAGE`,
     `AURA_INGEST_IMAGE`, `AURA_ARCADEDB_MCP_IMAGE`;
   - `aura-sandbox:edge` and `aura-egress:edge`, which the daemon starts, are appended;
   - each image is tagged `own` or `third-party`.

   A new service in `compose.yaml` is therefore scanned without anyone editing the workflow.
2. **`scan`** runs a matrix over that list, `fail-fast: false`:
   - Trivy runs from `aquasec/trivy:0.70.0@sha256:be1190af…`, with no Trivy action. The action's
     tags were rewritten in the March 2026 compromise (CVE-2026-33634); a digest-pinned image
     takes the action layer out of the trust path.
   - Pass one writes SARIF (HIGH and CRITICAL, `--ignore-unfixed`, exit 0). `upload-sarif` sends
     it with `category: trivy/<image>`. GitHub's code scanning then tracks each finding, keeps it
     open week after week, and closes it when it is gone.
   - Pass two is the gate: the severity D2 sets for the image, `--ignore-unfixed`,
     `--ignorefile .trivyignore.yaml`, `--exit-code 1`, and a table in the job summary.
3. **`go`** runs `govulncheck` v1.6.0 over `scripts/go_packages.sh`, as `ci.yml` does.
4. **`dependabot`** applies D5, with `actions: read`.
5. **`report`**: `needs: [scan, go, dependabot]`, `if: failure() && github.event_name == 'schedule'`. It opens
   or comments on the issue (D3) with `gh`.

**What it leaves to Dependabot.** No OSV-Scanner job. On 2026-10-24 every lockfile finding OSV
reported was a GHSA, which Dependabot's alerts already read. The one exception, GO-2026-5932 in
x/crypto, has no fix and is `govulncheck`'s.

### `.github/dependabot.yml`

- **New entries:** pip in `/docker/aura-ingest` and npm in `/packages/create-aura`, both weekly on
  Monday with the `build(deps)` prefix. `/docker/aura-craft-web` joins the docker directories.
- **The compose entry stays as it is.** Its last run proves the `allow` name and the GHCR lookup
  work. Widening `allow` to more images is a later change.

### Forks

- **`aura-pim-mcp`** gets a `dependabot.yml` for nuget, docker and github-actions, through its own
  PR.
- **`whatsapp-mcp`'s `continue-on-error`** is upstream's file and stays. What ships is gated by
  Aura's scan of its image.

## Order of landing

1. **Restart Dependabot's schedules.** Either the operator presses "Check for updates" on each
   entry at `/network/updates`, or the `dependabot.yml` change of step 3 lands. Both are actions the
   docs list as resetting Dependabot. On 2026-10-08 a config change started all six jobs within a
   minute.

   The proof is the next day's scheduled compose run in the `Dependabot Updates` workflow. Then
   vitest 4.1.12 goes into `/packages/create-aura`, which clears the failed security update.
2. **Clear the six images Aura builds.** Today these would turn the first run red:
   - pypdf 6.21.2 in `aura-ingest`;
   - the minimist copies in `aura-sandbox`;
   - Monday's Dependabot base-image refresh for the perl-base CRITICALs, or a manual repin;
   - then a rebuild of `:edge`.

   chi 5.4.0 and zod 4.6.6 move in the same change.
3. **Land the Dependabot entries, then the workflow** with `.trivyignore.yaml` holding the three
   third-party acceptances. In the same change, the two disposable-stack scripts read the
   postgres tag from `compose.yaml` (D1).
4. **Run it by `workflow_dispatch` on GitHub.** Measure its time and Docker Hub's answers to a
   hosted runner. Record the run in `docs/superpowers/verification/`.
5. **Write a PRD amendment** recording the gate as measured.

## What this does not cover

- **Release images.** Appliances on a release tag run images this scan does not read (D4).
- **Secrets, misconfiguration and licences.** The scanners are limited to `vuln`.
- **Reachability.** A finding in an image is reported whether or not Aura reaches the vulnerable
  code.
- **Updating third-party pins.** Apart from the whisper mirror, they still move by hand. This
  design makes a third-party CRITICAL visible; it does not move the pin.
- **The GitLab mirror** (operator's decision).
