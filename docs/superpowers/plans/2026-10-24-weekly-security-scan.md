# Weekly security scan: implementation plan

**Status (2026-10-24):**
- *Done:* Task 1 (#161); Tasks 2–6 on the operator's "Intanto inizia il workflow della scansione
  settimanale", given while #174 was in CI, on a local branch from master `03a127c`.
- *Next:* Task 7.
- *Pending:* Tasks 8–10 depend on the first run on GitHub.

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans, task by task, with
> test-driven-development for every script.

**Goal:** every image master ships, the Craft apps and the Go graph are scanned weekly on GitHub. A
fixable finding Aura can act on, a stopped Dependabot or a third-party CRITICAL turns the run red
and opens an issue.

**Architecture:** `.github/workflows/security-scan.yml`, weekly plus `workflow_dispatch`. Its jobs
call one script each in `scripts/security_scan/`, so that every piece runs and is tested off
GitHub:
- `images.sh` writes the image matrix from `compose.yaml`.
- The `scan` matrix runs Trivy from a digest-pinned image.
- `go` runs govulncheck.
- `dependabot_fresh.sh` reads the `Dependabot Updates` runs.
- `craft_osv.sh` resolves each Craft app's wasm32 graph and filters OSV-Scanner's findings to it.
- `report` opens or updates one issue on a red scheduled run.

**Tech Stack:**
- bash, with `jq` and `gh`. There is no `yq`: the dev container's `yq` is the Python wrapper and
  the runner's is mikefarah's, and their syntaxes differ. `awk` reads `dependabot.yml`;
- Trivy 0.70.0 (`aquasec/trivy@sha256:be1190af…`);
- OSV-Scanner 2.7.0, as a release binary checked by SHA-256;
- govulncheck v1.6.0;
- cargo 1.97.0, the Craft toolchain's.

**Spec:** `docs/superpowers/specs/2026-10-24-weekly-security-scan-design.md` (D1–D6, approved
2026-10-24). **Baseline:** `docs/superpowers/verification/2026-10-24-image-vulnerability-baseline.md`.

## Global Constraints

- **Pins.** Every action is pinned by commit SHA with a `# vN` comment, and no tool is installed at
  `@latest`. `scripts/workflow_pin_gate.sh` must pass.
- **The workflow file holds wiring only.** Every decision a run makes lives in a script with a
  test. The tests run in `ci.yml` beside `workflow_pin_gate_test.sh`, so a broken script goes red
  on its PR, not on a Monday.
- **No input-hash churn.** Nothing is added under `docker/craft-web/` or `docker/aura-craft-web/`:
  `scripts/craft_web_inputs.sh` hashes both whole, and a new file there would force a republish of
  the Craft image (`TestCraftWebImagePinMatchesItsInputs`).
- **Accepting a finding.** Every acceptance is dated, at most 90 days out, with its reason in the
  file. Trivy's go in `.trivyignore.yaml`, OSV's in `.github/osv-scanner-craft.toml`.
- **Shell style.** Each script uses `set -euo pipefail` and stays under 600 lines, in the style of
  `scripts/workflow_pin_gate.sh`.

## Review Focus

1. **The gate is not a skip.** A scan that cannot pull its image, download its database or read
   its input fails the job. It never reports zero findings.
2. **The image list fails closed.** A compose image still on a local default (`*:local`,
   `*:latest` without a registry) stops the `images` job instead of being dropped.
3. **Permissions are the least the jobs need.** `security-events: write` only where SARIF is
   uploaded, `issues: write` only in `report`.
4. **The Dependabot check reads entries, not ecosystems.** `npm` in `/web` and in
   `/packages/create-aura` are two entries. Run titles read
   `<ecosystem> in <dirs>[ for <deps>] - Update #N`, as measured on 2026-10-24.
5. **Expiry is measured, not assumed.** Neither tool's documentation says what an expired
   acceptance does. Trivy's docs say only that one without `expired_at` "is always valid". Each
   script's test proves an expired entry stops applying.

## Decisions taken while writing this plan

- **The OSV-Scanner config moves out of `docker/craft-web/`** to `.github/osv-scanner-craft.toml`.
  D6 named `docker/craft-web/osv-scanner.toml`. Measured:
  - `scripts/craft_web_inputs.sh` runs `find docker/craft-web docker/aura-craft-web -type f`;
  - so any file added there changes the pin hash;
  - accepting one advisory would then cost a 31-minute Craft rebuild and a re-pin (run 40091654320
    compiled for 1,834 s).

  The spec gets an amendment line.
- **`craft_osv.sh` clones and patches by itself** and does not call `docker/craft-web/build.sh`.
  - *Why not reuse it.* `build.sh` builds, and the scan needs only the source. Extracting its
    clone-and-patch half into a helper would change `docker/craft-web/`, with the republish above.
  - *What is duplicated.* About five lines: clone at the tag, check the commit, `git apply`.
  - *The guard.* The test pins that both read the same pins from the Dockerfile.
- **`packages: read` is added to the permissions.** The `scan` job pulls `ghcr.io/chetto1983/*`
  with the workflow token. A public package needs no login, and a private one fails without it.
  The spec's list omitted it.
- **The gate keeps D2's two Trivy passes**, SARIF then the gate. Both share one cache directory,
  so the image and the database download once per job. `trivy convert` was not tried.

## File structure

- `.github/workflows/security-scan.yml`: the jobs; the wiring only.
- `scripts/security_scan/images.sh` and `images_test.sh`: compose to matrix JSON.
- `scripts/security_scan/dependabot_fresh.sh` and `dependabot_fresh_test.sh`: D5 over
  `gh api` JSON (the test feeds a recorded fixture).
- `scripts/security_scan/craft_osv.sh` and `craft_osv_test.sh`: D6. The pins come from the two
  Dockerfiles, the tags from `git ls-remote`, the graph from `cargo tree`, the findings from
  OSV-Scanner, and the filter in `jq`.
- `scripts/security_scan/report.sh`: D3, the issue body from the run's failed jobs.
- `.trivyignore.yaml`: the three third-party CRITICAL acceptances (D2).
- `.github/osv-scanner-craft.toml`: empty of acceptances at first. The baseline's Craft findings
  are below HIGH or not fixable; Task 5 re-measures.
- `scripts/lib/disposable_stack.sh` and `scripts/coverage_docker.sh`: read the postgres tag from
  `compose.yaml` (D1).
- `.github/workflows/ci.yml`: run the new `*_test.sh`.

## Tasks

### Task 1: Dependabot reads its config again

- [x] Add pip `/docker/aura-ingest` and npm `/packages/create-aura` (#161). All seven entries ran at
  10:37 UTC on 2026-10-24 (`Dependabot Updates` runs 168–174).

### Task 2: the image list

- [x] Test first, against a fixture compose file:
  - a local default with no mapping is refused;
  - the five mapped variables resolve to `ghcr.io/<owner>/<name>:edge`;
  - `aura-sandbox` and `aura-egress` are appended;
  - the result is de-duplicated;
  - `ghcr.io/<owner>/*` is `own`, except `aura-whisper-server`, which is `third-party` (D2).
- [x] `images.sh [compose] [owner]` builds the result:
  - placeholders come from the file's own `${VAR:?}` names, as `verify-observability.ps1` reads
    them;
  - `docker compose --profile '*' config --images` resolves the rest;
  - the output is JSON for `fromJSON`.
- [x] Against the real `compose.yaml` it prints the 20 images the baseline scanned: 9 own, 7 at
  edge. With the whisper exception removed, the test fails.

### Task 3: the Dependabot check

- [x] Build a fixture from the titles of the 2026-10-24 runs. Test that:
  - every entry of `.github/dependabot.yml` is found;
  - removing an entry's runs fails it;
  - a failed run fails;
  - a run older than eight days does not count.
- [x] Write `dependabot_fresh.sh`. It reads the entries with `awk` and the runs with
  `gh api --paginate`, and prints a table to the job summary.
  - *Live result on 2026-10-24.* Six entries are ok. npm in `/packages/create-aura` fails on
    run 39730585760: the @vitest/mocker security update of 2026-10-22, which `e7c0a8c` did by hand.
  - *What follows from D5 as written.* That run keeps the check red until 2026-10-30, so the
    first scheduled run, on 2026-10-26, fails on it. Whether a later success clears a failed run
    is the operator's call.

### Task 4: the Craft job

- [x] Test the parts that need no network:
  - the pins read from both Dockerfiles: six apps with tag and commit, GridCraft from its `ADD`
    line;
  - the filter: an OSV finding whose `name@version` is absent from the `cargo tree` output is
    dropped;
  - severity: `max_severity` ≥ 7.0 with a fixed version fails;
  - an expired `[[IgnoredVulns]]` no longer hides a finding. This was measured once with
    OSV-Scanner 2.7.0 against WordCraft's lock, not in the test: it needs the OSV API.
- [x] Write `craft_osv.sh`. For each app it:
  - lists newer tags (report-only);
  - clones at the pinned tag and checks the commit;
  - applies `docker/craft-web/<app>.patch`;
  - runs `cargo tree --locked -p <app>-web --target wasm32-unknown-unknown -e normal`;
  - runs OSV-Scanner over the `Cargo.lock` with `--config`, then filters.
- [x] Run it locally over the six apps at #174's pins, in 47 s with a warm cargo cache.
  - *What gates.* Only zip 3.0.0, in WordCraft, DeckCraft and GridCraft: RUSTSEC-2026-0367 (7.3)
    and RUSTSEC-2026-0369 (9.1). The baseline found the same.
  - *Why they are accepted until 2027-01-22.* Both concern `extract()` to a filesystem. No source
    file of the three calls it, and the wasm32 target has no filesystem.
  - *The rest.* 14 lower findings, reported only.

### Task 5: the workflow

- [x] Write `.trivyignore.yaml` with the three acceptances (gosu in postgres, netty-handler in
  ArcadeDB, kokoro's five).
  - *Expiry.* Trivy 0.70.0 stops applying an entry past `expired_at`.
  - *Scope.* An ID alone would hide the same CVE in Aura's images, so every entry carries its
    exact purl. A purl at another version does not match.
- [x] Write `security-scan.yml` with the jobs `images`, `scan` (matrix, `fail-fast: false`),
  `go`, `dependabot`, `craft` and `report`.
  - *Triggers:* cron `41 4 * * 1` plus `workflow_dispatch`.
  - *Pins:* actions pinned at the SHAs `ci.yml` and `codeql.yml` already use. `upload-sarif` uses
    the same commit as `codeql-action/init`.
- [x] Write `report.sh`. It finds the open issue labelled `security-scan` or creates it (and the
  label), then comments with the run link and the failed jobs.
- [x] Check it: `bash scripts/workflow_pin_gate.sh`, actionlint 1.7.7 and every `*_test.sh`.
- [x] Run both Trivy steps locally with the workflow's flags.
  - *`aura-caddy:edge` (own).* The SARIF holds 2 results. The gate is red on brotli-libs and x/net,
    as in the baseline, and the second pass takes 1 s from the shared cache.
  - *`tempo` (third-party).* The gate is green.

### Task 6: one postgres tag (D1)

- [x] `disposable_stack.sh` and `coverage_docker.sh` read `postgres:` from `compose.yaml` when
  `POSTGRES_IMAGE` is unset.
  - *The test.* It checks the tag from another working directory, and checks that no copied tag
    is left.
  - *A gap found on the way.* The library test was never run by any workflow, and now `ci.yml`
    runs it.

### Task 7: the images Aura builds

- [ ] Re-measure the seven own images at `:edge` with the Task 5 gate. #171 already moved the
  Debian base (perl) and cloudflared.
- [ ] Fix each fixable HIGH or CRITICAL left, or accept it with a dated reason. The spec's step 2
  has the list. pypdf 6.21.2 is held by Dependabot's cooldown, so it moves by hand.

### Task 8: the first run on GitHub

- [ ] Merge, then run by `workflow_dispatch`. A dispatch needs the file on the default branch.
- [ ] Record the time per job, Docker Hub's answers, and each job's verdict in
  `docs/superpowers/verification/<date>-weekly-security-scan-first-run.md`.

### Task 9: the first scheduled run

- [ ] The Monday 04:41 UTC run: confirm the cron fired. If it is red, confirm the issue opened.

### Task 10: PRD amendment

- [ ] Record the gate as measured: what turns it red, the time, and what it does not cover.
