import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  test as base,
  expect,
  type Download,
  type Page,
  type Request,
  type Response,
} from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { emulateIOSHomeScreen, sharedFiles } from './iosHomeScreen';

// Several files at once, against the REAL bucket, on a desktop and on a phone.
//
// Measured on the lab VM before this spec existed (2026-09-28, SVAR 2.6.0): a desktop could
// Ctrl-click and Shift-click several files, but pressing a card's ⋮ collapsed the selection to
// that one card while offering the menu for several -- its Delete listed one file -- and that
// menu had no Download. A phone could not select a second file at all: every tap replaced the
// selection. So each test here makes a selection of two on the pointer the project has, opens
// the menu through a card's ⋮ (the only menu a phone can reach), and asserts what reached the
// wire.

interface Scratch {
  /** An absolute folder id holding alpha.txt, bravo.txt, charlie.txt and sub/keep.txt. */
  readonly folder: string;
}

// Each test gets its own folder, and the folder is deleted when the test ends -- as a fixture,
// not a `finally`, so a test that times out still leaves the operator's bucket as it found it.
const test = base.extend<{ scratch: Scratch }>({
  scratch: [
    async ({ page }, use, testInfo) => {
      await gotoAuthenticated(page, '/');
      const folder = `/e2e-multi-${Date.now().toString(36)}-${testInfo.project.name}`;
      const seeded = await page.evaluate(async (root) => {
        const put = async (dir: string, name: string) => {
          const form = new FormData();
          form.append('file', new File([`e2e ${name}`], name, { type: 'text/plain' }));
          form.append('name', name);
          const res = await fetch(`/api/filemanager/upload?id=${encodeURIComponent(dir)}`, {
            method: 'POST',
            body: form,
          });
          return res.status;
        };
        return [
          await put(root, 'alpha.txt'),
          await put(root, 'bravo.txt'),
          await put(root, 'charlie.txt'),
          await put(`${root}/sub`, 'keep.txt'),
        ];
      }, folder);
      expect(seeded, 'the scratch files were not uploaded').toEqual([200, 200, 200, 200]);

      await use({ folder });

      const removed = await page.evaluate(
        async (ids) => {
          const res = await fetch('/api/filemanager/files', {
            method: 'DELETE',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ ids }),
          });
          return res.status;
        },
        [folder],
      );
      expect(removed, `the scratch folder ${folder} was left behind`).toBe(200);
    },
    { timeout: 60_000 },
  ],
});

interface Write {
  readonly method: string;
  readonly contentType: string | undefined;
  readonly body: { operation?: string; ids?: string[]; target?: string };
}

function recordWrites(page: Page): Write[] {
  const seen: Write[] = [];
  page.on('request', (request: Request) => {
    const url = new URL(request.url());
    if (url.pathname !== '/api/filemanager/files' || request.method() === 'GET') return;
    seen.push({
      method: request.method(),
      contentType: request.headers()['content-type'],
      body: request.postDataJSON() as Write['body'],
    });
  });
  return seen;
}

// data-id=":body" is the widget's own marker for the file panel (see file-manager.spec.ts on
// why the sidebar tree's copy of each folder must not be the one pressed).
function panel(page: Page) {
  return page.locator('[data-id=":body"]').first();
}

function card(page: Page, name: string) {
  return panel(page).getByText(name, { exact: true });
}

function more(page: Page, id: string) {
  return panel(page).locator(`[data-action-id=":${id}"]`);
}

function selected(page: Page) {
  return panel(page).locator('.wx-item.wx-selected');
}

async function choose(page: Page, label: string): Promise<void> {
  // The label, not the hotkey hint printed beside it.
  await page.locator('[data-wx-menu="true"] .wx-value').getByText(label, { exact: true }).click();
}

async function openScratch(page: Page, folder: string, projectName: string): Promise<void> {
  const nav =
    projectName === 'chrome'
      ? page.getByRole('navigation', { name: /Primary|Principale/ })
      : page.getByRole('navigation', { name: /Modes|Modalit/ });
  await nav.getByRole('button', { name: /Documents|Documenti/ }).click();
  await card(page, folder.slice(1)).dblclick();
  // `sub` is the one entry no test moves or deletes.
  await expect(card(page, 'sub')).toBeVisible();
}

