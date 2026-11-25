import { expect, test, type Page, type TestInfo } from '@playwright/test';
import { api, documentsNamed, ensureLabel, openDocuments, type LiveRow } from './documentsLive';

// The Documenti view on the live stack (Task 13 step 3 of
// docs/superpowers/plans/2026-10-18-document-fields.md): the list, a filter, a bulk edit with its
// confirmation, and a review with "Rivisto, salva e avanti", at a desktop and a phone width, with
// a screenshot of every view attached. It runs against the dev stack's library, whose generated
// invoices fattura_2026_0323, _0386 and _0267 a rule of the run's own picks out. Everything the
// run writes is taken back at its end.
//
//   AURA_E2E_ORIGIN=https://localhost AURA_E2E_LIVE_DOCUMENTS=1 \
//     npx playwright test e2e/documents-live.spec.ts --project=chrome

const live = process.env.AURA_E2E_LIVE_DOCUMENTS === '1';

const INVOICES = [
  'fattura_2026_0323.xlsx',
  'fattura_2026_0386.xlsx',
  'fattura_2026_0267.xlsx',
] as const;

const VIEWPORTS = [
  { name: 'desktop', viewport: { width: 1440, height: 900 }, isMobile: false },
  { name: 'mobile', viewport: { width: 390, height: 844 }, isMobile: true },
] as const;

async function shot(page: Page, testInfo: TestInfo, name: string): Promise<void> {
  const path = testInfo.outputPath(`${name}.png`);
  await page.screenshot({ path, animations: 'disabled' });
  await testInfo.attach(name, { path, contentType: 'image/png' });
}

/** The labels a run of this spec names, so a run that died before its clean-up is taken back by
 * the next one. */
const RUN_LABEL = /^(Verifica |e2e-)(desktop|mobile)-[0-9a-z]+$/;

async function staleLabels(page: Page): Promise<Record<string, string>[]> {
  const out: Record<string, string>[] = [];
  for (const [kind, param] of [
    ['type', 'type'],
    ['tag', 'tags'],
  ] as const) {
    const { labels } = await api<{ labels: readonly { id: string; name: string }[] }>(
      page,
      'GET',
      `/api/documents/labels?kind=${kind}`,
    );
    for (const label of labels.filter((l) => RUN_LABEL.test(l.name))) {
      out.push({ [param]: label.id });
    }
  }
  return out;
}

/** takeBack removes every value the run's labels left on a document, then the labels. */
async function takeBack(page: Page, labels: Readonly<Record<string, string>>): Promise<void> {
  for (const [param, id] of Object.entries(labels)) {
    const { rows } = await api<{ rows: readonly LiveRow[] }>(
      page,
      'GET',
      `/api/documents?${param}=${encodeURIComponent(id)}&limit=50`,
    );
    for (const row of rows) {
      const remove = row.values
        .filter((v) => v.value_key === id)
        .map((v) => ({ field: v.field, value_key: v.value_key }));
      await api(page, 'PATCH', `/api/documents/${encodeURIComponent(row.id)}`, {
        set: [],
        remove,
      });
    }
    await api(page, 'DELETE', `/api/documents/labels/${encodeURIComponent(id)}`);
  }
}

