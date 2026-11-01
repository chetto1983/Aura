import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

async function openBoard(page: Page) {
  await page.getByRole('button', { name: /^(Board|Bacheca)$/ }).click();
  await expect(page.getByRole('region', { name: /^(Kanban board|Bacheca Kanban)$/ })).toBeVisible();
}

test('card progress is saved, reloaded, reset and completed through the real API', async ({
  page,
}, info) => {
  test.setTimeout(60_000);
  await gotoAuthenticated(page, '/');
  await openBoard(page);
  const added = page.waitForResponse(
    (response) =>
      response.url().endsWith('/api/board/cards') && response.request().method() === 'POST',
  );
  await page
    .getByRole('button', { name: /^(Add card to|Aggiungi scheda a) / })
    .first()
    .click();
  const created = await added;
  expect(created.ok()).toBe(true);
  const { id } = (await created.json()) as { id: string };
  const label = `e2e progress ${info.project.name} ${String(Date.now())}`;
  try {
    const editor = page.locator('.aura-board-editor');
    await editor.getByRole('textbox', { name: /^(Title|Titolo)/ }).fill(label);
    const slider = editor.getByRole('slider', { name: /^(Progress|Avanzamento) · \d+%$/ });
    await slider.press('Home');
    await expect(slider).toHaveValue('0');
    // Wait for the native controlled input between keys: a rapid key count can lose an
    // increment on a busy mobile browser. The contract is its observed value and saved ratio.
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
    for (const [key, ratio, percent] of [
      ['Home', 0, '0%'],
      ['End', 1, '100%'],
    ] as const) {
      await page.getByRole('button', { name: label, exact: true }).click();
      const progress = editor.getByRole('slider', { name: /^(Progress|Avanzamento) · \d+%$/ });
      await progress.press(key);
      await expect(progress).toHaveValue(String(ratio));
      await expect(progress).toHaveAccessibleName(new RegExp(` · ${percent}$`));
      await progress.press('Tab');
      const updated = page.waitForResponse(
        (response) =>
          response.url().endsWith(`/api/board/cards/${id}`) &&
          response.request().method() === 'PUT',
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
  } finally {
    const deleted = await page.evaluate(async (cardID) => {
      const response = await fetch(`/api/board/cards/${encodeURIComponent(cardID)}`, {
        method: 'DELETE',
      });
      return response.ok;
    }, id);
    expect(deleted).toBe(true);
  }
});
