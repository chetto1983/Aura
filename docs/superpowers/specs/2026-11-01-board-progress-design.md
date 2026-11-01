# Board progress from editor through API and storage

The operator extended the board UI task on 2026-11-01: progress and its API are required.
This supersedes the original presentation-only exclusion of progress. Covers, assignees,
attachments and global shell changes remain outside this request.

## Evidence and reuse

A real authenticated POST /api/board/cards with progress=0.55 returned 200, but the response
and subsequent GET omitted the field. The disposable probe card was deleted. Source agrees:
boardCardInput, board.Card/NewCard/CardPatch and aura.board_cards have no progress field.

Installed SVAR 2.6.0 getEditorItems({progress:true}) produces the native slider with min=0,
max=1, step=0.1. Its card computes round(progress*100). Use that ratio throughout, with no
adapter or second percent representation. The existing custom card body is necessary for
Aura metadata; the installed Kanban does not export its stock body. Aura has no progress
primitive to reuse, so add a semantic HTML progress element inside that existing body.

## Contract

- JSON `progress` is a finite number from 0 through 1, inclusive. 0.55 displays as 55%.
  Missing on create defaults to 0; missing on update leaves the stored value unchanged.
  An explicit 0 resets progress. Values outside the range and nonnumeric JSON are refused.
- PostgreSQL column is NOT NULL double precision DEFAULT 0 with a range CHECK. Existing
  cards receive 0. The migration's down drops only this new field. No inferred progress
  from columns: moving or renaming a column does not change a user's recorded progress.
- Add/update/list/search/get, move and duplicate retain or expose progress. An untouched
  save remains a no-op; progress-only updates must count as real changes. Duplicate copies
  progress along with the existing content.
- The deferred board tool accepts progress on add/update, publishes its bounds in its
  JSON schema and includes the percentage in the bounded card snapshot. Existing identity
  isolation, seen-card requirements, approvals and classifications remain unchanged.
- The native editor includes Progress/Avanzamento. The card displays a bar and percentage,
  including explicit 0% and 100%, with an accessible label and numeric value. Treat absent
  progress in an old server response as 0 while rebuilding/testing the client.
- Existing API paths and provider are reused; no new route, environment variable or
  dependency is introduced.

## Validation

- RED then GREEN for a real Add/Update store boundary rejecting invalid ratios, HTTP JSON
  roundtrip/absent-vs-zero behavior, tool propagation/rendering, and visible progress.
- Disposable PostgreSQL: default backfill, fractional add/read/update, reset, omitted field
  preservation, no-op saves, move/duplicate preservation, cross-identity isolation and
  range constraints, plus migration down/up. Discover the next migration slot at creation.
- WSL targeted unit/race, integration, vet/build, coverage and mutation of changed logic;
  sqlc generate and relevant frontend quality checks.
- Browser: slider edit, explicit save/reload and API GET returning the same ratio against
  the new backend, plus existing desktop/mobile geometry. An old VM response cannot prove
  the new persistence path. Record deployment and test-harness boundaries explicitly.
- Independent review of the final backend/UI changes before committing/pushing.

Risk: medium (additive migration and public API field). No env/catalog additions. Every
edited non-generated source file remains at most 600 lines. Work stays on master and
preserves other sessions' changes.
