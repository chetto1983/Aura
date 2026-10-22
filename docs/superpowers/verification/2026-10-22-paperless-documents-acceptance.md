# Paperless document workflow acceptance

Date: 2026-10-22. Scope: the Documents A/B workflow, tested against the existing live stack at
`http://127.0.0.1:9080`. The temporary runtime contains the frontend/backend source from
`6a9f8a4cb`; branch base `5bb0a9176` adds documentation. This initial acceptance work changes tests only.
It does not establish that this revision has been published or deployed from GHCR.

## Repeatable browser coverage

The new `web/e2e/paperless-workflow-live.spec.ts` creates its own PDF, Rust source and one-second
WAV. It observes a real completed upload waiting for indexing, waits for all selected uploads to
finish storing before reloading, and obtains each indexed document through the real API using
`file_type=all`. The workflow exercises content search with a token absent from the filename,
the default document category, classification/date save, explicit review, a server saved view,
reload, a fresh authenticated browser context, direct detail links and return navigation.
It checks the shared PdfCraft dispatch, readable Rust source, and actual audio playback through
the visible Play/Pause controls. The same workflow passed at 1440 px and 390 px: **2 tests,
1.4 min** in the final run retaining image files (desktop 25.4 s, 390 px 57.9 s).

`web/e2e/paperless-isolation-live.spec.ts` uses the existing admin onboarding and sign-in helpers
to create a unique `example.com` identity with the normal fixed zero starting credit. Both
identities upload their own synthetic PDF and save their own view. Both owner paths return the
document, indexed passages, a real image thumbnail and the saved view. In both directions the
other identity receives 404 for document detail/content/thumbnail and view PUT/DELETE; its view
list and document search omit the foreign objects. Rechecking the owners proves the denied
mutations did not change or remove their views. The live isolation run passed: **1 test, 1.5 min**.

Authentication reuses `auth.ts` and the existing private helper cache. Traces, videos and automatic
screenshots are disabled for these live specs. Credentials and session material are absent from
this report and test output. Browser screenshots explicitly attached by the workflow show only
the run's synthetic filtered document or media detail.

## Query/upload mutation probes

Eight sequential controlled faults were applied to the actual Task 3 query/upload implementation,
each restored byte-for-byte in a `finally` block. The final result is **8/8 killed (100%)**:

| Fault | Covering suite |
|---|---|
| Default content search changed to filename search | `documentRoute.test.ts` |
| Default Documents category changed to All | `documentRoute.test.ts` |
| External return destination admitted | `documentRoute.test.ts` |
| Detail document ID no longer URL-escaped | `documentRoute.test.ts` |
| Upload index lookup restricted to Documents | `useDocumentUploads.test.tsx` |
| Uploaded source key inferred from local filename | `useDocumentUploads.test.tsx` |
| Upload session storage shared between identities | `useDocumentUploads.test.tsx` |
| Waiting upload marked ready without an indexed source | `useDocumentUploads.test.tsx` |

The first default-category probe survived: the existing route test asserted default search mode
but omitted the category. The added assertion checks omitted, empty and invalid categories,
plus explicit All/audio/video/image/code. The fault then failed as expected; unchanged production
source passed the two covering suites (**8 tests**). No coverage exclusions, thresholds or
existing assertions were relaxed. These are targeted fault probes, not a global Stryker score.

## Cleanup and limits

Cleanup deletes only exact source keys recorded by each run's uniquely named uploads, its saved
views and its type label. The identity test closes its separate context and removes the exact
created account through the normal admin removal saga. No real document, existing reference
fixture, chat turn, spending cap or mailbox is modified. Source removal is asynchronous in the
index; the object deletion itself is checked for HTTP 200. One early selector-development run
hit its overall timeout and closed the page before its `finally` could read the queue. Its exact
run UUID was recovered from the test-owned document names; a separate authenticated context
deleted only those three files, one view and one label, all HTTP 200. After the final run, a
read-only query for the acceptance filename prefix returned **zero rows**.

Initial test development corrected fixture sequencing and selectors, without changing the
application: reloading before later uploads finished canceled those in-flight files; saved-view
rows expose an “Open view” action; global and detail navigation both have a “Documents” button;
the custom audio player's media element is intentionally hidden. The final assertions exercise
the existing user controls and actual stored/indexed state.

Related live evidence covers the broader native Office/editor/media scenarios without repeating
all of those manual fixtures here:
[workspace](2026-10-22-documents-workspace-live.md),
[detail](2026-10-22-documents-detail-live.md), and
[code/media ingestion](2026-10-22-code-media-ingestion.md).

## Frontend gates

Full coverage ran once with the unchanged configured exclusions and all four 85% thresholds:

```sh
cd web
node node_modules/vitest/vitest.mjs run --coverage --maxWorkers=2
```

**380 files / 3,264 tests passed**, exit 0, 435.53 s.