test.describe('the Documenti view, live', () => {
  test.skip(
    !live,
    'set AURA_E2E_LIVE_DOCUMENTS=1 against a rebuilt Aura holding the bench library',
  );
  test.describe.configure({ mode: 'serial' });

  for (const device of VIEWPORTS) {
    test.describe(device.name, () => {
      test.use({
        viewport: device.viewport,
        isMobile: device.isMobile,
        hasTouch: device.isMobile,
        actionTimeout: 20_000,
      });

      test('list, filter, bulk edit and review, as the operator', async ({ page }, testInfo) => {
        test.setTimeout(5 * 60_000);
        const run = `${device.name}-${Date.now().toString(36)}`;
        const typeName = `Verifica ${run}`;
        const tagName = `e2e-${run}`;
        await openDocuments(page);
        for (const stale of await staleLabels(page)) await takeBack(page, stale);
        const labels: Record<string, string> = {};
        try {
          labels.type = await ensureLabel(page, 'type', typeName, {
            match_algorithm: 'regex',
            match_pattern: 'fattura_2026_0(323|386|267)\\.xlsx',
          });
          labels.tags = await ensureLabel(page, 'tag', tagName);
          await page.reload();

          // The list is a region named Documenti inside the Documents surface, which carries the
          // same name.
          const list = page
            .getByRole('region', { name: 'Documenti' })
            .getByRole('region', { name: 'Documenti' });
          await expect(list.getByRole('heading', { name: 'Documenti' })).toBeVisible();
          if (device.isMobile) {
            // A phone lists documents as cards, not as the grid.
            await expect(list.getByRole('listitem').first()).toBeVisible();
          } else {
            await expect(list.locator('.wx-row').first()).toBeVisible();
            // The grid scrolls inside the room left to it, rather than growing past the page and
            // taking the rows below the fold and the pager with it.
            const fits = await list.evaluate((region) => {
              const grid = region.querySelector('.wx-grid');
              return (
                grid !== null &&
                grid.getBoundingClientRect().bottom <= region.getBoundingClientRect().bottom + 1
              );
            });
            expect(fits, 'the grid stays inside the view').toBe(true);
            const nameWidth = await list
              .locator('.wx-header .wx-cell', { hasText: 'Nome' })
              .evaluate((cell) => cell.getBoundingClientRect().width);
            expect(nameWidth, 'the name column keeps a readable width').toBeGreaterThanOrEqual(150);
            // The operator drags a column's edge, and the list keeps the width it was left at.
            const header = list.locator('.wx-header .wx-cell', { hasText: 'Nome' });
            const grip = await header.locator('.wx-grip').boundingBox();
            if (grip === null) throw new Error('the name column has no resize grip');
            await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2);
            await page.mouse.down();
            await page.mouse.move(grip.x - 150, grip.y + grip.height / 2, { steps: 10 });
            await page.mouse.up();
            const dragged = await header.evaluate((cell) => cell.getBoundingClientRect().width);
            expect(dragged, 'the dragged column narrows').toBeLessThan(nameWidth - 100);
            const kept = await page.evaluate(() => localStorage.getItem('aura.documents.widths'));
            expect(kept, 'the width is remembered').toContain('file_name');
          }
          await shot(page, testInfo, `${device.name}-01-elenco`);

          // On a phone the filters sit behind one button, in a popover outside the list region.
          if (device.isMobile)
            await list.getByRole('button', { name: 'Filtri', exact: true }).click();
          await page.getByRole('button', { name: 'Tipo', exact: true }).click();
          const menu = page.getByRole('dialog', { name: 'Tipo' });
          await menu.getByRole('checkbox', { name: new RegExp(typeName) }).click();
          for (const name of INVOICES) {
            await expect(list.getByRole('button', { name, exact: true })).toBeVisible();
          }
          await shot(page, testInfo, `${device.name}-02-filtro-tipo`);
          await page.keyboard.press('Escape');
          if (device.isMobile) {
            // Escape closes only the Tipo menu; the Filtri popover stays open over the cards'
            // checkboxes (CI, 2026-11-25) and a second Escape does not close it, so its button
            // closes it, as a tap would.
            const filters = page.getByRole('dialog', { name: 'Filtri' });
            if (await filters.isVisible()) {
              await list.getByRole('button', { name: 'Filtri', exact: true }).click();
            }
            await expect(filters).toBeHidden();
          }
          await shot(page, testInfo, `${device.name}-03-filtrato`);

          // "Rivisto, salva e avanti" opens the next document in the list's order, the indexing
          // order, which nothing fixes: the reviewed invoice is the first shown, so it has a next
          // one, and the other two take the bulk edit.
          const placed = await Promise.all(
            INVOICES.map(async (name) => {
              const box = await list.getByRole('button', { name, exact: true }).boundingBox();
              return { name, y: box?.y ?? Number.POSITIVE_INFINITY };
            }),
          );
          const [reviewedName, ...bulkNames] = placed.sort((a, b) => a.y - b.y).map((p) => p.name);
          if (reviewedName === undefined) throw new Error('no invoice in the filtered list');

          for (const name of bulkNames) {
            await list.getByRole('checkbox', { name: `Seleziona ${name}` }).click();
          }
          const bar = page.getByRole('toolbar', { name: 'Modifica in blocco' });
          await bar.getByRole('button', { name: 'Tag', exact: true }).click();
          const tags = page.getByRole('dialog', { name: 'Tag, sui 2 selezionati' });
          await tags.getByRole('button', { name: new RegExp(tagName) }).click();
          await shot(page, testInfo, `${device.name}-04-modifica-in-blocco`);
          // On a phone the open menu covers the bar's last row, where Applica sits: the menu
          // closes first, as a tap outside it would.
          await page.keyboard.press('Escape');
          await bar.getByRole('button', { name: 'Applica' }).click();
          const confirm = page.getByRole('dialog', { name: 'Confermi la modifica?' });
          await expect(confirm).toContainText('2 documenti');
          await shot(page, testInfo, `${device.name}-05-conferma`);
          await confirm.getByRole('button', { name: 'Conferma' }).click();
          await expect(bar).toBeHidden();
          for (const name of bulkNames) {
            const [doc] = await documentsNamed(page, name);
            expect(doc?.values.some((v) => v.value_key === labels.tags)).toBe(true);
          }
          await shot(page, testInfo, `${device.name}-06-applicato`);

          await list.getByRole('button', { name: reviewedName, exact: true }).click();
          await expect(page.getByRole('heading', { name: reviewedName })).toBeVisible();
          const use = page.getByRole('button', { name: `Usa il suggerimento ${typeName}` });
          await expect(use).toBeVisible();
          // The preview is GridCraft (/apps/gridcraft/): its own loader leaves once the app
          // runs, and the workbook it is sent then paints on its canvas, which the DOM cannot
          // see, so a short wait follows.
          await expect(
            page.frameLocator(`iframe[title="${reviewedName}"]`).locator('#gridcraft_loading'),
          ).toBeHidden({ timeout: 60_000 });
          await page.waitForTimeout(1_500);
          await shot(page, testInfo, `${device.name}-07-dettaglio`);
          await use.click();
          await expect(
            page.getByRole('button', { name: 'Salva dettagli', exact: true }),
          ).toBeEnabled();
          await shot(page, testInfo, `${device.name}-08-dettaglio-modificato`);
          // The detail is the section holding its review button: the shell's own navigation
          // carries a "Documenti" too.
          const detail = page
            .locator('section')
            .filter({ has: page.getByRole('button', { name: 'Rivisto, salva e avanti' }) });
          await detail.getByRole('button', { name: 'Rivisto, salva e avanti' }).click();
          await expect(page.getByRole('heading', { name: reviewedName })).toBeHidden();
          await expect(detail.getByRole('heading', { level: 1 })).toBeVisible();
          const [reviewed] = await documentsNamed(page, reviewedName);
          expect(reviewed?.needs_review).toBe(false);
          expect(reviewed?.values.some((v) => v.value_key === labels.type && v.accepted)).toBe(
            true,
          );
          await shot(page, testInfo, `${device.name}-09-avanti`);

          await detail.getByRole('button', { name: 'Documenti', exact: true }).click();
          // On a phone the Documents navigation is a drawer behind its own button.
          if (device.isMobile)
            await page.getByRole('button', { name: 'Navigazione documenti' }).click();
          await page.getByRole('link', { name: 'Organizzazione' }).click();
          await expect(page.getByRole('heading', { name: 'Liste e regole' })).toBeVisible();
          await expect(page.getByRole('heading', { name: typeName, level: 3 })).toBeVisible();
          await expect(page.getByText(/^\d+ document[oi]$/).first()).toBeVisible();
          await shot(page, testInfo, `${device.name}-10-liste`);
          await page.getByRole('button', { name: `Modifica ${typeName}` }).click();
          await expect(page.getByRole('form', { name: `Modifica ${typeName}` })).toBeVisible();
          await shot(page, testInfo, `${device.name}-11-regola`);
          await page.getByRole('button', { name: 'Annulla' }).click();
          await page.getByRole('tab', { name: 'Campi personalizzati' }).click();
          await shot(page, testInfo, `${device.name}-12-campi`);
        } finally {
          await takeBack(page, labels);
        }
      });
    });
  }
});
