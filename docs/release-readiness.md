# Aura release-readiness checklist

This is the release contract for Phase 41 and PRD Amendment #106.3. A checked source-control box
means the mechanism is implemented. Release approval additionally requires the named machine
evidence from the exact candidate commit; a missing, skipped, stale, or blocked required artifact
is a failed release.

## Architecture decisions

- [x] Agent loop semantics — ADR 0040
- [x] Tool consequence policy — ADR 0041
- [x] Memory provenance and erasure — ADR 0042
- [x] MCP trust and lifecycle — ADR 0043
- [x] Deployment profiles — ADR 0044
- [x] Sandbox boundary — ADR 0037

## Required candidate evidence

| Gate | Required evidence | Pass condition |
|---|---|---|
| security | CodeQL/govulncheck/workflow-pin/profile-policy jobs | zero unresolved release-blocking alerts; strict profile assertions pass |
| coverage | owned-surface coverage report | statements ≥85% with no empty/filtered tier |
| mutation | Go critical-boundary + frontend Stryker reports | killed ≥70% in every named scope |
| capability | `capability-eval.json` | every declared scenario executed and passed; zero skip/missing |
| load | `load-report.json` | supported concurrency met; success ratio and p95 inside declared budget |
| chaos | `chaos-report.json` | DB, MCP, Garage, and process-kill scenarios executed, degraded truthfully, recovered |
| disaster recovery | `dr-report.json` | Postgres, Neo4j offline, sidecars, and Garage restored and checksum-verified |
| observability | observability contract + live `/readyz` evidence | failure reasons honest and alert/runbook links valid |
| rollback | candidate rollback rehearsal | previous image/config starts, migrations are compatible, readiness returns healthy |
| audit | dated definitive closure ledger | no undisclosed `open`; `external_blocked` rows excluded from score |

## Operational checks

- [ ] Candidate image digest and source commit recorded.
- [ ] Secrets/config validated under the intended strict profile.
- [ ] Backup artifacts are outside the data volumes they protect.
- [ ] Neo4j maintenance window announced before an operator live dump.
- [ ] Restore targets are disposable and distinct from live databases/volumes/buckets.
- [ ] `/healthz`, `/readyz`, dashboards, alerts, and runbook links checked.
- [ ] Rollback command, image digest, database compatibility, and responsible operator recorded.
- [ ] External connector blockers and accepted risks disclosed.
- [ ] Definitive score computed only from executed evidence and exceeds the PRD threshold.

## Rollback rule

Stop a release when any required evidence is absent or non-terminal. Roll back application and
configuration first to the recorded digest. Database rollback is allowed only when the migration
compatibility test explicitly permits it; otherwise restore into a new target and switch after
verification. Never overwrite the last known-good backup during rollback.