| Metric | Exact result |
|---|---|
| Statements | 13,724/14,875 = 92.26% |
| Branches | 10,587/12,201 = 86.77% |
| Functions | 4,025/4,423 = 91.00% |
| Lines | 12,379/13,140 = 94.20% |

`npm run lint`, `npm run typecheck`, `npm run format:check`, `npm run dup`, `npm run deadcode`,
`npm run lint:contract`, `npm run mutation:contract` and `npm run contrast` passed. Duplication
reported zero clones; the lint contract ran eight real probes; contrast passed 102/102 required
WCAG AA token pairs. The mutation contract checks runner name filtering, not mutation strength.
Final typechecking caught an optional `baseURL` type in the new fresh-context test; the explicit
required-live-origin guard now passes typechecking. No production frontend source changed,
so this task did not rewrite the embedded production bundle.

The full unchanged global Stryker configuration completed once in **71 min 24 s**. Its initial
892-test dry run passed. It generated **6,344 mutants**, with **5,130 killed**, **20 timeouts**,
**1,064 survivors**, **127 without coverage** and **3 runtime errors**. Stryker's final score is
**81.22%**, above the unchanged 70% break threshold. The JSON report includes 69 files with
mutants; instrumentation reported 70 files. This measures the configured global mutation scope,
not every frontend file, and does not imply that every mutant was detected. The retained report
is `web/reports/mutation/mutation.json`; the run log is the private workspace's
`task-5-web-stryker.log`. The targeted Task 3 probes above and Task 4's separate 8/8 probes cover
the newly introduced workflow and persistence logic beyond that configured global scope.

## Screenshot comparison

The final run retained `1440-library.png`, `390-library.png`, `1440-audio-preview.png` and
`390-audio-preview.png` below `output/playwright/paperless-acceptance-e2e/`, in the corresponding
viewport's Playwright test directory. The desktop library, 390 px library and 390 px audio detail
images were opened and visually inspected alongside captured Paperless images
`output/playwright/paperless-comparison/07-paperless-content-search.png` and
`18-paperless-mobile-library.png`.

Both products expose archive search, organization, saved views and a document result without
requiring a folder-first workflow. Aura retains its global app navigation and branding, puts
secondary mobile filters in a disclosure, and uses a single-column compact result on the phone;
Paperless's captured mobile result uses two thumbnail columns. Aura's phone result wraps the
long synthetic filename and retains review/date/type; its detail separates metadata/content/
history/preview and provides the working media controls. The measured document width remained
within the viewport at 390 px. This is behavior/layout evidence, not a pixel-parity claim or an
invented usability score. Existing native-editor evidence establishes actual Office saves;
this test's PDF assertion establishes native dispatch/frame visibility rather than every editor
operation.

The separately measured database coverage baseline reconciliation is documented in
[coverage tiers](2026-10-22-document-coverage-tiers.md). Backend/native release gates,
whole-change review, merge, push, CI and GHCR publication are owned by the coordinating agent
and remain outside this report's completion claim.

## Final-review and user follow-up

The whole-branch review through `ed760a1f5` found one source issue: an initial successful null
ingestion response was not recorded, so the first external Files import could leave a fresh
identity's cached empty library unchanged. The user also requested Delete directly beside
pinned saved views. The consolidated source correction is `564aa3bc4`; it reuses the existing
saved-view API, mutation invalidation and confirmation UI, with native DialogTrigger focus
return and nested-drawer keyboard handling.

The [first-import evidence](2026-10-22-paperless-documents-first-import.md) records a real new
identity's Files upload appearing in its mounted library without navigation or an upload queue.
The [saved-view evidence](2026-10-22-paperless-saved-view-delete.md) records both 1440 px and
390 px passing: named 44 px controls, Cancel/Escape focus return, exactly one real target
deletion, preserved sibling/original views, unchanged search and document total. The final
focused regression run passed 27 tests in six files. Real keyboard failures were fixed in
production while retaining their assertions.

Those linked records separately account for the subsequent full frontend coverage run and the
coordinator's final incremental mutation verification. The earlier full measurements above
retain their original source scope. Publication and published-image deployment remain separate
release work until their exact pushed revision is verified.

The scoped source review of `4d3ceb1c0..564aa3bc4` approved the fix with no remaining
Critical, Important or Minor findings. The subsequent full frontend run passed **381 files /
3,274 tests**, exit 0, 398.20 seconds, on the frozen consolidated production tree:

| Metric | Final consolidated result |
|---|---|
| Statements | 13,730/14,883 = 92.25% |
| Branches | 10,606/12,223 = 86.77% |
| Functions | 4,025/4,424 = 90.98% |
| Lines | 12,384/13,146 = 94.20% |

All four unchanged 85% thresholds passed. One intervening coverage run was stopped as
incomplete when a real live keyboard failure required a production correction; it is not
counted as passing evidence. The final default incremental Stryker verification uses the
existing cache and remains a separate recorded result.
