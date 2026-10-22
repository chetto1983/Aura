# Documents first-import cache refresh — 2026-10-22

## Finding and change

Final review F1 (Important/P2) found that `useIngestion` ignored a successful
`indexing: null` response. A fresh identity's first import through Files could therefore
leave its already mounted Library's empty document cache unchanged. The guard now skips
only `undefined`; successful empty snapshots participate in the existing identity-scoped
signature comparison. The polling interval, cache keys and invalidation mechanism are unchanged.

The F1 production TypeScript change is the guard in
`web/src/documents/useDocumentWorkspace.ts`. The embedded `internal/webui/dist` was rebuilt
once after the source fix, then held unchanged while the controller built and applied the
Linux binary. The live runtime was the existing local stack at `http://127.0.0.1:9080`,
using the controller's temporary `ed760a1f5+dirty` binary with those rebuilt assets.

## TDD and scope

Before the production change:

```sh
cd web
npx vitest run src/documents/useDocumentWorkspace.test.tsx --maxWorkers=2
```

RED: three regressions failed with `expected false to be true` on real query-cache
invalidation: empty-to-indexed, indexed-to-empty, and the current identity becoming empty
after switching identities. Three tests passed. Restoring the original truthiness guard is
the F1 fault probe; these failures demonstrate that the new tests kill that fault.

GREEN after the one-line change: **6/6 tests passed**, exit 0. The suite also preserves the
initial non-null snapshot behavior, repeated-null behavior, loading/error distinction,
foreign identity isolation and the existing assertion that ingestion does not recursively
invalidate/refetch itself. All three document cache families are checked for both identities.
No existing test assertion was relaxed.

## Live first import

```sh
cd web
AURA_E2E_ORIGIN=http://127.0.0.1:9080 AURA_E2E_LIVE_DOCUMENTS=1 \
  npx playwright test e2e/paperless-first-import-live.spec.ts --project=chrome \
  --output=../output/playwright/paperless-final-fix
```

The F1-only artifact run passed **1/1**, exit 0, **25.0 seconds**. A later run retained the
same successful F1 case in **19.6 seconds** against `ec4cf56e2+dirty`, before the independent
sidebar keyboard fixes; that case's explicit screenshot and JSON remain at the paths below.
A disposable identity was created
through the normal administrator UI, signed in through Authula, and completed the standard
first-run skip flow. Its Library initially displayed the empty state and returned
`{"indexing":null}`. While that Library remained mounted, the test posted one unique PDF
through the existing Files multipart upload endpoint. It issued no document-cache refetch
command and performed no navigation after upload.

The first non-null polling response reported `status: ready`, `in_progress: 0`,
`finished: 5`, `errors: 0`. The new file appeared within the test's 10-second UI deadline.
The original Library DOM element remained connected, the URL stayed unchanged, navigation
count stayed zero and the Documents upload queue remained empty. This isolates the ingestion
refresh from queue-based invalidation. The screenshot was opened and visually inspected:
the Library displayed one document and its unique synthetic filename.

Artifacts retained under
`output/playwright/paperless-final-fix/paperless-first-import-liv-c4a51-ready-mounted-empty-Library-chrome/`:

- `first-import-snapshots.json`: the actual initial/final ingestion values, filename,
  navigation count and empty queue.
- `mounted-library.png`: the unchanged Library showing the first imported document.

The first attempt exposed an incorrect assertion in this new live test: `finished` was
assumed to count documents and expected one. The measured value was five. Inspection of
`services/ingest/app.py` confirmed that it forwards CocoIndex's `total.num_finished`, a
work-component counter. The test was corrected to check ready/zero in-progress/zero errors,
positive finished work, and the actual file in the UI. The next run passed. A final run added
explicit artifact paths because the list reporter did not retain buffer-only attachments.
Both failed-attempt and successful-run cleanup completed through `finally`: only the exact
synthetic source was deleted, the tenant context was closed and the disposable identity was
removed through the administrator UI. The operator session was never logged out.

## Quality gates and limits

The frontend lint, TypeScript check, formatting check, duplicate-code check and dead-code
check passed. Duplication reported zero clones. `npm run build` passed; Vite retained its
existing advisory about chunks larger than 500 kB. Touched source files remain below 600 LOC.

The F1-only full frontend run (`npm test -- --maxWorkers=2`) passed with exit 0:
**380 files / 3,269 tests**, **435.01 seconds**. Statements were 13,723/14,875 = 92.25%,
branches 10,585/12,201 = 86.75%, functions 4,024/4,423 = 90.97%, and lines
12,378/13,140 = 94.20%. The configured exclusions and all four 85% thresholds remained
unchanged. The user's subsequent sidebar-delete request required additional source changes
and its own final full run; that consolidated evidence is recorded in
`2026-10-22-paperless-saved-view-delete.md`.

The previously completed global Stryker measurement at `ed760a1f5` remains applicable to
its unchanged configured mutation scope: **81.22%**, above its unchanged 70% break threshold.
`useDocumentWorkspace.ts` is outside that configured scope, and no configured mutant source
or mutation configuration changed. This is not a new global mutation measurement; the F1
regression's RED/GREEN cycle supplies the focused guard-fault evidence.

This run verifies the first-import refresh and its cache/identity boundaries. It does not
remeasure every Documents, Craft, backend, or broader application acceptance scenario.
