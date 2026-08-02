# Aura release-readiness checklist

This is the release contract for Phase 41 and PRD Amendment #106.3. A checked source-control box
means the mechanism is implemented. Release approval additionally requires the named machine
evidence from the exact candidate commit; a missing, skipped, stale, or blocked required artifact
is a failed release.

The machine gate is:

```bash
make evidence-contracts
make release-readiness
```

`make release-readiness` accepts only the ten canonical JSON reports in
`artifacts/production-readiness/`, all bound to the exact full `git rev-parse HEAD`, all newer
than 24 hours. It emits `release-readiness-report.json` with the SHA-256 of every input so the
approved bundle cannot be silently replaced.

For a publishable candidate, run the GitHub Actions `Production Readiness` workflow on the
candidate branch and supply the immutable previously-approved image. Its job downloads only
successful CI artifacts for that exact SHA, verifies exact-SHA CodeQL, performs the live image
rollback rehearsal, runs the ten-report gate, and uploads the immutable bundle. The tag-triggered
`Release` workflow refuses to publish unless that exact commit has a successful
`Production readiness bundle` check.

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
| security | `security-report.json` from `security_evidence.py` | exact-SHA CodeQL Go+JS, govulncheck, workflow-pin, strict-profile tests pass |
| coverage | `coverage-report.json` | statements ≥85% with the `db_integration` tier; no empty/filtered tier |
| mutation | `mutation-report.json` | killed ≥70% separately for gateway, identity, profile, sandbox, and frontend |
| capability | `capability-eval.json` | every declared scenario executed and passed; zero skip/missing |
| load | `load-report.json` | supported concurrency met; success ratio and p95 inside declared budget |
| chaos | `chaos-report.json` | DB, MCP, Garage, and process-kill scenarios executed, degraded truthfully, recovered |
| disaster recovery | `dr-report.json` | Postgres, sidecars, and Garage restored and checksum-verified (three planes; memory has no drill yet) |
| observability | `observability-report.json` | negative fixtures, runtime smoke, live health/readiness, dashboards, alerts, runbooks pass |
| rollback | `rollback-report.json` | distinct image digests; previous config starts; migrations compatible; candidate restored healthy |
| audit | `audit-closure-report.json` | current-only register is empty; `release_ready:true`; zero `open` or `external_blocked` rows |

## Operational checks

- [ ] Candidate image digest and source commit recorded.
- [ ] Secrets/config validated under the intended strict profile.
- [ ] Backup artifacts are outside the data volumes they protect.
- [ ] Restore targets are disposable and distinct from live databases/volumes/buckets.
- [ ] `/healthz`, `/readyz`, dashboards, alerts, and runbook links checked.
- [ ] Rollback command, image digest, database compatibility, and responsible operator recorded.
- [ ] External connector blockers and accepted risks disclosed.
- [ ] Current audit register is empty; historical scores are not reused as release evidence.

## Rollback rule

Stop a release when any required evidence is absent or non-terminal. Roll back application and
configuration first to the recorded digest. Database rollback is allowed only when the migration
compatibility test explicitly permits it; otherwise restore into a new target and switch after
verification. Never overwrite the last known-good backup during rollback.
