import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// A chat attachment used to reach the index as "<assetID>.pdf".
//
// That is not a display bug. The object key deliberately omits the filename -- a key travels
// into presigned URLs and access logs, so "Contratto riservato.pdf" there would leak the
// subject -- and nothing else carried it, so the ingest sweep derived a name from the key.
// The derived name then went into the searchable name column, which means the operator could
// not find their own document by the name they gave it.
//
// The name now rides in S3 user metadata, percent-encoded because that metadata is an HTTP
// header and therefore US-ASCII. This spec proves the browser half: the presign declares the
// header and the upload carries it. The bytes in Garage and the row in ArcadeDB are checked
// outside the browser, where the ground truth actually lives.
test.beforeEach(() => {
  test.skip(test.info().project.name !== 'chrome', 'one upload path, proven once on desktop');
});

test('a chat attachment carries its real name to the object store', async ({ page }) => {
  // Accented on purpose: this is the case S3 metadata cannot carry raw, measured against the
  // running Garage on 2026-08-13, and the case an Italian operator produces by default.
  const fileName = 'Perizia città di Ghèdi 2026.txt';
  const marker = `GHEDI-${Date.now().toString(36)}`;
  const dir = mkdtempSync(join(tmpdir(), 'aura-attach-'));
  const path = join(dir, fileName);
  writeFileSync(path, `Perizia tecnica. Marcatore univoco: ${marker}.\n`, 'utf8');

  await gotoAuthenticated(page, '/');
  await expect(page.getByRole('textbox', { name: /Ask Aura/ })).toBeVisible({ timeout: 30_000 });

  const presigned = page.waitForResponse(
    (res) =>
      new URL(res.url()).pathname === '/api/assets/presign' && res.request().method() === 'POST',
    { timeout: 30_000 },
  );
  // The PUT goes straight to Garage, so it is captured as a request rather than a response:
  // what matters is the header the browser actually put on the wire.
  const uploaded = page.waitForRequest(
    (req) => req.method() === 'PUT' && req.headers()['x-amz-meta-filename'] !== undefined,
    { timeout: 60_000 },
  );

  // A CORS refusal never becomes an HTTP response, so it has to be watched for as a failed
  // request. It is not hypothetical: the identity buckets had no CORS rule at all, and this
  // is the signal that says so instead of leaving a mute 240s timeout on the chip below.
  const blocked: string[] = [];
  page.on('requestfailed', (req) => {
    if (req.method() === 'PUT') blocked.push(req.failure()?.errorText ?? 'unknown');
  });

  await page.locator('input[type="file"][aria-label="Add files"]').setInputFiles(path);

  const presignBody = (await (await presigned).json()) as {
    readonly asset?: { readonly id?: string; readonly object_key?: string };
    readonly upload?: { readonly required_headers?: Record<string, string> };
  };
  const declared = presignBody.upload?.required_headers?.['x-amz-meta-filename'];
  expect(declared, 'presign declared no filename header').toBeTruthy();
  expect(decodeURIComponent(declared ?? '')).toBe(fileName);

  const sent = (await uploaded).headers()['x-amz-meta-filename'];
  expect(decodeURIComponent(sent ?? ''), 'the browser did not send the signed name').toBe(fileName);

  // The upload is only real if the store accepted it: the header is SIGNED, so a mismatch
  // would be rejected outright rather than producing a nameless object. 'Failed' is asserted
  // against explicitly because that is the state the CORS gap produced, and a bare wait for
  // 'Ready' would have spent its whole timeout staring at it.
  const chip = page.getByRole('button', { name: `Remove ${fileName}` }).locator('..');
  await expect(chip.getByText('Failed', { exact: true })).toHaveCount(0);
  expect(blocked, 'the upload PUT was blocked before it reached the store').toEqual([]);
  await expect(chip.getByText(/Ready|Processing/)).toBeVisible({ timeout: 180_000 });

  // Handed to the out-of-browser checks, which read Garage and ArcadeDB directly.
  console.log(`ATTACHMENT_ASSET_ID ${presignBody.asset?.id ?? ''}`);
  console.log(`ATTACHMENT_MARKER ${marker}`);
  console.log(`ATTACHMENT_NAME ${fileName}`);
});
