import { expect, test, type Page, type Request } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// The write half of the corpus browser, against the REAL bucket -- nothing is mocked here.
//
// file-manager.spec.ts mocks the listing and only reads, which is exactly why a write
// regression once shipped unnoticed: the listing kept working while create, rename, move,
// copy and delete all answered 400. The cause was a header. RestDataProvider.send overrides
// Rest.sendRequest and loses its "application/json" default, so the browser labelled every
// write text/plain, and the server refuses that on purpose -- a cross-origin form can post
// text/plain with no preflight but cannot post JSON, which is what makes the browser ask
// permission before a page the operator never opened can delete their files.
//
// So this spec asserts the header ON THE WIRE, not just that the button worked. A future
// provider upgrade that drops the label again fails here rather than in production.

// Desktop only, and not for convenience: on a narrow viewport SVAR parks the sidebar that
// HOSTS the "Add New" button off-canvas, so the create affordance is absent from the page
// rather than merely awkward to reach -- measured on the Pixel 5 profile against the live
// cockpit, where the toolbar renders only the tree toggle, search and the view modes.
// file-manager.spec.ts documents the same layout for the tree it descends. The writes
// themselves are viewport-independent (one provider, one set of handlers), so proving them
// on desktop proves the contract; what mobile lacks is the button, not the request.
test.beforeEach(() => {
  test.skip(
    test.info().project.name !== 'chrome',
    'the widget renders no Add New control on a narrow viewport',
  );
});

interface SeenWrite {
  method: string;
  contentType: string | undefined;
}

function recordWrites(page: Page): SeenWrite[] {
  const seen: SeenWrite[] = [];
  page.on('request', (request: Request) => {
    const url = new URL(request.url());
    if (!url.pathname.startsWith('/api/filemanager/files')) return;
    if (request.method() === 'GET') return;
    seen.push({ method: request.method(), contentType: request.headers()['content-type'] });
  });
  return seen;
}

function panelEntry(page: Page, name: string) {
  return page.locator('[data-id=":body"]').first().getByText(name, { exact: true });
}

async function openFiles(page: Page, projectName: string): Promise<void> {
  await gotoAuthenticated(page, '/');
  const nav =
    projectName === 'chrome'
      ? page.getByRole('navigation', { name: /Primary|Principale/ })
      : page.getByRole('navigation', { name: /Modes|Modalit/ });
  await nav.getByRole('button', { name: /Documents|Documenti/ }).click();
  await expect(page.getByRole('region', { name: /Documents|Documenti/ })).toBeVisible();
  // The root listing has to have landed before the toolbar will act on it.
  await expect(page.getByRole('button', { name: 'Add New' })).toBeVisible();
}

test('a folder created through the widget reaches the bucket, labelled as JSON', async ({
  page,
}, testInfo) => {
  const writes = recordWrites(page);
  await openFiles(page, testInfo.project.name);

  // Unique per run: the bucket is real and a leftover from an earlier run must not decide
  // whether this one passes.
  const folder = `e2e-write-${Date.now().toString(36)}`;

  const created = page.waitForResponse(
    (res) =>
      new URL(res.url()).pathname.startsWith('/api/filemanager/files') &&
      res.request().method() === 'POST',
  );
  await page.getByRole('button', { name: 'Add New' }).click();
  await page.getByText('Add new folder', { exact: true }).click();
  // fill(), not keyboard.type(): the dialog takes focus while it mounts, and typing into
  // it dropped a character mid-name on the first run -- the folder came out `e-write-…`
  // where `e2e-write-…` was asked for. The search box is the other text input on the page;
  // the dialog's field is the one the component leaves without a placeholder.
  await page.locator('input[type="text"][placeholder=""]').fill(folder);
  await page.getByRole('button', { name: 'OK' }).click();

  const response = await created;
  expect(response.status(), 'the create was refused by the server').toBe(200);
  await expect(panelEntry(page, folder)).toBeVisible();

  const post = writes.find((write) => write.method === 'POST');
  expect(post, 'no create request was issued').toBeDefined();
  expect(post?.contentType).toContain('application/json');

  // Clean up through the UI, which is also the DELETE leg of the same contract: the verb
  // has its own handler and its own body, so a header regression could hit it alone.
  const deleted = page.waitForResponse(
    (res) =>
      new URL(res.url()).pathname.startsWith('/api/filemanager/files') &&
      res.request().method() === 'DELETE',
  );
  await panelEntry(page, folder).click({ button: 'right' });
  // .first(): the menu row prints "Delete" twice -- once as the label, once as the keyboard
  // shortcut on the right -- so an exact-text match resolves to two nodes and clicks neither.
  await page.getByText('Delete', { exact: true }).first().click();
  await page.getByRole('button', { name: 'OK' }).click();

  const deleteResponse = await deleted;
  expect(deleteResponse.status(), 'the delete was refused by the server').toBe(200);
  const del = writes.find((write) => write.method === 'DELETE');
  expect(del?.contentType).toContain('application/json');

  await expect(panelEntry(page, folder)).toHaveCount(0);
});

// The other half of the claim. The test above would still pass against a server that had no
// gate at all, so this one relabels the write on the wire exactly the way the browser did
// before the provider was fixed -- text/plain, the label a cross-origin form can send -- and
// requires the DEPLOYED container to refuse it. Together they pin both sides: the client
// sends the label, and the server would not accept the write without it.
test('the deployed server refuses a write that is not labelled JSON', async ({
  page,
}, testInfo) => {
  await openFiles(page, testInfo.project.name);

  await page.route(
    (url) => url.pathname.startsWith('/api/filemanager/files'),
    async (route) => {
      if (route.request().method() === 'GET') return route.fallback();
      const headers = { ...route.request().headers(), 'content-type': 'text/plain;charset=UTF-8' };
      await route.continue({ headers });
    },
  );

  const refused = page.waitForResponse(
    (res) =>
      new URL(res.url()).pathname.startsWith('/api/filemanager/files') &&
      res.request().method() === 'POST',
  );
  const folder = `e2e-refused-${Date.now().toString(36)}`;
  await page.getByRole('button', { name: 'Add New' }).click();
  await page.getByText('Add new folder', { exact: true }).click();
  await page.locator('input[type="text"][placeholder=""]').fill(folder);
  await page.getByRole('button', { name: 'OK' }).click();

  expect((await refused).status(), 'the CSRF Content-Type gate is not enforced').toBe(400);

  // And the bucket did not take it: a reload asks the server, not the optimistic row the
  // widget drew before the answer came back.
  await page.reload({ waitUntil: 'domcontentloaded' });
  await page
    .getByRole('navigation', { name: /Primary|Modes/ })
    .getByRole('button', { name: /Documents/ })
    .click();
  await expect(page.getByRole('button', { name: 'Add New' })).toBeVisible();
  await expect(panelEntry(page, folder)).toHaveCount(0);
});