// A refusal names its reason: the status alone says a write failed, the body says why.
async function expectAccepted(response: Response, verb: string): Promise<void> {
  const reason = response.ok() ? '' : await response.text();
  expect(response.status(), `the ${verb} was refused by the server: ${reason}`).toBe(200);
}

// The pointer each project has: Ctrl-click on a desktop, the card menu's Select and plain taps
// on a phone. Then the menu is opened through the second card's ⋮ -- the path that used to
// collapse the selection -- and the selection must still be both.
async function selectTwoAndOpenMenu(
  page: Page,
  folder: string,
  projectName: string,
  names: readonly [string, string],
): Promise<void> {
  const [first, second] = names;
  if (projectName === 'chrome') {
    await card(page, first).click();
    await card(page, second).click({ modifiers: ['ControlOrMeta'] });
  } else {
    await more(page, `${folder}/${first}`).click();
    await choose(page, 'Select');
    await expect(selected(page)).toHaveCount(1);
    await card(page, second).click();
  }
  await expect(selected(page)).toHaveCount(2);
  await test.info().attach('two-selected', {
    body: await page.screenshot(),
    contentType: 'image/png',
  });

  await more(page, `${folder}/${second}`).click();
  await expect(page.locator('[data-wx-menu="true"]')).toBeVisible();
  await expect(selected(page)).toHaveCount(2);
  await test.info().attach('menu-for-several', {
    body: await page.screenshot(),
    contentType: 'image/png',
  });
}

test('two selected files are deleted together, in one request', async ({
  page,
  scratch,
}, testInfo) => {
  const { folder } = scratch;
  const writes = recordWrites(page);
  await openScratch(page, folder, testInfo.project.name);
  await selectTwoAndOpenMenu(page, folder, testInfo.project.name, ['alpha.txt', 'bravo.txt']);

  await choose(page, 'Delete');
  const confirm = page.getByText('Are you sure you want to delete these items:');
  await expect(confirm).toBeVisible();
  await expect(page.getByText(`${folder}/alpha.txt`, { exact: true })).toBeVisible();
  await expect(page.getByText(`${folder}/bravo.txt`, { exact: true })).toBeVisible();
  await testInfo.attach('delete-confirm', {
    body: await page.screenshot(),
    contentType: 'image/png',
  });

  const deleted = page.waitForResponse(
    (res) =>
      new URL(res.url()).pathname === '/api/filemanager/files' &&
      res.request().method() === 'DELETE',
  );
  await page.getByRole('button', { name: 'OK' }).click();
  await expectAccepted(await deleted, 'delete');

  const del = writes.filter((write) => write.method === 'DELETE');
  expect(del, 'several files must go in ONE request').toHaveLength(1);
  expect(del[0]?.contentType).toContain('application/json');
  expect(del[0]?.body.ids).toEqual([`${folder}/alpha.txt`, `${folder}/bravo.txt`]);

  // A reload asks the bucket, not the rows the widget removed before the answer came back.
  await page.reload({ waitUntil: 'domcontentloaded' });
  await openScratch(page, folder, testInfo.project.name);
  await expect(card(page, 'alpha.txt')).toHaveCount(0);
  await expect(card(page, 'bravo.txt')).toHaveCount(0);
});

test('two selected files are moved together into a folder', async ({ page, scratch }, testInfo) => {
  const { folder } = scratch;
  const writes = recordWrites(page);
  await openScratch(page, folder, testInfo.project.name);
  await selectTwoAndOpenMenu(page, folder, testInfo.project.name, ['alpha.txt', 'charlie.txt']);
  await choose(page, 'Cut');

  await card(page, 'sub').dblclick();
  await expect(card(page, 'keep.txt')).toBeVisible();
  const moved = page.waitForResponse(
    (res) =>
      new URL(res.url()).pathname === '/api/filemanager/files' && res.request().method() === 'PUT',
  );
  // Paste from a file's own ⋮ lands in the folder being viewed; it is reachable on both.
  await more(page, `${folder}/sub/keep.txt`).click();
  await choose(page, 'Paste');
  await expectAccepted(await moved, 'move');

  const put = writes.filter((write) => write.method === 'PUT');
  expect(put, 'several files must go in ONE request').toHaveLength(1);
  expect(put[0]?.body).toEqual({
    operation: 'move',
    ids: [`${folder}/alpha.txt`, `${folder}/charlie.txt`],
    target: `${folder}/sub`,
  });

  await page.reload({ waitUntil: 'domcontentloaded' });
  await openScratch(page, folder, testInfo.project.name);
  await expect(card(page, 'alpha.txt')).toHaveCount(0);
  await card(page, 'sub').dblclick();
  await expect(card(page, 'alpha.txt')).toBeVisible();
  await expect(card(page, 'charlie.txt')).toBeVisible();
  await testInfo.attach('moved-into-sub', {
    body: await page.screenshot(),
    contentType: 'image/png',
  });
});

