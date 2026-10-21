# Documents workspace live checks

Date: 2026-10-22. First frontend build tested through the existing GHCR Aura container with
the temporary source binary (`52cb5dbaf+dirty`). These are implementation checks, not a claim
that the final revision has been published. The root agent owns the shared browser.

## Confirmed in the first build

- Direct `/documents/overview` loaded the new sidebar and real counts. Desktop library used
  the available space and did not reserve the token/cost footer.
- Content search for Cedar found the synthetic contract, whereas the separate filename API
  check found none. Switching to cards retained the search and used the actual native PNG.
- Saving `UX study — Cedar`, renaming it to `UX study — Cedar archive`, pinning it and enabling
  its Overview entry survived a full reload. The saved query contains server query parameters;
  cards is stored separately as its display mode.
- At 390 x 844, the page width and document scroll width were both 390 px. Names wrapped in
  compact rows and the runtime telemetry was absent.
- Two different PDFs named `UX-study-same-name.pdf` were selected together through Upload files.
  They received distinct item UUID folders, document IDs and source hashes. Both became Indexed.
  Reload retained both ready links and issued zero upload POST requests.
- One upload response was deliberately intercepted with HTTP 503. The UI showed Upload failed,
  the supplied error and Retry. After removing the interception, Retry wrote the JSON fixture
  through the real server with the same item UUID and changed to Stored / waiting for indexing.
  This failure simulation is distinct from the successful real upload and ingestion checks.
- The retried JSON file became one indexed Code row under the same UUID folder. The queue
  persisted only its metadata, without storing file bytes in browser storage.
- The Italian light-theme video library displayed translated controls and loaded real native
  thumbnails for the visible cards. No untranslated document resource keys appeared. This
  screenshot was also opened and visually inspected.

## Findings sent to the implementer

- The hidden upload proxy was exposed as a second accessible Upload files button.
- Mobile advanced filters, sorting and display occupied about 340 px and scrolled independently;
  display controls were clipped and only about two rows remained visible. Keep the main search
  and file-type controls visible and collapse secondary controls. Two identical menu icons also
  needed a clearer distinction.
- A saved-view action was labeled Open document instead of Open view.
- Thumbnail URLs lacked the file revision, and a failure remained sticky across an indexed
  source change. Refresh by source hash/indexed revision and reset the failed state for it.
- Processing led with raw pipeline status, a large component count and an ISO timestamp.
  Primary status should use human-readable localized wording, with raw counters in a disclosure.
- The outer shell landmark still announced Chat while displaying Documents; it needs the
  corresponding workspace name.

The implementer received the screenshots and measurements; no assertion was relaxed to accept
these findings. The next build was embedded and checked as described below.

Screenshots: `output/playwright/paperless-comparison/aura-new-overview-desktop.png`,
`aura-new-library-desktop.png`, `aura-new-cedar-card.png`, `aura-new-code-mobile.png`.
The first three were opened and visually inspected; the mobile screenshot established the
filter-layout finding above. Full detail/editor acceptance belongs to the next task.
Italian/light screenshot: `output/playwright/paperless-comparison/aura-new-video-desktop-it-light.png`.

## Targeted second-build recheck

The production frontend now committed as `b237f0c13` was embedded in the temporary runtime.
At 390 x 844 in Italian/light, the compact list displayed four complete rows, with secondary
controls in a named Filters popover. Document width remained 390 px. The revised mobile
screenshot `aura-new-code-mobile-fixed.png` was opened and inspected.

There was one accessible Upload action and its proxy input was hidden. Document navigation used
a distinct Files icon; the outer landmark announced Documents. Processing showed a localized
state, reported issue count and local time, with raw component counts in a closed disclosure.
The synthetic PDF loaded from a thumbnail URL containing its actual source SHA. Opening its
detail and using browser Back restored search, file type, filename mode and card display.
The final freshly navigated library reported zero console errors or warnings.

One minor sizing discrepancy remained: `min-h-11` resolved to 42.625 px because the actual root
font was 15.5 px. This was measured after all animations ended, not inferred from class names.
Independent review approved functional scope and quality with this sole minor correction.
Fix `46ac9bcdb` replaced the rem-based targets with literal 44 px values. Scoped re-review found
no remaining issues. The fixed production bundle was embedded and measured live: all 13 controls
in the mobile Filters popover had both a 44 px rendered height and 44 px computed minimum height;
document width stayed 390 px. No new test was written merely to assert the CSS spelling.

## Reviewed merge and fresh verification

Task commits `b237f0c13`, `46ac9bcdb` and evidence commit `4b8fc580e` were fast-forward merged
into master. Fresh post-merge Vitest covered Documents, routed-surface blocking, AppShell and
the file-manager API: **126 tests passed in 30 files**, 19.55 seconds. The source/runtime fixes
are approved with no remaining review findings. Full frontend coverage and final publication
remain in the final acceptance task, after the shared detail/editor changes.
