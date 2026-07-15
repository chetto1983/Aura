---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 18
type: execute
wave: 8
depends_on: ["37F-09", "37F-11", "37F-13", "37F-16", "37F-17"]
files_modified:
  - internal/webui/dist
  - docs/aura-quality-snapshot.md
autonomous: true
requirements: [WEBSHARE-04]

must_haves:
  truths:
    - "The full Go tag matrix passes locally on a disposable DB before any push"
    - "Every owned 37F package individually clears 85% coverage, not just the aggregate"
    - "The web gates pass: vitest >=85% and Stryker >=70%"
    - "The SC3 core survives mutation at >=70% killed with zero leak-class survivors"
    - "internal/webui/dist matches the web source — the CI freshness gate compares them"
    - "docs/aura-quality-snapshot.md is re-attested for every row whose CI-gate-path glob matches a file this phase changed"
    - "scripts/quality_snapshot_gate.sh prints ok before the push"
  artifacts:
    - path: "docs/aura-quality-snapshot.md"
      provides: "re-attested quality rows for every glob this phase touched"
    - path: "internal/webui/dist"
      provides: "a rebuilt bundle matching the web source"
  key_links:
    - from: "docs/aura-quality-snapshot.md"
      to: "scripts/quality_snapshot_gate.sh"
      via: "the freshness gate matching changed files against row globs"
      pattern: "quality_snapshot_gate"
  prohibitions:
    - "MUST NOT run the coverage gate against the live aura DB — use scripts/coverage_docker.sh (disposable aura_cov). The 2026-07-10 footgun truncated the live deployment's auth tables (operator identity + Authula wiped, no backup)."
    - "MUST NOT run web gates in WSL — WSL has no node; vitest/tsc/prettier/stryker run on Windows Git Bash"
    - "MUST NOT run .exe binaries natively on this host — AV blocks them; build and run in a container or WSL"
    - "MUST NOT bump a quality-snapshot date without a re-attestation note naming what changed and why the number can or cannot move"
    - "MUST NOT accept a mutation survivor that changes what reaches the Snapshot, at any score"
    - "MUST NOT run docker builder prune — the stack is a ~45-60min cold rebuild"
---

<objective>
Prove the whole phase green locally, rebuild the bundle, and re-attest the quality snapshot — everything
that must be true **before** the push.

