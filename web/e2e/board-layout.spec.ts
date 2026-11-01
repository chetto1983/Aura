import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

test('the desktop board fills its width and keeps its controls compact', async ({ page }, info) => {
  test.skip(info.project.name !== 'chrome', 'Desktop geometry; mobile uses the scrolling board.');
  await page.setViewportSize({ width: 1440, height: 960 });
  await gotoAuthenticated(page, '/');
  await page
    .getByRole('navigation', { name: /Primary|Principale/ })
    .getByRole('button', { name: /^(Board|Bacheca)$/ })
    .click();
  const board = page.getByRole('region', { name: /^(Kanban board|Bacheca Kanban)$/ });
  await expect(board).toBeVisible();
  const columns = board.locator('section.wx-column');
  await expect(columns).toHaveCount(3);
  await expect(columns.first()).toBeVisible();
  const bounds = await board.boundingBox();
  const last = await columns.last().boundingBox();
  if (bounds === null || last === null) throw new Error('Board columns have no layout.');
  expect(bounds.y).toBeLessThanOrEqual(185);
  expect(Math.abs(bounds.x + bounds.width - last.x - last.width)).toBeLessThanOrEqual(24);
});

test('the mobile card editor and its save action fit the viewport', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await gotoAuthenticated(page, '/');
  await page.getByRole('button', { name: /^(Board|Bacheca)$/ }).click();
  const board = page.getByRole('region', { name: /^(Kanban board|Bacheca Kanban)$/ });
  await expect(board).toBeVisible();
  const added = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/board/cards') && response.request().method() === 'POST',
  );
  await board
    .getByRole('button', { name: /^(Add card to|Aggiungi scheda a) / })
    .first()
    .click();
  const response = await added;
  expect(response.ok()).toBe(true);
  const card = (await response.json()) as { id: string };
  try {
    const editor = page.locator('.wx-editor-kanban');
    await expect(editor).toBeVisible();
    const bounds = await editor.boundingBox();
    if (bounds === null) throw new Error('The card editor has no layout.');
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(390);
    const save = editor.getByRole('button', { name: /^(Save|Salva)$/ });
    await expect(save).toBeInViewport({ ratio: 1 });
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
      390,
    );
  } finally {
    const removed = await page.evaluate(async (id) => {
      const result = await fetch(`/api/board/cards/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      });
      return result.ok;
    }, card.id);
    expect(removed).toBe(true);
  }
});
