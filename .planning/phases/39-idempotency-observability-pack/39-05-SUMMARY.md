---
phase: 39-idempotency-observability-pack
plan: 05
subsystem: observability
tags: [prometheus, grafana, tempo, otlp, docker-compose, alerting, ci]

# Dependency graph
requires:
  - phase: 39-idempotency-observability-pack
    plan: 04
    provides: "Stable bounded OTel metrics, private Prometheus listener, and OTLP tracing lifecycle"
provides:
  - "Tested Prometheus recording and alert rules with stable dashboard, panel, threshold, and runbook links"
  - "Four provisioned Grafana dashboards with bounded queries and Prometheus-to-Tempo correlation"
  - "Digest-pinned optional Compose profile with private scrape/OTLP ports and bounded local retention"
  - "One hermetic verifier for asset parsing, catalog/link validation, negative fixtures, and runtime smoke"
affects: ["39-06 learning observability", "39-07 phase closeout", "operations", "CI"]

# Tech tracking
tech-stack:
  added:
    - "Prometheus v3.13.1 (official image, multi-arch digest pinned)"
    - "Grafana v12.3.3 (official image, multi-arch digest pinned)"
    - "Tempo v2.9.0 (official image, multi-arch digest pinned)"
  patterns:
    - "Alert annotations form a checked graph to immutable dashboard UIDs, panel IDs, thresholds, and runbooks"
    - "Prometheus and Tempo share Aura's network namespace to consume loopback-only telemetry without host publication"
    - "Static and clean-room runtime checks share one PowerShell verifier entry point in local development and CI"

key-files:
  created:
    - observability/prometheus/rules/aura-recording.yml
    - observability/prometheus/rules/aura-alerts.yml
    - observability/prometheus/tests/aura-rules.test.yml
    - observability/grafana/dashboards/aura-overview.json
    - observability/grafana/dashboards/aura-agents.json
    - observability/grafana/dashboards/aura-tools-mcp.json
    - observability/grafana/dashboards/aura-data-retention.json
    - observability/tempo/tempo.yml
    - scripts/verify-observability.Tests.ps1
  modified:
    - compose.yaml
    - .env.example
    - .github/workflows/ci.yml
    - scripts/verify-observability.ps1

key-decisions:
  - "Prometheus and Tempo use network_mode service:aura, preserving the Phase 39 loopback-only metrics/OTLP boundary while remaining reachable to Grafana through Aura's Compose DNS identity."
  - "All operational images are literal official tag-plus-digest references; scrape and OTLP ports are never host-published, while Grafana is loopback-only and anonymous Viewer access cannot edit provisioned dashboards."
  - "Tempo exclusively owns 14-day local trace retention; Aura retention metrics and cleanup never claim or delete Tempo blocks."
  - "The verifier derives allowed metric names from the catalog and recording rules, checks the alert/dashboard/runbook graph, and proves negative cases before starting an isolated trace/series/dashboard smoke."

patterns-established:
  - "Dashboard queries use catalog-owned raw metrics or canonical recording rules and only the six bounded metric dimensions plus Prometheus-owned le/job/instance labels."
  - "Every alert panel has a stable numeric ID, checked threshold, explicit no-data copy, and a runbook data link."
  - "Hermetic config validation uses the same digest-pinned Prometheus, Tempo, and Grafana images that Compose deploys."

requirements-completed: [OBS-04]