Two project gates make this a real plan rather than a formality:
- **`scripts/quality_snapshot_gate.sh`** fails the push if any changed file matches a quality-row glob
  whose `Last measured` date is stale (PRD amendment #20). 37F touches a wide surface, so several rows
  will match.
- **The dist freshness gate** compares `internal/webui/dist` against the web source. 37F adds a whole
  route and several components, so a stale dist is a red job.

A green local full-matrix run is worth more than a push-and-wait CI cycle — the coverage job takes ~20
minutes to tell you it failed.

Purpose: everything verifiable, verified, before anyone waits on CI.
Output: a green local matrix, a fresh dist, a re-attested snapshot.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@CLAUDE.md
</context>

## Artifacts this plan produces

A rebuilt `internal/webui/dist` and re-attested `docs/aura-quality-snapshot.md` rows.

<tasks>

<task type="auto">
  <name>Task 1: Full local gate — Go matrix, web gates, mutation, dist rebuild</name>
  <read_first>
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` — the sampling rate, the coverage floor, and the Manual-Only table
    - `scripts/coverage_docker.sh` and `scripts/coverage_gate.sh` — the disposable-DB runner and the gate it wraps. `coverage_gate.sh:35` refuses `db_integration` against a DB named `aura` locally; never bypass it.
    - `Makefile` — the `quality` and `quality-full` targets and what each runs
    - `CLAUDE.md` §"Quality tooling & gates" — the WSL toolchain (prepend `~/.local/bin:~/go/bin` to PATH; the login shell does not include them), the mutation recipe, and the `.env` `AURA_WEB_AUTH_SECRET` leak that breaks `make coverage`
    - The prior dist-rebuild commit `0d58e6a9f` ("chore(web): rebuild internal/webui/dist to match source (CI freshness)") — the precedent for how the bundle is rebuilt and committed
  </read_first>
  <action>
    Run the whole gate locally, in the right place for each piece.

    **Go (WSL — the full primary dev environment; `CGO_ENABLED=1` so native `-race` works):**
    - `export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"` — the login shell does not include these.
    - `unset AURA_WEB_AUTH_SECRET` — `.env` exports it, it flips `SecretConfigured`, and it breaks the
      config tests.
    - `make quality` — vet + build + file-size + lint(+dupl) + race + vuln.
    - `bash scripts/coverage_docker.sh` — the **disposable** `aura_cov` DB, stack up. **Never** the live
      `aura` DB.
    - Assert the aggregate ≥85% **and every owned 37F package individually ≥85** — `internal/share`,
      `internal/agui`, `internal/objectstore`, `internal/runner`, `internal/cron/handlers`,
      `internal/config`. The 2026-06-13 campaign floor is per-package, not just aggregate. Record all six.
    - Mutation spot-check (WSL only — the only `go-mutesting` fork supporting go1.26): re-run on
      `internal/share/redact.go`, `snapshot.go`, and `token.go`; confirm ≥70% killed; reconcile with plan
      37F-03's recorded score. Apply the autopsy rule: classify survivors; `%w`-dense ones are
      near-equivalent and may be advisory-accepted **with a written reason**. **A survivor that changes
      what reaches the Snapshot is not acceptable at any score** — that survivor is a live leak.

    **Web (Windows Git Bash — WSL has no node):**
    - `npx tsc --noEmit -p web/tsconfig.json`
    - `npx vitest run --coverage` — ≥85% on `web/src/chat/share/*`, `web/src/shell/ShareShell.tsx`,
      `web/src/routes/SharePage.tsx`, `web/src/settings/SharedLinksSection.tsx`
    - `npx stryker run` — ≥70%. Targets per RESEARCH: `web/src/chat/share/*` (the modal state machine, the
      tier logic, the stale detection) and `web/src/shell/ShareShell.tsx`.
    - `npx eslint web/src` and the project's prettier check.

    **Rebuild the bundle:** `internal/webui/dist` must match the web source — CI compares them and a stale
    dist is a red job. Follow `0d58e6a9f`'s precedent. Known cross-platform trap: a Windows-generated
    lockfile misses the Linux WASM optional deps (`@emnapi/*`), so if the dist build feeds a Linux image,
    run `npm install` in CI or in a `node:22` container rather than committing a Windows-only lock.

    If any gate is red, fix it here. Do not hand a red tree to the next plan.
  </action>
  <verify>
    <automated>bash scripts/coverage_docker.sh</automated>
  </verify>
  <acceptance_criteria>
    - `make quality` exits 0 (vet, build, file-size, lint+dupl, race, vuln).
    - `bash scripts/coverage_docker.sh` exits 0 with the aggregate ≥85%, run against the **disposable** `aura_cov` DB.
    - **Every owned 37F package individually reports ≥85.0%** — all six numbers recorded in the SUMMARY.
    - `golangci-lint run ./...` reports 0 issues; `govulncheck ./...` clean.
    - Mutation on `internal/share/redact.go` + `snapshot.go` + `token.go` is ≥70% killed, every survivor classified, and **zero** leak-class survivors accepted.
    - `npx tsc --noEmit -p web/tsconfig.json` clean; `npx eslint web/src` reports 0 errors.
    - `npx vitest run --coverage` ≥85% on every new web module.
    - `npx stryker run` ≥70% on `web/src/chat/share/*` and `web/src/shell/ShareShell.tsx`.
    - `internal/webui/dist` is rebuilt and matches the source; the CI freshness gate would pass.
    - No `.exe` was run natively on this host.
  </acceptance_criteria>
  <done>The full Go tag matrix, the web gates, and both mutation gates are green locally on a disposable DB, and `internal/webui/dist` matches the web source.</done>
</task>

<task type="auto">
  <name>Task 2: Quality snapshot re-attestation (PRD amendment #20)</name>
  <read_first>
    - `docs/aura-quality-snapshot.md` — the row table, the `Last measured` column, and the note format. **Read the rows and their CI-gate-path globs**; identify every row whose glob matches a file 37F changed.
    - `scripts/quality_snapshot_gate.sh` — the gate that fails the push. Read what it matches and what it requires.
    - `CLAUDE.md` §"QUALITY SNAPSHOT AT PHASE CLOSE" — the exact rule: for EVERY row whose CI-gate-path glob matches a changed file, bump `Last measured` to today and **PREPEND a re-attestation note** — a fresh measurement if the metric moved, else a **metric-neutral justification naming exactly what changed and why the number cannot move** — keeping the prior notes as `Prior …`.
  </read_first>
  <action>
    Re-attest the quality snapshot for this phase's blast radius.

    First, compute the actual match set — do not guess. Run the gate with the real changed-file set and
    base date (the exact invocation is in CLAUDE.md's rule): it must print `ok: … checked N row(s)`. Any
    row it flags is a row you must handle.

    For **every** flagged row: bump `Last measured` to today and **PREPEND** a re-attestation note.
    - If the metric **moved** — and several will, since 37F adds a package and a large test surface —
      record the **fresh measurement from Task 1's real run**. Do not carry an old number forward.
    - If the metric **cannot move**, write a **metric-neutral justification naming exactly what changed and
      why** — e.g. a row whose glob matches a file 37F only added a route line to, where the measured
      property is untouched. A bare date bump with no reason is exactly what this gate exists to catch.
    - Keep the prior notes, demoted to `Prior …`.

    Re-run until it prints `ok`. **Verify locally FIRST** — the CI job runs the same script, and a failed
    push after a 20-minute wait is the expensive way to learn this.
  </action>
  <verify>
    <automated>bash scripts/quality_snapshot_gate.sh</automated>
  </verify>
  <acceptance_criteria>
    - Running the gate with the real changed-file set and base date prints `ok: … checked N row(s)` and exits 0 (the full invocation is in CLAUDE.md's QUALITY SNAPSHOT rule).
    - Every flagged row has `Last measured` = today AND a **prepended** re-attestation note.
    - Every moved metric carries a **fresh number from Task 1's real run** — not a carried-forward value.
    - Every metric-neutral row's note **names the specific change** and why the number cannot move. No bare date bumps.
    - Prior notes are preserved as `Prior …`, not deleted.
    - `git diff docs/aura-quality-snapshot.md` shows no row deleted.
  </acceptance_criteria>
  <done>Every quality row matching this phase's changed files is re-attested with today's date and a note that is either a fresh measurement or a specific metric-neutral justification, and the gate prints `ok`.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| local green → CI green | A locally-passing tree can still fail CI on the dist freshness gate, the quality-snapshot gate, or the stricter Skills db_integration-only coverage job. Each is checked locally first. |
| the coverage gate → the live database | The gate creates and drops databases. Pointed at the wrong one, it truncates production. |
| a mutation score → actual test strength | A high score with an unclassified leak-class survivor is a false comfort. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-64 | Tampering | the coverage gate truncating the live deployment's auth tables | mitigate | `scripts/coverage_docker.sh` only (disposable `aura_cov`, dropped on exit); `coverage_gate.sh:35` refuses a DB named `aura` locally. This closed the 2026-07-10 incident that wiped the operator identity and Authula with no backup. |
| T-37F-81 | Repudiation | a falsely-green CI from a stale dist | mitigate | The bundle is rebuilt and diffed against source before the push, per the `0d58e6a9f` precedent. |
| T-37F-82 | Repudiation | a bare quality-snapshot date bump hiding an unmeasured regression | mitigate | Every flagged row gets a fresh measurement or a metric-neutral justification naming the specific change; prior notes preserved. Bare bumps are precisely what the gate catches. |
| T-37F-84 | Repudiation | a mutation survivor in the SC3 core silently accepted | mitigate | Survivors are classified; `%w`-dense near-equivalents may be advisory-accepted with a reason, but a survivor that changes what reaches the Snapshot is rejected at any score. |
| T-37F-63 | Repudiation | a single package silently below the floor behind a passing aggregate | mitigate | The floor is asserted per-package as well as in aggregate; all six numbers recorded. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | 37F adds zero dependencies. `govulncheck` runs in `make quality`; the npm lock is not modified. |
</threat_model>

<verification>
- `make quality` → 0
- `bash scripts/coverage_docker.sh` → aggregate ≥85%, every owned package ≥85%
- `go-mutesting` on the SC3 core (WSL) → ≥70% killed, no leak-class survivor
- `npx tsc --noEmit`, `npx vitest run --coverage` (≥85%), `npx stryker run` (≥70%), `npx eslint web/src` (Windows Git Bash)
- `internal/webui/dist` matches source
- `scripts/quality_snapshot_gate.sh` with the real changed-file set → prints `ok`
</verification>

<success_criteria>
Everything a machine can prove is proven: the full matrix is green locally on a disposable DB with every
owned package ≥85%, the SC3 core survives mutation with no leak-class survivor, the web gates clear both
floors, the bundle is fresh, and the quality snapshot is honestly re-attested.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-18-SUMMARY.md` when done.
Record the six per-package coverage numbers, both mutation scores, and the quality rows re-attested.
</output>
