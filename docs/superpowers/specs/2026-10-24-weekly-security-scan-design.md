# Weekly security scan: every image master ships, every week, on GitHub

Agreed with the operator on 2026-10-24, at `e7c3d1a`. **The operator asked:** automate the checks on every
sidecar weekly, so that no vulnerability is left behind, the way senior teams do it. **The
operator decided:** it runs on GitHub only, with no fallback job on the GitLab mirror.

The operator approved the six decisions below on 2026-10-24 ("approvo D1-D6"), and in the same
message asked for the Craft apps to be upgraded. The measurement is
`docs/superpowers/verification/2026-10-24-image-vulnerability-baseline.md`.

## What exists, read in the tree and on GitHub on 2026-10-24

1. **Dependabot keeps most manifests current.** `.github/dependabot.yml` has five entries:
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
   - *What stopped.* None of the five configured entries has run since. The daily compose entry
     missed 15 days, and the weekly entries missed the Mondays of 2026-10-12 and 2026-10-19.
   - *The account outage explains only part of it.* The repository's own scheduled workflows ran
     until 2026-10-11 13:02 and again from 2026-10-21 17:23. Dependabot missed 2026-10-10 and
     2026-10-11 before that gap, and every day since it closed.
   - *The config is not the cause.* The compose entry's last run (2026-10-09 03:25) read
     `chetto1983/aura-whisper-server 2026.10.08.1056`, listed its GHCR tags, and answered "No
     update needed", which was correct that day.
   - *The documented auto-pause does not apply.* It needs a Dependabot PR open for 90 days, and
     none is open.
   - *GitHub no longer holds the config.* The repository's Dependabot tab reads "Dependabot version
     updates aren't configured yet" (operator's screenshot, 2026-10-24). The file is on master and
     passes the schemastore `dependabot-2.0` schema with zero errors. GitHub reinstated the account
     on 2026-10-21 (`c7387e1`), and the file has not changed since `4441425` (2026-10-08). Dependabot
     reads it again when it changes on the default branch.
   - *A failed security update.* The one for `@vitest/mocker` in `/packages/create-aura` failed on
     2026-10-22 with `security_update_not_possible`: vitest 4.1.11 held it at 4.1.11, and the fix is
     4.1.12. `e7c0a8c` moved vitest by hand two hours later. Dependabot opened no PR, and a red
     dynamic run is easy to miss.
4. **Two manifests get no version updates at all:**
   - `docker/aura-ingest/requirements.txt` (pip). Security updates reach it, version updates do not.
   - `packages/create-aura/package-lock.json`, the published `create-aura-appliance` installer.

   `docker/aura-craft-web/Dockerfile` stays out on purpose. Its rust base only builds WebAssembly
   that ships `FROM scratch`. Any change to the file changes the input hash
   `TestCraftWebImagePinMatchesItsInputs` checks: a 1.97.0 → 1.99.0 edit moves it from
   `ea8923cbe8756de5` to `a7559cebc0904474`. The grouped base-image PR would then stay red until
   someone republishes the Craft image.
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
8. **Nothing watches the six Craft apps.** The cockpit serves them to view and edit files:
   PdfCraft, WordCraft, DeckCraft, PhotoCraft and FilmCraft, built from source with Aura's patches
   (`docker/aura-craft-web/Dockerfile`), and GridCraft's release zip (`docker/aura/Dockerfile`).
   - *They are behind.* All six are pinned at storytold's releases of 2026-10-17. By 2026-10-23
     each had two to five newer minor releases: pdfcraft v0.9.0 → v0.13.0, wordcraft and deckcraft
     v0.8.0 → v0.10.0, photocraft v0.10.0 → v0.15.1, filmcraft v0.9.0 → v0.12.0, gridcraft
     v0.8.0 → v0.11.1. Of their tools, trunk is at v0.21.14 (v0.22.0 is out), binaryen at
     version_123 (133 is out) and rust at 1.97.0 (1.99.0 is out). wasm-bindgen 0.2.129 is current.
   - *No check sees them.* The versions are arguments in a Dockerfile, and the `Cargo.lock` files
     live upstream. So Dependabot cannot read them, the dependency graph has no cargo package, and
     Trivy found nothing in `aura:edge`'s WebAssembly.
   - *What ships carries advisories, none reachable as used.* This was measured with
     `cargo tree --target wasm32-unknown-unknown` on each `-web` crate at its pinned tag with the
     patch applied, then OSV-Scanner:
     - zip 3.0.0 is in wordcraft, deckcraft and gridcraft: RUSTSEC-2026-0367/0368/0369, 9.1 at
       most, fixed in 9.0.0. All three concern `extract()` to a filesystem, which no source file
       in the three calls.
     - crypto-bigint, ctutils and rsa are in pdfcraft: timing side channels.
     - rustybuzz, ttf-parser and paste are unmaintained.
     - filmcraft's graph carries none.
   - *Upgrading changes none of that.* The newest tags hold the same versions of those crates.