test('two selected files are both downloaded from the menu for several', async ({
  page,
  scratch,
}, testInfo) => {
  const { folder } = scratch;
  const downloads: Download[] = [];
  page.on('download', (download) => downloads.push(download));
  await openScratch(page, folder, testInfo.project.name);
  await selectTwoAndOpenMenu(page, folder, testInfo.project.name, ['alpha.txt', 'charlie.txt']);

  await choose(page, 'Download');

  await expect.poll(() => downloads.length, { timeout: 10_000 }).toBe(2);
  const names = downloads.map((download) => download.suggestedFilename()).sort();
  expect(names).toEqual(['alpha.txt', 'charlie.txt']);
  for (const download of downloads) {
    expect(await download.failure(), `${download.suggestedFilename()} did not arrive`).toBeNull();
  }
});

// iOS's home-screen app: the two files go to ONE share sheet, read from the live route, and no
// download starts -- each would navigate the app to iOS's file page (prd.md §3). A tap whose
// activation lapsed while the files were fetched leaves a Save on the bar for a second one.
test('two selected files go to one share sheet in the iOS home-screen app', async ({
  page,
  scratch,
}, testInfo) => {
  const { folder } = scratch;
  await emulateIOSHomeScreen(page);
  await page.reload();
  const downloads: Download[] = [];
  page.on('download', (download) => downloads.push(download));
  await openScratch(page, folder, testInfo.project.name);
  await selectTwoAndOpenMenu(page, folder, testInfo.project.name, ['alpha.txt', 'charlie.txt']);

  await choose(page, 'Download');
  const save = page.getByRole('button', { name: 'Save' });
  await expect
    .poll(async () => (await sharedFiles(page)).length > 0 || (await save.count()) > 0)
    .toBe(true);
  if ((await sharedFiles(page)).length === 0) await save.click();

  await expect
    .poll(async () => (await sharedFiles(page)).map(({ name, size }) => ({ name, size })))
    .toEqual([
      { name: 'alpha.txt', size: 'e2e alpha.txt'.length },
      { name: 'charlie.txt', size: 'e2e charlie.txt'.length },
    ]);
  expect(downloads).toEqual([]);
});

// The direct route answers a byte range from the live store: iOS plays no <video> from a server
// that does not, and a clip opened from the Documents page streams from this route.
test('a clip in the Documents bucket is served by byte range', async ({ page, scratch }) => {
  const clip = readFileSync(resolve(process.cwd(), 'e2e/fixtures/media-edit/clip.mp4'));
  const answer = await page.evaluate(
    async ({ dir, base64 }) => {
      const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
      const form = new FormData();
      form.append('file', new File([bytes], 'clip.mp4', { type: 'video/mp4' }));
      form.append('name', 'clip.mp4');
      const upload = await fetch(`/api/filemanager/upload?id=${encodeURIComponent(dir)}`, {
        method: 'POST',
        body: form,
      });
      const res = await fetch(
        `/api/filemanager/direct?id=${encodeURIComponent(`${dir}/clip.mp4`)}`,
        {
          headers: { Range: 'bytes=0-99' },
        },
      );
      return {
        upload: upload.status,
        status: res.status,
        range: res.headers.get('content-range'),
        type: res.headers.get('content-type'),
        length: (await res.arrayBuffer()).byteLength,
      };
    },
    { dir: scratch.folder, base64: clip.toString('base64') },
  );
  expect(answer).toEqual({
    upload: 200,
    status: 206,
    range: `bytes 0-99/${String(clip.length)}`,
    type: 'video/mp4',
    length: 100,
  });
});
