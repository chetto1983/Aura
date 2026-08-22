import type { Page } from '@playwright/test';

// The chat lane GETs the stored compaction on every thread open (useCompaction). A fixture
// that leaves the route unstubbed lets that request reach the real `aura serve`, which has no
// session for a fixture browser context (auth is faked by stubbing /api/me) and answers 401 —
// or 404 for a conversation id that only exists in the fixture. Either lands in
// browserHealth's failed-request list and fails a test that never asked about compaction.
//
// The fixture answer is "this thread has never been compacted", which is what every thread
// fixture means: no marker, no watermark.

/** Stubs GET /api/conversations/:id/compaction as an uncompacted thread. */
export async function stubNoCompaction(page: Page): Promise<void> {
  await page.route('**/api/conversations/*/compaction', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ covers_through_seq: 0, source_turns: 0, summary: '' }),
    }),
  );
}