coverage:
  - id: D1
    description: "Prometheus recording rules, multi-window alerts, deterministic threshold/debounce annotations, and three operator runbooks cover readiness, LLM/tools/MCP/DB/idempotency, retention, disk, and learning pressure."
    requirement: OBS-04
    verification:
      - kind: unit
        ref: "observability/prometheus/tests/aura-rules.test.yml via pinned promtool v3.13.1"
        status: pass
      - kind: other
        ref: "scripts/verify-observability.ps1 alert/dashboard/runbook graph and threshold checks"
        status: pass
    human_judgment: false
  - id: D2
    description: "Four immutable Grafana dashboards cover the required operational domains with bounded queries, stable panel IDs, no-data behavior, runbook links, and Tempo drilldown."
    requirement: OBS-04
    verification:
      - kind: integration
        ref: "scripts/verify-observability.ps1 clean-room Grafana provisioning and UID API smoke"
        status: pass
      - kind: other
        ref: "83 catalog-backed PromQL queries and 20 alert links checked by the static verifier"
        status: pass
    human_judgment: false
  - id: D3
    description: "The optional Compose profile is digest pinned, resource bounded, internally networked, retention bounded, read-only-configured, health checked, and gated by the same CI verifier."
    requirement: OBS-04
    verification:
      - kind: integration
        ref: "docker compose --profile observability config --quiet"
        status: pass
      - kind: integration
        ref: "scripts/verify-observability.ps1 synthetic Prometheus series plus OTLP trace plus four dashboards"
        status: pass
      - kind: unit
        ref: "scripts/verify-observability.Tests.ps1 ten precise negative fixtures"
        status: pass
    human_judgment: false

duration: 5h 17m across resumed sessions
completed: 2026-07-21
status: complete
---

# Phase 39 Plan 05: Deployable Observability Pack Summary

**Aura now ships a digest-pinned, privately networked Prometheus/Grafana/Tempo pack whose alerts, dashboards, runbooks, catalog queries, and runtime correlations are continuously verified as one contract.**

## Performance

- **Duration:** 5h 17m elapsed across resumed sessions
- **Started:** 2026-07-21T15:30:09+02:00
- **Completed:** 2026-07-21T20:47:21+02:00
- **Tasks:** 3 TDD tasks
- **Implementation files:** 19

## Accomplishments

- Added deterministic recording rules and 20 page/warning alerts for readiness, scheduler/resume, LLM SLO burns, tools/MCP/DB, idempotency conflicts and indeterminate outcomes, retention backlog/failure, disk pressure, and learning capacity.
- Added promtool rule tests plus operator runbooks whose stable dashboard UID, panel ID, URL, threshold, debounce, and runbook annotations are machine checked.
- Added four read-only Grafana dashboards with stable UIDs, 27 stable panels, bounded-label queries, explicit no-data behavior, alert-aligned thresholds, runbook links, and Tempo trace drilldown.
- Added local Tempo storage with a fixed 14-day retention owner and Grafana Prometheus exemplar/trace-to-metrics correlation.
- Added an optional Compose profile with literal official image digests, private scrape/OTLP networking, loopback-only Grafana, read-only config mounts, named data volumes, resource budgets, healthchecks, and bounded retention.
- Expanded the verifier into a 452-line cross-platform contract gate with ten negative fixtures, pinned promtool/Tempo/Grafana checks, Compose rendering, metric/label catalog validation, alert-link validation, and a clean-room synthetic series/trace/dashboard smoke.

## Task Commits

Each task kept explicit RED/GREEN history:

1. **Task 1: Define Prometheus recording, alert, and runbook semantics**
   - RED - `244b94edc` (test): alert semantics, threshold, no-data, and link contract.
   - GREEN - `99269004a` (feat): recording rules, alert rules, promtool tests, and operator runbooks.
2. **Task 2: Provision stable Grafana dashboards and Tempo correlation**
   - RED - `af7d5b7b1` (test): stable dashboard/provisioning asset contract.
   - CONTINUITY - `dd6cee184` (wip): persisted the pause/resume handoff without production changes.
   - GREEN - `9b3d3d611` (feat): four dashboards, Grafana provisioning, datasource correlation, and Tempo storage.
3. **Task 3: Wire digest-pinned Compose deployment and hermetic CI validation**
   - RED - `62834e9bc` (test): invalid YAML/JSON, duplicate UID, catalog drift, link drift, image, port, and path fixtures.
   - GREEN - `f8ffc48c6` (feat): Compose profile, CI gate, static validation, and clean-room runtime smoke.

## Files Created/Modified

