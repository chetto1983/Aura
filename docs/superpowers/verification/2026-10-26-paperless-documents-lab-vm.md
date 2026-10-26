# Paperless-style Documents on the lab VM

Date: 2026-10-26. This closes the last two steps of Task 5 in
`docs/superpowers/plans/2026-10-21-paperless-documents.md`: the gates, then publication and the
running commit.

**Verdict:** the work is published and runs on the lab VM, and every CI job passes on a later
master SHA that contains it.

## What was measured

- **Ancestry.** `564aa3bc4`, the last source fix, is in the `v1.2.0` tag and in `c034fb3c5`.
- **CI.** All 29 jobs of `ci.yml` passed in run 40285370268 on `df12d57e8` (2026-10-26), which
  contains the work. They include:
  - Build + vet + lint + deadcode + file-size cap;
  - Unit tests (race detector), and the Race + leak DB tier;
  - Integration tests (db_integration);
  - the owned-surface coverage gate;
  - Web unit tests (Vitest + coverage) and Web E2E (Playwright);
  - Stryker mutation, the three Go mutation groups, and Critical mutation testing.
- **The lab VM.** On `192.168.101.158`, `aura` runs `c034fb3c5`, read from the image's
  `org.opencontainers.image.revision` label. It is healthy and was started at 07:21:46Z. The
  updater pulled it.
- **The Documents API**, read with the operator's own session (GETs only, no test identity):

| Request | Result |
|---|---|
| `GET /api/documents/ingestion` | 200, `ready`, 367 finished, 0 in progress, 0 errors |
| `GET /api/documents/labels?kind=tag` | 200 |
| `GET /api/documents/views` | 200 |
| `GET /documents/library` (the cockpit route) | 200 |

The first labels request was sent without `kind` and answered 400. The handler requires the kind,
so the error was the probe's.

## What this does not show

- The CI run is on `df12d57e8`, not on the SHA where the work landed on 2026-10-22. That day's
  gates are the frontend ones in `2026-10-22-paperless-documents-acceptance.md`.
- No workflow ran on the VM. Upload, search, saved views and previews were accepted on the local
  stack on 2026-10-22, in the same file. The probe shows that the API and the route answer, not
  that the UI works.
- The bodies of the labels and views responses were not read.
