import { randomUUID } from 'node:crypto';
import { expect, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { api, type LiveRow } from './documentsLive';
import { sameOriginFetch } from './live';

export const indexingTimeout = 180_000;

export function fixturePDF(text: string): Buffer {
  const stream = `BT /F1 12 Tf 20 100 Td (${text}) Tj ET\n`;
  const objects = [
    '<</Type/Catalog/Pages 2 0 R>>',
    '<</Type/Pages/Kids[3 0 R]/Count 1>>',
    '<</Type/Page/Parent 2 0 R/MediaBox[0 0 600 200]/Resources<</Font<</F1 4 0 R>>>>/Contents 5 0 R>>',
    '<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>',
    `<</Length ${String(stream.length)}>>\nstream\n${stream}endstream`,
  ];
  let pdf = '%PDF-1.4\n';
  const offsets = [0];
  objects.forEach((object, i) => {
    offsets.push(Buffer.byteLength(pdf));
    pdf += `${String(i + 1)} 0 obj\n${object}\nendobj\n`;
  });
  const xref = Buffer.byteLength(pdf);
  pdf += 'xref\n0 6\n0000000000 65535 f \n';
  pdf += offsets
    .slice(1)
    .map((offset) => `${String(offset).padStart(10, '0')} 00000 n \n`)
    .join('');
  pdf += `trailer\n<</Root 1 0 R/Size 6>>\nstartxref\n${String(xref)}\n%%EOF\n`;
  return Buffer.from(pdf);
}

export function fixtureWAV(): Buffer {
  const rate = 16_000;
  const wav = Buffer.alloc(44 + rate * 2);
  wav.write('RIFF', 0);
  wav.writeUInt32LE(wav.length - 8, 4);
  wav.write('WAVEfmt ', 8);
  wav.writeUInt32LE(16, 16);
  wav.writeUInt16LE(1, 20);
  wav.writeUInt16LE(1, 22);
  wav.writeUInt32LE(rate, 24);
  wav.writeUInt32LE(rate * 2, 28);
  wav.writeUInt16LE(2, 32);
  wav.writeUInt16LE(16, 34);
  wav.write('data', 36);
  wav.writeUInt32LE(rate * 2, 40);
  // Each run gets distinct bytes even when the index deduplicates identical files.
  const frequency = 300 + parseInt(randomUUID().slice(0, 4), 16) / 100;
  for (let i = 0; i < rate; i++) {
    wav.writeInt16LE(Math.round(Math.sin((i * frequency * 2 * Math.PI) / rate) * 3000), 44 + i * 2);
  }
  return wav;
}

export async function openPaperless(page: Page): Promise<void> {
  await page.addInitScript(() => {
    localStorage.setItem('aura.language', 'en');
  });
  await gotoAuthenticated(page, '/documents/processing');
  await expect(page.getByRole('button', { name: 'Upload files', exact: true })).toBeVisible();
}

export interface Uploaded extends LiveRow {
  readonly file_type: string;
}

export async function indexed(page: Page, fileName: string): Promise<Uploaded> {
  let found: Uploaded | undefined;
  await expect
    .poll(
      async () => {
        const result = await api<{ rows: Uploaded[] }>(
          page,
          'GET',
          `/api/documents?file_type=all&search_mode=name&q=${encodeURIComponent(fileName)}`,
        );
        found = result.rows.find((row) => row.file_name === fileName);
        return found !== undefined;
      },
      { timeout: indexingTimeout, intervals: [2000] },
    )
    .toBe(true);
  if (!found) throw new Error(`Not indexed: ${fileName}`);
  return found;
}

export async function cleanFiles(page: Page, sources: readonly string[]): Promise<void> {
  if (sources.length === 0) return;
  const result = await sameOriginFetch(page, '/api/filemanager/files', {
    method: 'DELETE',
    body: JSON.stringify({ ids: sources.map((source) => `/${source}`) }),
  });
  expect(result.status, 'delete only the exact synthetic source keys').toBe(200);
}

export async function uploadSources(page: Page, names: readonly string[]): Promise<string[]> {
  return page.evaluate(
    (wanted) => {
      const sources: string[] = [];
      for (const key of Object.keys(sessionStorage)) {
        if (!key.startsWith('aura.documents.uploads.v1.')) continue;
        const items = JSON.parse(sessionStorage.getItem(key) ?? '[]') as {
          name: string;
          sourceKey?: string;
        }[];
        sources.push(
          ...items.flatMap((item) =>
            wanted.includes(item.name) && item.sourceKey ? [item.sourceKey] : [],
          ),
        );
      }
      return sources;
    },
    [...names],
  );
}
