import { test, expect } from './fixtures';

test('source inbox accepts text-like uploads and exposes extracted ingest actions', async ({ authedPage: page }) => {
  let uploadedName = '';
  let sources: unknown[] = [];

  await page.route('**/api/sources', async (route) => {
    if (route.request().method() !== 'GET') {
      await route.continue();
      return;
    }
    await route.fulfill({ json: sources });
  });

  await page.route('**/api/sources/upload', async (route) => {
    uploadedName = route.request().postDataBuffer()?.toString('utf8') ?? '';
    sources = [{
      id: 'src_aaaaaaaaaaaaaaaa',
      kind: 'text',
      filename: 'notes.txt',
      status: 'extract_complete',
      created_at: '2026-05-05T10:00:00Z',
      wiki_pages: [],
    }];
    await route.fulfill({
      json: {
        id: 'src_aaaaaaaaaaaaaaaa',
        status: 'extract_complete',
        duplicate: false,
        filename: 'notes.txt',
        note: 'extracted - ready for ingest',
      },
    });
  });

  await page.goto('/sources');
  sources = [{
    id: 'src_bbbbbbbbbbbbbbbb',
    kind: 'xlsx',
    filename: 'budget.xlsx',
    status: 'extract_complete',
    created_at: '2026-05-05T10:00:00Z',
    wiki_pages: [],
  }];
  await page.reload();
  const xlsxRow = page.getByRole('row').filter({ hasText: 'budget.xlsx' });
  await expect(xlsxRow.getByRole('button', { name: /^Ingest$/i })).toBeVisible();

  const input = page.locator('input[type="file"]');
  await expect(input).toHaveAttribute('accept', /\.txt/);
  await expect(input).toHaveAttribute('accept', /\.md/);
  await expect(input).toHaveAttribute('accept', /\.json/);
  await expect(input).toHaveAttribute('accept', /\.csv/);
  await expect(input).toHaveAttribute('accept', /\.xlsx/);
  await expect(input).toHaveAttribute('accept', /\.docx/);

  await input.setInputFiles({
    name: 'notes.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('Aura should remember text uploads.'),
  });

  await expect.poll(() => uploadedName).toContain('notes.txt');
  await expect(page.getByRole('cell', { name: 'notes.txt' })).toBeVisible();
  await expect(page.getByRole('heading', { name: /extracted|estratto/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Ingest$/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Download$/i })).toBeVisible();
});