- `observability/prometheus/rules/aura-recording.yml` - canonical low-cardinality operational recordings.
- `observability/prometheus/rules/aura-alerts.yml` - deterministic page/warning semantics and stable operator links.
- `observability/prometheus/tests/aura-rules.test.yml` - executable promtool rule expectations.
- `observability/runbooks/*.md` - readiness, LLM/tools, and data/retention triage.
- `observability/grafana/dashboards/*.json` - overview, agents, tools/MCP, and data/retention dashboards.
- `observability/grafana/provisioning/**/aura.yml` - immutable datasource and dashboard providers.
- `observability/tempo/tempo.yml` - internal OTLP receivers and bounded local trace retention.
- `compose.yaml` and `.env.example` - optional deployable profile, budgets, storage, and loopback UI knob.
- `scripts/verify-observability.ps1` - single local/CI validation and runtime-smoke entry point.
- `scripts/verify-observability.Tests.ps1` - precise negative contract suite.
- `.github/workflows/ci.yml` - blocking hermetic observability job.

## Decisions Made

- The private metrics listener remains loopback-only. Prometheus shares Aura's network namespace rather than weakening the listener bind or publishing port 9464.
- Tempo shares the same namespace, so Aura's existing `localhost:4317` OTLP configuration remains private and operational under the profile. Grafana reaches both sidecars through `http://aura` on the default Compose network.
- Grafana is exposed only on `127.0.0.1`; anonymous access is Viewer-only, UI writes are disabled for provisioned dashboards, and sign-up/login forms are disabled.
- Dashboard variables are omitted instead of allowing operator-entered label values. Panels aggregate only over finite catalog dimensions.
- Runtime smoke uses isolated, digest-pinned containers and leaves no containers, networks, or temporary data behind. It emits a valid bounded Prometheus series and OTLP JSON trace, then queries Prometheus, Tempo, and each Grafana UID.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Preserved the loopback-only metrics security invariant with a shared network namespace**
- **Found during:** Task 3 Compose wiring.
- **Issue:** A normally networked Prometheus container cannot scrape Aura's intentionally loopback-only `127.0.0.1:9464` listener.
- **Fix:** Prometheus and Tempo use `network_mode: service:aura`; neither scrape nor OTLP ports are host-published.
- **Verification:** Compose render, verifier port checks, and clean-room private scrape/trace smoke pass.
- **Committed in:** `f8ffc48c6`.

**2. [Rule 3 - Blocking] Added Windows PowerShell 5 native-argument compatibility**
- **Found during:** Task 3 runtime smoke.
- **Issue:** Windows PowerShell 5 split dotted native flags such as `--config.file`, while CI uses PowerShell 7 semantics.
- **Fix:** The verifier quotes only dotted native arguments on Windows PowerShell 5 and preserves normal argv elsewhere.
- **Verification:** Pinned Tempo config validation and full runtime smoke pass locally; CI invokes the identical script under `pwsh`.
- **Committed in:** `f8ffc48c6`.

**3. [Rule 1 - Bug] Corrected one dashboard recording-rule name**
- **Found during:** Task 3 catalog/query graph validation.
- **Issue:** The retention dashboard referenced `aura:learning_oldest_age:max` instead of the defined `aura:learning_oldest_age:max_seconds` record.
- **Fix:** Updated the panel query and retained catalog-derived unknown-metric rejection.
- **Verification:** All 83 queries pass; the unknown-metric negative fixture still fails precisely.
- **Committed in:** `f8ffc48c6`.

---

**Total deviations:** 3 auto-fixed blocking/bug issues.
**Impact on plan:** All fixes strengthened deployability or validation without broadening host exposure, label cardinality, or product scope.

## Issues Encountered

- Tempo's distroless image cannot initialize a fresh Docker-managed trace volume through a shell. The service runs as `0:0` solely to initialize and write its dedicated named volume; it has no host-published ports, receives read-only config, and remains resource bounded.
- Clean-room tmpfs mounts start root-owned. The smoke assigns temporary writable ownership or uses root only inside disposable containers, then removes every container, network, and temporary directory.
- Prometheus 3 rejects scrape responses without a declared content type. The production config now specifies `PrometheusText0.0.4` as a fallback, allowing the minimal smoke server while preserving format parsing.
- Grafana's search endpoint searches titles rather than exact UIDs. The smoke uses `/api/dashboards/uid/<uid>` so stable identity, not fuzzy text, is the assertion.
- `pwsh` is not installed on this Windows host. Local verification used Windows PowerShell 5 with the same script; the CI job runs the required `pwsh -NoProfile` command on Ubuntu.

