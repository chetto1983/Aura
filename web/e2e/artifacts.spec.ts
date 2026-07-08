import { expect, test, type Page, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// artifacts.spec.ts proves the WEBART-08 end-to-end surface: a turn that emits an
// `aura.artifact` CUSTOM descriptor (a delivered send_file carrying an `asset_id`)
// makes the deliverable appear in the "Artefatti" panel — which AUTO-OPENS on the
// first delivery of a thread (37B-07 one-time auto-open) — and the row's download
// control issues a request to the id-addressed authenticated route GET
// /api/assets/{id}/download. Runs on chromium (desktop ResizablePanel) + mobile-chrome
// (right Drawer) so the panel holds on a touch viewport, not just desktop.
//
// Golden-replay, NOT a live agent turn (plan prohibition): the `aura.artifact` frame
// and the /api/assets list are BOTH mocked at the page-network layer (mirroring
// replay.spec.ts `sseFromFrames` + assets.spec.ts fixtures), so the spec exercises
// the REAL in-browser sseAdapter onArtifact pump + useThreadArtifacts refetch + the
// panel render/download wiring without any backend agent loop.
//
// No-skip-as-green (CLAUDE.md): every test asserts a COUNTED number of DOM/route
// facts (domAssertions / downloadHits) and guards `> 0`, so a no-op run FAILS rather
// than passing green. The auth + serve harness itself is the same live gate the
// sibling e2e specs use (playwright.config webServer → `aura serve`); when neither a
// live serve nor the auth stack is reachable the shared auth helper THROWS.

const CONV_ID = '77777777-7777-7777-7777-777777777777';

// Two accepted, agent-delivered assets on the thread. The panel projects them
// newest-first over the server's created_at ASC list (selectAgentArtifacts), so the
// NEWER `report.xlsx` must render before the OLDER `notes.txt`.
const NEWEST_ID = 'asset-report-xlsx';
const OLDEST_ID = 'asset-notes-txt';

function agentAssets(): unknown[] {
  return [
    {
      id: OLDEST_ID,
      source_kind: 'agent',
      thread_id: CONV_ID,
      scope: 'thread',
      status: 'accepted',
      modality: 'document',
      file_name: 'notes.txt',
      mime_type: 'text/plain',
      declared_size_bytes: 128,
      size_bytes: 128,
      created_at: '2026-07-09T10:00:00.000Z',
      updated_at: '2026-07-09T10:00:00.000Z',
    },
    {
      id: NEWEST_ID,
      source_kind: 'agent',
      thread_id: CONV_ID,
      scope: 'thread',
      status: 'accepted',
      modality: 'document',
      file_name: 'report.xlsx',
      mime_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      declared_size_bytes: 4096,
      size_bytes: 4096,
      created_at: '2026-07-09T11:30:00.000Z',
      updated_at: '2026-07-09T11:30:00.000Z',
    },
  ];
}

// The delivery turn: a short assistant text then the `aura.artifact` CUSTOM
// descriptor carrying the newest asset's id. The descriptor MUST carry
// `tool_call_id` + `filename` (isArtifactDescriptor) for the onArtifact pump to fire
// (which triggers the panel refetch + one-time auto-open).
function deliveryTurnFrames(): Record<string, unknown>[] {
  return [
    { type: 'RUN_STARTED', threadId: CONV_ID, runId: 'run-1' },
    { type: 'TEXT_MESSAGE_START', messageId: 'msg-1' },
    { type: 'TEXT_MESSAGE_CONTENT', messageId: 'msg-1', delta: 'Here is your report.' },
    { type: 'TEXT_MESSAGE_END', messageId: 'msg-1' },
    {
      type: 'CUSTOM',
      name: 'aura.artifact',
      value: {
        tool_call_id: 'call-1',
        filename: 'report.xlsx',
        asset_id: NEWEST_ID,
        size_bytes: 4096,
        mime_type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      },
    },
    { type: 'RUN_FINISHED', threadId: CONV_ID, runId: 'run-1', outcome: { type: 'success' } },
  ];
}

function sseFromFrames(frames: readonly Record<string, unknown>[]): string {
  return frames.map((f) => `event: ${String(f.type)}\ndata: ${JSON.stringify(f)}\n\n`).join('');
}

function sseResponse(route: Route, body: string) {
  return route.fulfill({
    status: 200,
    headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' },
    body,
  });
}

async function installConversationRoutes(page: Page) {
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes(`/api/conversations/${CONV_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: CONV_ID,
          title: 'Artifacts thread',
          status: 'active',
          total_input_tokens: 0,
          total_output_tokens: 0,
          total_cached_tokens: 0,
          total_cost_usd: 0,
        }),
      });
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
  });
  await page.route('**/api/conversations/*/rot-events', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/api/approvals', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages: [] }),
    }),
  );
}

test.describe('cockpit artifacts — delivery surfaces in the Artefatti panel + download (WEBART-08)', () => {
  test('an aura.artifact delivery auto-opens the panel, lists newest-first, and downloads via the id route', async ({
    page,
  }) => {
    let domAssertions = 0;
    let downloadHits = 0;
    let downloadedPath = '';

    await installConversationRoutes(page);

    // The identity-scoped list: GET /api/assets?thread_id=… → the two agent assets.
    await page.route(/\/api\/assets\?thread_id=/, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(agentAssets()),
      }),
    );

    // The id-addressed authenticated download route. Fulfilled as an attachment so the
    // <a download> click resolves to a real download; the interceptor RECORDS the hit
    // (and the exact path) so we can assert only the opaque id ever reached the wire.
    await page.route(/\/api\/assets\/[^/]+\/download(\?|$)/, (route) => {
      downloadHits += 1;
      downloadedPath = new URL(route.request().url()).pathname;
      return route.fulfill({
        status: 200,
        headers: {
          'Content-Type': 'application/octet-stream',
          'Content-Disposition': 'attachment; filename="report.xlsx"',
        },
        body: 'xlsx-bytes',
      });
    });

    // The live delivery turn streams the aura.artifact descriptor.
    await page.route('**/agent/run', (route) =>
      sseResponse(route, sseFromFrames(deliveryTurnFrames())),
    );

    // 1) Open the thread and run a turn that delivers an artifact.
    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await composer.fill('ship the file');
    await composer.press('Enter');

    // 2) AUTO-OPEN: the Artefatti panel opens on the first delivery of the thread and
    // lists the delivered asset (report.xlsx). The panel exists on both surfaces
    // (desktop ResizablePanel / mobile Drawer) — getByRole('heading') matches either.
    await expect(page.getByRole('heading', { name: 'Artifacts' })).toBeVisible({ timeout: 15000 });
    domAssertions += 1;

    const reportName = page.getByText('report.xlsx', { exact: true });
    await expect(reportName).toBeVisible({ timeout: 10000 });
    await expect(page.getByText('notes.txt', { exact: true })).toBeVisible();
    domAssertions += 1;

    // 2a) NEWEST-FIRST: report.xlsx (newer created_at) precedes notes.txt in the list.
    const listItems = page.getByRole('listitem').filter({ hasText: /report\.xlsx|notes\.txt/ });
    await expect(listItems.first()).toContainText('report.xlsx');
    await expect(listItems.nth(1)).toContainText('notes.txt');
    domAssertions += 1;

    // 3) DOWNLOAD: clicking the row's download control issues GET /api/assets/{id}/download.
    // The <a download> click yields a download event; assert BOTH the browser download
    // filename and the route-interceptor hit + the exact id-only path (T-37B-16: no
    // object_key / bucket / host path ever leaves — only the opaque id).
    const downloadLink = page.getByRole('link', { name: 'Download report.xlsx' });
    await expect(downloadLink).toHaveAttribute('href', `/api/assets/${NEWEST_ID}/download`);
    domAssertions += 1;

    const [download] = await Promise.all([page.waitForEvent('download'), downloadLink.click()]);
    expect(download.suggestedFilename()).toBe('report.xlsx');
    expect(downloadHits).toBeGreaterThan(0);
    expect(downloadedPath).toBe(`/api/assets/${NEWEST_ID}/download`);
    // Negative surface: the addressed path carries ONLY the opaque id — never an
    // object_key, bucket, or host/container path segment.
    expect(downloadedPath).not.toMatch(/bucket|object_key|garage|\/root\/|\/home\//i);

    // COUNTED-ASSERTION GUARD: a no-op (panel never opened / row never rendered) run
    // FAILS rather than passing green.
    expect(domAssertions).toBeGreaterThanOrEqual(4);
  });

  test('a 404 on the download route leaves no unauthenticated surface (negative)', async ({
    page,
  }) => {
    let downloadHits = 0;

    await installConversationRoutes(page);
    await page.route(/\/api\/assets\?thread_id=/, (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(agentAssets()),
      }),
    );
    // A non-owner / missing asset: the route 404s. The row must still address the asset
    // purely by its opaque id (no fallback to a raw host path or object key).
    await page.route(/\/api\/assets\/[^/]+\/download(\?|$)/, (route) => {
      downloadHits += 1;
      return route.fulfill({ status: 404, contentType: 'application/json', body: '{}' });
    });
    await page.route('**/agent/run', (route) =>
      sseResponse(route, sseFromFrames(deliveryTurnFrames())),
    );

    await gotoAuthenticated(page, `/c/${CONV_ID}`);
    const composer = page.getByPlaceholder('Ask Aura');
    await composer.fill('ship the file');
    await composer.press('Enter');

    await expect(page.getByRole('heading', { name: 'Artifacts' })).toBeVisible({ timeout: 15000 });
    const downloadLink = page.getByRole('link', { name: 'Download report.xlsx' });
    await expect(downloadLink).toHaveAttribute('href', `/api/assets/${NEWEST_ID}/download`);

    // Clicking a 404 download hits the id route and surfaces nothing more — the DOM
    // never exposes an alternative (unauthenticated / raw-path) download target.
    await downloadLink.click().catch(() => undefined);
    await expect.poll(() => downloadHits, { timeout: 10000 }).toBeGreaterThan(0);
    const hrefs = await page
      .getByRole('link')
      .evaluateAll((links) =>
        (links as HTMLAnchorElement[]).map((a) => a.getAttribute('href') ?? ''),
      );
    for (const href of hrefs) {
      expect(href).not.toMatch(/bucket|object_key|garage|\/root\/|\/home\//i);
    }
  });
});
