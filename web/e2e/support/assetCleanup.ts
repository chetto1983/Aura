import type { Page } from '@playwright/test';

// assetCleanup.ts — these specs also run against the lab VM, signed in as the operator. Every
// asset a run presigns is recorded here and deleted when the test ends, so a run leaves the
// operator's library the way it found it.

export interface CreatedAssets {
  ids(): readonly string[];
}

/** Records the id of every asset this page presigns, whoever presigned it (test or editor). */
export function trackCreatedAssets(page: Page): CreatedAssets {
  const seen = new Set<string>();
  page.on('response', (response) => {
    if (!response.url().includes('/api/assets/presign') || !response.ok()) return;
    void response
      .json()
      .then((body: { asset?: { id?: unknown } }) => {
        if (typeof body.asset?.id === 'string') seen.add(body.asset.id);
      })
      .catch(() => undefined);
  });
  return { ids: () => [...seen] };
}

export async function deleteAssets(page: Page, ids: readonly string[]): Promise<void> {
  await page.evaluate(async (all) => {
    await Promise.all(all.map((id) => fetch(`/api/assets/${id}`, { method: 'DELETE' })));
  }, ids);
}

export interface AssetRow {
  readonly status: string;
  readonly modality: string;
  readonly object_key: string;
  readonly summary: string;
}

export async function readAsset(page: Page, id: string): Promise<AssetRow> {
  return page.evaluate(async (assetId) => {
    const res = await fetch(`/api/assets/${assetId}`);
    if (!res.ok) throw new Error(`asset ${assetId}: HTTP ${String(res.status)}`);
    return (await res.json()) as AssetRow;
  }, id);
}