## Decisions

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
  - *Amended 2026-10-24, once measured.* The docs say only that an entry without `expired_at`
    "is always valid". Trivy 0.70.0 stops applying an entry past it.
  - *An ID alone is too wide.* It hides the same CVE in every image, Aura's own included: kokoro's
    and `aura-egress`'s perl-base share CVEs. So each entry also names the exact `purls` Trivy
    reports. A purl at another version of the same package does not match.
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
  - *Amended 2026-10-24, the operator's decision.* A failed run counts only if no later run on the
    same entry succeeded. On that day the rule as first written would have kept the 2026-10-26 run
    red for a failure of 2026-10-22: the @vitest/mocker security update, which `e7c0a8c` had already
    done by hand.

Finding 3 shows why: Dependabot stopped for 15 days, and nothing said so. The check cannot tell a
version update from a security update by its run's name, so a security update can mask a stopped
schedule. For `docker` and `docker-compose`, which get no security updates, the check is exact.

**D6. The weekly run watches the Craft apps with a job of its own.** No tool reads these pins.
This job is new code, about forty lines of shell, so it went to the operator before anything was
written. The operator approved it.
- It reads the six pins from the two Dockerfiles.
- For each app it lists the upstream tags (`git ls-remote`) and reports newer releases. That part
  is report-only: an upgrade means rebasing a patch and a 35-minute rebuild, so it is the
  operator's call.
- It resolves the app's wasm32 graph at the pinned tag, with the patch applied
  (`cargo tree -p <app>-web --target wasm32-unknown-unknown`).
- It runs OSV-Scanner over the app's whole `Cargo.lock`, then keeps only the findings whose
  package name and version appear in the `cargo tree` output. This is how the baseline was
  measured.
- A fixable HIGH or CRITICAL left after that filter turns the job red.
- Accepting one goes in `docker/craft-web/osv-scanner.toml`, which the job passes with
  `--config`. It is OSV-Scanner's own format: one `[[IgnoredVulns]]` per advisory id, with
  `reason` and an `ignoreUntil` at most 90 days away (OSV-Scanner docs, *Configuration*). It is a
  separate file because Trivy's `.trivyignore.yaml` (D2) uses other field names.
  - *Amended 2026-10-24: the file is `.github/osv-scanner-craft.toml`.* `scripts/craft_web_inputs.sh`
    hashes `docker/craft-web/` whole, so a file there changes the Craft image pin. Accepting one
    advisory would then cost a rebuild and a re-pin.
  - *Expiry, measured with OSV-Scanner 2.7.0.* Once `ignoreUntil` is past, the finding is reported
    again.

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

**What it leaves to Dependabot.** No OSV-Scanner job over the repository's own lockfiles; D6's
Craft job is the only use of OSV-Scanner. On 2026-10-24 every finding OSV reported on those
lockfiles was a GHSA, which Dependabot's alerts already read. The one exception, GO-2026-5932 in
x/crypto, has no fix and is `govulncheck`'s.

### `.github/dependabot.yml`

- **New entries:** pip in `/docker/aura-ingest` and npm in `/packages/create-aura`, both weekly on
  Monday with the `build(deps)` prefix.
- **The compose entry stays as it is.** Its last run proves the `allow` name and the GHCR lookup
  work. Widening `allow` to more images is a later change.

### Forks

- **`aura-pim-mcp`** gets a `dependabot.yml` for nuget, docker and github-actions, through its own
  PR.
- **`whatsapp-mcp`'s `continue-on-error`** is upstream's file and stays. What ships is gated by
  Aura's scan of its image.

## Order of landing

1. **Make GitHub read `dependabot.yml` again, with the two new entries.**
   - *Why this way.* The Dependabot tab shows no configuration, so it has no "Check for updates"
     to press. A change to the file on the default branch makes Dependabot read it. On 2026-10-08
     one started a run for every configured entry within a minute.
   - *The proof.* The Dependabot tab lists the seven entries, and the next day's scheduled compose
     run appears in the `Dependabot Updates` workflow.
2. **Clear the six images Aura builds.** Every fixable HIGH and CRITICAL in the baseline would
   turn the first run red. Each one is fixed or accepted with a dated entry:
   - `aura-ingest`: pypdf 6.21.2, plus libexpat1 and libbrotli1 from the Debian base;
   - `aura-sandbox`: the minimist copies, undici, brace-expansion, libexpat1, libbrotli1;
   - `aura-egress`: perl-base and libbrotli1, through Monday's Dependabot base-image refresh or a
     manual repin;
   - `aura`: libexpat1, libbrotli1, brace-expansion, undici;
   - `aura-caddy`: brotli-libs and golang.org/x/net;
   - `aura-cloudflared`: the Go standard library, x/net and klauspost/compress inside
     Cloudflare's binary, and libssl3t64, by moving its pinned release;
   - then a rebuild of `:edge`.

   chi 5.4.0 and zod 4.6.6 move in the same change.
3. **Land the workflow** with `.trivyignore.yaml` holding the three third-party acceptances. In
   the same change, the two disposable-stack scripts read the postgres tag from `compose.yaml`
   (D1).
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
