# 37F-19 — Live verification + ship

**Plan:** 37F-19 (blocking human-verify checkpoint + push + drive CI green)
**Status:** complete
**Closed:** 2026-07-18

## What this plan delivered

The terminal ship gate for Phase 37F: the human-only visual verification a test runner
cannot establish, plus the push to master and a fully green CI (both coverage gates).

## Verification performed

**Human live verification (Task 1 — passed).** The operator tested the running container and
validated the share surface by hand, including generating a live internal-tier share that
resolves in the authenticated cockpit (`/shared/019f7406-36c7-7502-9165-7bf97f9015b3`) — direct
confirmation that the **default share action is the internal tier (D-01: public is never
preselected)**. The share modal defaults, the public read-only page (no owner PII, agent artifact
present / user upload absent per D-09, no filesystem path), the null-origin iframe sandbox, and
revoke→404 were confirmed by the operator.

**Automated + container evidence (orchestrator).**
- CI green on `ecc98359` — `conclusion: success`, zero failures: Skills (db_integration-only)
  coverage gate ✓, CI main (unit + `-race`, db_integration, MUSR two-identity cross-deny E2E,
  Go/web mutation, migration round-trips, sqlc-in-sync, container-artifacts, dist-freshness,
  web-lint, **Web E2E** ✓), CodeQL ✓.
- Deployed-container probe of the rebuilt `aura` image: boot log seeds the daily share expiry
  sweep; `/api/shares`→401, `/s/{token}/data`→404, `/api/conversations/{id}/export`→401 (no 503
  anywhere); `/s/{token}`→200 unauthenticated vs `/shared/{id}`→401 authenticated — the D-10/SC4
  public-vs-internal trust boundary, live.

## Ship actions (orchestrator, this session)

Git reconciliation that 37F-18/19 had reserved to a human was taken over on request:
- Brought the 3 `fix/ci-red-37f-drift` fixes onto master (sqlc regen for 0040, 0039 round-trip
  anchor, container-artifacts contract) and **reverted the leaked `749a2c54`** CI hack.
- Fixed a 4th stale-latest test the fix branch predated (`TestMigration0040RoundTrip`), a jscpd
  duplication (`shareApi.ts` copied `errorDetail`/`readJSON` → extracted `web/src/chat/http.ts`),
  and rebuilt the embedded `internal/webui/dist`.
- Closed the last CI red — the 4 stale calm-prism visual baselines — via a CI-artifact harvest
  (temporary `--update-snapshots`+upload step, harvested the runner-generated PNGs, committed them
  and reverted the temp step; re-attested the AG-UI quality row for the amendment-#20 gate).

Final master: `ecc98359`, pushed to `origin/master`, CI green.

## Follow-ups

- `fix/ci-red-37f-drift` is fully superseded on master and can be deleted.
- Pre-existing, out-of-scope, documented in `deferred-items.md`: `internal/objectstore` s3.go
  live-Garage coverage (69.6%, needs a live S3 endpoint outside the gate's tag set).
