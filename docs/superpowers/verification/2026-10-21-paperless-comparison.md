# Live Paperless comparison and Aura deployment

Date: 2026-10-21, Europe/Rome. This records the baseline study, not acceptance of the new UI.

## Deployment verified

- Remote `master` and the initial checkout both pointed to `9ef3a2e73128a4dba6016c719a7bcc931e04452d`.
- Before update, `docker exec aura aura version` reported `48708ec62` and Go 1.27.2.
- Published GHCR revision labels for aura, caddy, ingest, cloudflared, sandbox and egress were
  checked against `9ef3a2e73128a4dba6016c719a7bcc931e04452d`.
- `docker compose up -d --no-build --pull never --wait --wait-timeout 300 aura arcadedb-mcp
  aura-ingest caddy aura-cloudflared aura-pim-mcp whatsapp` completed successfully after pulls.
- Main service, ingestion, MCP sidecars, proxy, tunnel, Postgres, ArcadeDB and embedding services
  reported healthy. Garage was running; its absence of a Docker healthcheck is not a failed check.
- `docker exec aura aura version` then reported the expected full SHA and Go 1.27.3.
- Per-identity browser/sandbox/egress containers were recreated with the GHCR images and their
  image labels also matched the expected revision.
- Postgres backup before update: `backups/before-ghcr-9ef3a2e73-20261021.dump`, 1,856,868 bytes.
- Existing volumes retained. Ignored `.env` image/pull-policy selectors now use GHCR.

The user's correction stopped a local build that had been started. No result of that build was
deployed. Further application changes are being implemented separately after this study.

Paperless Compose project `aura-paperless-study` is isolated from Aura. Paperless 3.3.3 is healthy
at `http://127.0.0.1:8010`; its SQLite/data/media/broker volumes are study-specific. Its only
uploaded corpus is three generated synthetic PDFs.

## Executed journeys

1. Uploaded the same three PDFs through both browser UIs.
2. Observed Paperless per-file upload/processing completion and task links.
3. Reproduced Aura's stale empty result: uploaded at about 21:17:06; indexed at 21:17:58;
   API returned three rows at 21:18:18 while UI still showed zero for `UX-study`.
4. Searched `Cedar`: Paperless found the contract; Aura's library returned zero. Filename search
   found the same Aura document.
5. Opened previews in both. Aura saved the sample agreement date `2026-08-01` and review state.
6. Assigned type `fattura` to exactly the two Aura sample invoices using bulk UI and confirmation;
   API readback verified both values and the separately reviewed agreement.
7. Saved Paperless search as `UX study - Cedar`, pinned it to sidebar/dashboard, navigated to
   dashboard and observed the one-document result there.
8. Refreshed Aura detail: document closed, URL stayed `/`, search became empty. Refreshed Paperless
   detail: `/documents/2/details` remained open.
9. Inspected both at 390×844 and captured library/detail layouts. This was a viewport test only.
10. Moved only the synthetic Paperless contract to trash, observed 29 days remaining, restored it,
    and reopened the same document ID. The trash returned to zero.
11. Inspected organization, matching rules, custom fields, workflow editor, mail and settings
    surfaces. Unsaved setup forms were cancelled. No real mail or external messages were sent.

## Artifacts and checks

- Findings and full matrix: `docs/paperless-ngx-live-comparison-2026-10-21.md`.
- Images, synthetic fixtures, isolated Compose config: `output/playwright/paperless-comparison/`.
- Visual report: `output/playwright/paperless-comparison/index.html`; served locally at
  `http://127.0.0.1:8766` from a staging folder containing only report/images/docs.
- Browser verification of report: 13 steps, 21 image elements, all decoded with nonzero width.
- Two temporary browser-auth helper files were removed after use; credentials remain only in
  their normal ignored local environment/session locations.
- Before implementation, `npx vitest run src/documents --reporter=dot`: **11 files, 46 tests pass**.
- Before implementation, WSL `go test ./internal/docfields ./internal/agui`: both packages pass
  (0.404 s and 29.208 s reported test times). These are baselines, not proof of the new changes.

## Limits

The live study does not prove OCR/Office conversion quality, large-library latency, native mobile
support, full accessibility, optional AI, multi-user rights, file versions/merge, retention expiry
or mail automation. Paperless date extraction misread the sample dates in its default config.
The full comparison distinguishes executed flows, inspected controls and source-only inventory.

The user confirmed the rebuild belongs in Documents, permitted code reuse from Paperless, and
explicitly declined worktrees. Implementation continues in the original checkout on a named
branch. No new Documents implementation is claimed complete by this record.
