import { test as base, type Page } from '@playwright/test';

// assetCleanup.ts — these specs also run against the lab VM, signed in as the operator. Every
// asset a run presigns is recorded and deleted when the test ends, so a run leaves the operator's
// library the way it found it.
//
// It is a fixture, not a `finally` in each test body: when a test times out Playwright tears the
// page down, and a `finally` that reaches for it then throws and deletes nothing. A fixture that
// depends on `page` is torn down BEFORE `page`, with a timeout of its own.

interface CreatedAssets {
  ids(): readonly string[];
  /** Waits for every presign answer seen so far; answers how many could not be read. */
  settled(): Promise<number>;
}

/** Records the id of every asset this page presigns, whoever presigned it (test or editor). */
function trackCreatedAssets(page: Page): CreatedAssets {
  const seen = new Set<string>();
  const reading: Promise<void>[] = [];
  page.on('response', (response) => {
    if (!response.url().includes('/api/assets/presign') || !response.ok()) return;
    reading.push(
      response.json().then((body: { asset?: { id?: unknown } }) => {
        if (typeof body.asset?.id !== 'string') throw new Error('presign answered no asset id');
        seen.add(body.asset.id);
      }),
    );
  });
  return {
    ids: () => [...seen],
    settled: async () =>
      (await Promise.allSettled(reading)).filter(({ status }) => status === 'rejected').length,
  };
}

/** Deletes the assets and answers the ones the route would not delete. A 404 is already gone. */
async function deleteAssets(page: Page, ids: readonly string[]): Promise<readonly string[]> {
  return page.evaluate(async (all) => {
    const refused = await Promise.all(
      all.map(async (id) => {
        const res = await fetch(`/api/assets/${id}`, { method: 'DELETE' });
        return res.ok || res.status === 404 ? undefined : `${id}: HTTP ${String(res.status)}`;
      }),
    );
    return refused.filter((line) => line !== undefined);
  }, ids);
}

export const test = base.extend<{ createdAssets: CreatedAssets }>({
  createdAssets: [
    async ({ page }, use) => {
      const created = trackCreatedAssets(page);
      await use(created);
      const unread = await created.settled();
      const refused = await deleteAssets(page, created.ids());
      const left = [
        ...refused,
        ...(unread > 0 ? [`${String(unread)} presign answers unread`] : []),
      ];
      if (left.length > 0) throw new Error(`the run left assets behind: ${left.join('; ')}`);
    },
    { auto: true, timeout: 60_000 },
  ],
});

export { expect } from '@playwright/test';

export interface AssetRow {
  readonly status: string;
  readonly modality: string;
  readonly object_key: string;
  readonly summary: string;
  readonly document_id?: string;
}

export async function readAsset(page: Page, id: string): Promise<AssetRow> {
  return page.evaluate(async (assetId) => {
    const res = await fetch(`/api/assets/${assetId}`);
    if (!res.ok) throw new Error(`asset ${assetId}: HTTP ${String(res.status)}`);
    return (await res.json()) as AssetRow;
  }, id);
}
