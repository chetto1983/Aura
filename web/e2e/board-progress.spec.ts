import { randomUUID } from 'node:crypto';
import { expect, test, type APIRequestContext, type Page } from '@playwright/test';
import { browserCookieHeader, gotoAuthenticated } from './auth';

const ownCards = new WeakMap<Page, { api: APIRequestContext; id?: string }>();

test.afterEach(async ({ page }) => {
  const owned = ownCards.get(page);
  if (owned === undefined) return;
  try {
    if (owned.id !== undefined) {
      const deleted = await owned.api.delete(`/api/board/cards/${encodeURIComponent(owned.id)}`, {
        headers: { 'Idempotency-Key': randomUUID() },
        timeout: 10_000,
      });
      expect(deleted.ok()).toBe(true);
    }
  } finally {
    ownCards.delete(page);
    await owned.api.dispose();
  }
});

async function openBoard(page: Page) {
  await page.getByRole('button', { name: /^(Board|Bacheca)$/ }).click();
  await expect(page.getByRole('region', { name: /^(Kanban board|Bacheca Kanban)$/ })).toBeVisible();
}

test('card progress is saved, reloaded, reset and completed through the real API', async ({
  page,
  playwright,
}, info) => {
  test.setTimeout(60_000);
  await gotoAuthenticated(page, '/');
  await openBoard(page);
  // Keep teardown independent of the page: a timed-out browser must not hide the original
  // failure or leave its card behind when the fixture closes the context.
  const owned: { api: APIRequestContext; id?: string } = {
    api: await playwright.request.newContext({
      baseURL: new URL(page.url()).origin,
      storageState: await page.context().storageState(),
      ignoreHTTPSErrors: true,
      extraHTTPHeaders: await browserCookieHeader(
        page,
        new URL('/api/board/cards/', page.url()).href,
      ),
    }),
  };
  ownCards.set(page, owned);
  const added = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/board/cards') && response.request().method() === 'POST',
    { timeout: 10_000 },
  );
  await page
    .getByRole('button', { name: /^(Add card to|Aggiungi scheda a) / })
    .first()
    .click();
  const created = await added;
  expect(created.ok()).toBe(true);
  const { id, label: initialLabel } = (await created.json()) as { id: string; label: string };
  owned.id = id;
  const label = `e2e progress ${info.project.name} ${String(Date.now())}`;
  const editor = page.locator('.aura-board-editor');
  const title = editor.getByRole('textbox', { name: /^(Title|Titolo)/ });
  await expect(title).toHaveValue(initialLabel);
  await title.fill(label);
  const slider = editor.getByRole('slider', { name: /^(Progress|Avanzamento) · \d+%$/ });
  await slider.press('Home');
  await expect(slider).toHaveValue('0');
  const bounds = await slider.boundingBox();
  if (bounds === null) throw new Error('The progress slider has no layout.');
  await slider.click({ position: { x: bounds.width * 0.55, y: bounds.height / 2 } });
  // Native thumb geometry may round the click to a neighboring step; adjust the observed
  // value rather than sending a long, timing-dependent burst of keyboard events.
  await expect
    .poll(
      async () => {
        const value = Number(await slider.inputValue());
        if (value !== 0.55) await slider.press(value < 0.55 ? 'ArrowRight' : 'ArrowLeft');
        return Number(await slider.inputValue());
      },
      { timeout: 15_000, intervals: [100] },
    )
    .toBe(0.55);
  await slider.press('Tab');
  await expect(slider).toHaveValue('0.55');
  await expect(slider).toHaveAccessibleName(/ · 55%$/);
  const saved = page.waitForResponse(
    (response) =>
      response.url().endsWith(`/api/board/cards/${id}`) && response.request().method() === 'PUT',
    { timeout: 10_000 },
  );
  await editor.getByRole('button', { name: /^(Save|Salva)$/ }).click();
  const save = await saved;
  expect(save.ok()).toBe(true);
  expect(save.request().postDataJSON()).toMatchObject({ progress: 0.55 });

  await page.reload({ waitUntil: 'domcontentloaded' });
  await openBoard(page);
  await expect(
    page.getByRole('button', { name: label, exact: true }).getByText('55%', { exact: true }),
  ).toBeVisible();
  for (const [key, ratio, percent, previous] of [
    ['Home', 0, '0%', 0.55],
    ['End', 1, '100%', 0],
  ] as const) {
    await page.getByRole('button', { name: label, exact: true }).click();
    const progress = editor.getByRole('slider', { name: /^(Progress|Avanzamento) · \d+%$/ });
    await expect(title).toHaveValue(label);
    await expect(progress).toHaveValue(String(previous));
    await progress.press(key);
    await expect(progress).toHaveValue(String(ratio));
    await expect(progress).toHaveAccessibleName(new RegExp(` · ${percent}$`));
    await progress.press('Tab');
    const updated = page.waitForResponse(
      (response) =>
        response.url().endsWith(`/api/board/cards/${id}`) && response.request().method() === 'PUT',
      { timeout: 10_000 },
    );
    await editor.getByRole('button', { name: /^(Save|Salva)$/ }).click();
    expect((await updated).ok()).toBe(true);
    const stored = await page.evaluate(async (cardID) => {
      const response = await fetch('/api/board/cards');
      const cards = (await response.json()) as { id: string; progress: number }[];
      return cards.find((card) => card.id === cardID)?.progress;
    }, id);
    expect(stored).toBe(ratio);
    await expect(
      page.getByRole('button', { name: label, exact: true }).getByText(percent, { exact: true }),
    ).toBeVisible();
  }
});
