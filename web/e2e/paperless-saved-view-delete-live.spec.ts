import { randomUUID } from 'node:crypto';
import { expect, test } from '@playwright/test';
import type { SavedView } from '../src/documents/workspaceApi';
import { api } from './documentsLive';
import { openPaperless } from './paperlessFixtures';

test.use({ trace: 'off', video: 'off', screenshot: 'off' });

for (const width of [1440, 390]) {
  test(`pinned saved views can be cancelled or deleted from the ${String(width)}px navigation`, async ({
    page,
  }, testInfo) => {
    test.skip(
      process.env.AURA_E2E_LIVE_DOCUMENTS !== '1',
      'requires an explicitly selected live stack',
    );
    test.setTimeout(120_000);
    await page.setViewportSize({ width, height: 900 });
    await openPaperless(page);
    const originalViews = (await api<{ views: SavedView[] }>(page, 'GET', '/api/documents/views'))
      .views;
    const originalTotal = (
      await api<{ total: number }>(page, 'GET', '/api/documents?file_type=all&limit=1')
    ).total;
    const token = randomUUID();
    const created: SavedView[] = [];
    const deletes: string[] = [];
    try {
      for (const label of ['Delete', 'Keep']) {
        const { view } = await api<{ view: SavedView }>(page, 'POST', '/api/documents/views', {
          name: `${label} ${token}`,
          version: 1,
          query: `file_type=all&search_mode=name&q=${token}`,
          display: 'cards',
          pinned: true,
          dashboard: true,
        });
        created.push(view);
      }
      const [target, neighbor] = created;
      if (!target || !neighbor) throw new Error('Synthetic views were not created');
      page.on('request', (request) => {
        if (request.method() === 'DELETE') deletes.push(new URL(request.url()).pathname);
      });
      await page.goto(`/documents/library?file_type=all&search_mode=name&q=${token}&display=cards`);
      if (width < 1024)
        await page.getByRole('button', { name: 'Document navigation', exact: true }).click();
      const nav = page.getByRole('navigation', { name: 'Document navigation', exact: true });
      const action = nav.getByRole('button', { name: `Delete: ${target.name}`, exact: true });
      await expect(action).toBeVisible();
      const size = await action.boundingBox();
      expect(size?.width).toBeGreaterThanOrEqual(44);
      expect(size?.height).toBeGreaterThanOrEqual(44);
      const url = page.url();
      await page.screenshot({ path: testInfo.outputPath('pinned-view-delete-action.png') });
      await action.click();
      const confirmation = page.getByRole('dialog', {
        name: 'Delete this saved view?',
        exact: true,
      });
      await expect(confirmation.getByText(target.name, { exact: true })).toBeVisible();
      const cancel = confirmation.getByRole('button', { name: 'Cancel', exact: true });
      await expect(cancel).toBeFocused();
      await page.keyboard.press('Tab');
      await expect(
        confirmation.getByRole('button', { name: 'Confirm', exact: true }),
      ).toBeFocused();
      await page.keyboard.press('Tab');
      await expect(cancel).toBeFocused();
      await page.screenshot({ path: testInfo.outputPath('named-delete-confirmation.png') });
      await cancel.click();
      await expect(confirmation).toBeHidden();
      await expect(action).toBeFocused();
      expect(deletes).toEqual([]);
      await expect(nav.getByRole('link', { name: target.name, exact: true })).toBeVisible();
      await action.click();
      await expect(confirmation).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(confirmation).toBeHidden();
      await expect(action).toBeFocused();
      expect(deletes).toEqual([]);
      await action.click();
      const deleted = page.waitForResponse(
        (response) =>
          response.request().method() === 'DELETE' &&
          new URL(response.url()).pathname === `/api/documents/views/${target.id}`,
      );
      await confirmation.getByRole('button', { name: 'Confirm', exact: true }).click();
      expect((await deleted).status()).toBe(200);
      await expect(nav.getByRole('link', { name: target.name, exact: true })).toHaveCount(0);
      await expect(nav.getByRole('link', { name: neighbor.name, exact: true })).toBeVisible();
      expect(deletes).toEqual([`/api/documents/views/${target.id}`]);
      expect(page.url()).toBe(url);
      const afterViews = (await api<{ views: SavedView[] }>(page, 'GET', '/api/documents/views'))
        .views;
      expect(afterViews.map((view) => view.id)).not.toContain(target.id);
      expect(afterViews.map((view) => view.id)).toContain(neighbor.id);
      for (const view of originalViews) expect(afterViews).toContainEqual(view);
      expect(
        (await api<{ total: number }>(page, 'GET', '/api/documents?file_type=all&limit=1')).total,
      ).toBe(originalTotal);
      expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
        width,
      );
      await page.screenshot({ path: testInfo.outputPath('sidebar-after-delete.png') });
      if (width < 1024) {
        await page.keyboard.press('Escape');
        await expect(nav).toBeHidden();
      }
    } finally {
      const remaining = (await api<{ views: SavedView[] }>(page, 'GET', '/api/documents/views'))
        .views;
      for (const view of created) {
        if (remaining.some((entry) => entry.id === view.id))
          await api(page, 'DELETE', `/api/documents/views/${view.id}`);
      }
    }
  });
}