## Threat Surface Scan

- No dashboard or rule introduces identity, conversation, request, operation-key, raw tool/server name, URL, prompt, response, SQL, or error-text labels.
- The verifier rejects unknown `aura_*` metrics and labels outside the six bounded catalog dimensions plus Prometheus-owned `le`, `job`, and `instance`.
- Prometheus port 9090, Aura scrape port 9464, Tempo query port 3200, and OTLP ports 4317/4318 are not host-published. Grafana alone is loopback-published.
- All three official images are literal multi-architecture digest references; CI validates the exact same references and refuses tag-only substitutions.
- Config and dashboard mounts are read-only. Prometheus, Tempo, and Grafana state use separate named volumes with explicit retention/resource budgets.
- Tempo owns and compacts its trace blocks for 336 hours. Aura retention operations are explicitly forbidden from deleting Tempo storage.
- The runtime smoke uses fixed non-sensitive synthetic IDs/content and removes all disposable containers, networks, and files in `finally` cleanup.

## Known Stubs

None introduced. The verifier's `-StaticOnly` mode is an explicit CI contract gate, while the default invocation executes the full clean-room runtime smoke. No TODO, FIXME, XXX, HACK, placeholder feature, or unimplemented production branch was added.

## User Setup Required

Start the optional pack with `docker compose --profile observability up -d`. Grafana defaults to `http://127.0.0.1:3000`; `AURA_GRAFANA_PORT`, Prometheus retention, and observability memory budgets are documented in `.env.example`. Keep `AURA_OTEL_EXPORTER=otlp` and `AURA_OTEL_ENDPOINT=localhost:4317` to send Aura traces to the profile-local Tempo receiver.

## Next Phase Readiness

- Plan 39-06 can add any remaining learning/retention signals against the checked catalog and existing data/retention dashboard without inventing labels or UIDs.
- Plan 39-07 can close the phase against one executable observability contract rather than separately auditing YAML, JSON, links, Compose, and runtime correlation.
- No blockers.

---
*Phase: 39-idempotency-observability-pack*
*Completed: 2026-07-21*

## Self-Check: PASSED

**Files verified to exist:**
- FOUND: `observability/prometheus/rules/aura-recording.yml`
- FOUND: `observability/prometheus/rules/aura-alerts.yml`
- FOUND: `observability/prometheus/tests/aura-rules.test.yml`
- FOUND: `observability/grafana/dashboards/aura-overview.json`
- FOUND: `observability/grafana/dashboards/aura-agents.json`
- FOUND: `observability/grafana/dashboards/aura-tools-mcp.json`
- FOUND: `observability/grafana/dashboards/aura-data-retention.json`
- FOUND: `observability/tempo/tempo.yml`
- FOUND: `scripts/verify-observability.ps1`
- FOUND: `scripts/verify-observability.Tests.ps1`

**Commits verified to exist:**
- FOUND: `244b94edc` (Task 1 RED)
- FOUND: `99269004a` (Task 1 GREEN)
- FOUND: `af7d5b7b1` (Task 2 RED)
- FOUND: `dd6cee184` (continuity handoff)
- FOUND: `9b3d3d611` (Task 2 GREEN)
- FOUND: `62834e9bc` (Task 3 RED)
- FOUND: `f8ffc48c6` (Task 3 GREEN)

**Fresh plan-level verification:**
- `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-observability.Tests.ps1` - pass, ten negative fixtures.
- `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-observability.ps1 -StaticOnly` - pass, 4 dashboards, 20 alerts, 83 queries, pinned tooling, and Compose render.
- `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/verify-observability.ps1` - pass, synthetic series, OTLP trace, and four provisioned dashboards.
- `docker compose --profile observability config --quiet` - pass.
- `go test ./cmd/aura -run 'TestProductionContainerArtifactsMatchFatImageContract|TestDotEnvTemplateHygiene' -count=1` - pass.
- `bash scripts/check-file-size.sh` - pass; verifier is 452 lines and negative suite is 132 lines.
- `git diff --check` and commit hooks - pass.
