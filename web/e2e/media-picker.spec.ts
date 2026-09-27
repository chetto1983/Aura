import { resolve } from 'node:path';
import { gotoAuthenticated } from './auth';
import { expect, test } from './support/assetCleanup';
import { uploadAsset } from './support/assetUpload';
import { FIXTURES, openStudioWith } from './support/videoStudio';

// media-picker.spec.ts — a library file opened from the Studio's Garage picker is edited AS its
// asset. It used to be uploaded again on every open, and every copy was itself a library file
// the picker offered next time: the operator's library filled with twins of one clip.

test('a library clip opened twice from the Garage picker is never uploaded again', async ({
  page,
}, info) => {
  test.setTimeout(4 * 60_000);
  await gotoAuthenticated(page, '/');
  const name = `picker-${info.project.name}-${String(Date.now())}.mp4`;
  const clip = await uploadAsset(page, resolve(FIXTURES, 'clip-a.mp4'), name, 'video/mp4', {
    use: 'media',
  });
  await openStudioWith(page, clip, 'garage picker check');

  let presigned = 0;
  page.on('request', (request) => {
    if (request.url().includes('/api/assets/presign')) presigned += 1;
  });
  for (let open = 1; open <= 2; open += 1) {
    await page.getByRole('button', { name: 'Open Garage library' }).first().click();
    const listing = page
      .getByRole('dialog', { name: 'Choose from Garage' })
      .locator('[data-id=":body"]')
      .first();
    await listing.getByText('media', { exact: true }).dblclick();
    await listing.getByText(name, { exact: true }).dblclick();
    const editor = page.getByRole('dialog', { name: 'Video editor' });
    await expect(editor.getByRole('button', { name: 'Clip 1' })).toBeVisible({ timeout: 60_000 });
    await editor.getByRole('button', { name: 'Close the editor' }).click();
    await expect(editor).toHaveCount(0);
  }
  expect(presigned, 'uploads made by opening a library file').toBe(0);
});
