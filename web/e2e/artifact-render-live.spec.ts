import { expect, test } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// artifact-render-live.spec.ts drives the REAL cockpit against a REAL agent-delivered
// HTML artifact on a live stack — no mocked route, no fixture asset. It is the live
// half of the sealed-render lane whose server side is unit-tested in
// internal/agui/asset_render_test.go: the panel opens the artifact, the preview frames
// GET /api/assets/{id}/render with src= (never srcdoc), and the document that comes
// back carries the sealed policy AND still executes.
//
// Gated because it needs a stack with that specific artifact in it:
//   AURA_E2E_LIVE_ARTIFACT=1
//   AURA_E2E_ARTIFACT_THREAD=<conversation uuid>
//   AURA_E2E_ARTIFACT_FILE=<file name of an accepted text/html asset on that thread>
//
// No-skip-as-green: every leg increments `proofs` and the test fails below the count,
// so a run that silently framed nothing cannot report green.

const live = process.env.AURA_E2E_LIVE_ARTIFACT === '1';
const threadID = process.env.AURA_E2E_ARTIFACT_THREAD ?? '';
const fileName = process.env.AURA_E2E_ARTIFACT_FILE ?? '';

interface SealedProbe {
  readonly status: number;
  readonly contentType: string | null;
  readonly csp: string | null;
  readonly nosniff: string | null;
  readonly cacheControl: string | null;
  readonly body: string;
}

test.describe('live artifact render lane', () => {
  test.skip(!live, 'set AURA_E2E_LIVE_ARTIFACT=1 with THREAD + FILE against a live stack');

  test('inline artifact expands, splits code and preview, downloads and returns to chat', async ({
    page,
  }, testInfo) => {
    test.setTimeout(90_000);
    await gotoAuthenticated(page, `/c/${threadID}`);
    const viewer = page.getByRole('region', { name: `Artifact preview: ${fileName}` });
    await expect(viewer).toBeVisible({ timeout: 20_000 });
    await expect(viewer.locator('iframe')).toHaveAttribute('sandbox', 'allow-scripts');
    await viewer.scrollIntoViewIfNeeded();
    await page.screenshot({ path: testInfo.outputPath('inline.png') });
    await viewer.getByRole('button', { name: 'Expand artifact', exact: true }).click();
    await expect(viewer.getByRole('button', { name: 'Back to conversation' })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath('expanded.png') });
    await viewer.getByRole('button', { name: 'Show code', exact: true }).click();
    await expect(viewer.locator('pre')).toContainText('<', { timeout: 15_000 });
    await expect(viewer.locator('pre.shiki')).toBeVisible({ timeout: 10_000 });
    await expect(viewer.locator('iframe')).toBeVisible();
    await expect(viewer.getByRole('separator')).toBeVisible();
    const sourceBox = await viewer.locator('pre').boundingBox();
    const frameBox = await viewer.locator('iframe').boundingBox();
    expect(sourceBox).not.toBeNull();
    expect(frameBox).not.toBeNull();
    if (!sourceBox || !frameBox) throw new Error('Split preview did not lay out');
    if ((page.viewportSize()?.width ?? 1280) < 640) {
      expect(frameBox.y).toBeGreaterThan(sourceBox.y);
    } else {
      expect(frameBox.x).toBeGreaterThan(sourceBox.x);
      await expect(
        page.getByRole('complementary', { name: 'Navigation', exact: true }),
      ).toBeVisible();
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await page.screenshot({ path: testInfo.outputPath('split.png') });
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
    await viewer.getByRole('button', { name: 'Copy code', exact: true }).click();
    await expect(viewer.getByRole('button', { name: 'Copied', exact: true })).toBeVisible();
    const downloadPromise = page.waitForEvent('download');
    await viewer.getByRole('link', { name: `Download ${fileName}`, exact: true }).click();
    const download = await downloadPromise;
    expect(download.suggestedFilename()).toBe(fileName);
    await viewer.getByRole('button', { name: 'Hide code', exact: true }).click();
    await expect(viewer.locator('pre')).toHaveCount(0);
    await viewer.getByRole('button', { name: 'Back to conversation' }).click();
    await expect(page.getByRole('button', { name: 'Expand artifact', exact: true })).toBeFocused();
    await page.reload();
    await expect(viewer).toBeVisible({ timeout: 15_000 });
  });

  test('the cockpit frames the sealed render route and the document runs', async ({
    page,
  }, testInfo) => {
    test.setTimeout(90_000);
    let proofs = 0;

    await gotoAuthenticated(page, `/c/${threadID}`);

    // The panel toggle and the panel's own close button BOTH answer to /artifact/i, so the
    // opener is addressed by its exact label and taken from outside the panel — .last() once
    // picked the close button and shut the panel it had just opened.
    const panel = page.getByRole('region', { name: 'Artifacts' });
    if (!(await panel.isVisible().catch(() => false))) {
      await page.getByRole('button', { name: 'Toggle the artifacts panel' }).first().click();
      await expect(panel).toBeVisible({ timeout: 15_000 });
    }
    await expect(panel.getByText(fileName, { exact: true })).toBeVisible({ timeout: 20_000 });
    proofs += 1;

    // Open the preview. The row's own label opens the modal; the trailing control downloads.
    await panel.getByText(fileName, { exact: true }).click();
    const dialog = page.getByRole('dialog');
    await dialog.waitFor({ state: 'visible', timeout: 15_000 });
    proofs += 1;

    // 1) The default tab is the rendered one, and it frames the SEALED ROUTE via src=.
    //    srcdoc is the thing being replaced: a srcdoc document inherits the embedder's CSP
    //    and can carry none of its own, so asserting the absence of srcdoc is asserting the
    //    property, not the implementation.
    const frame = dialog.locator('iframe');
    await expect(frame).toHaveAttribute('src', /\/api\/assets\/[0-9a-f-]+\/render$/, {
      timeout: 15_000,
    });
    await expect(frame).toHaveAttribute('sandbox', 'allow-scripts');
    expect(await frame.getAttribute('srcdoc')).toBeNull();
    proofs += 1;

    const src = await frame.getAttribute('src');
    expect(src).not.toBeNull();
    if (src === null) {
      throw new Error('sealed artifact iframe has no src');
    }

    // 2) The response is a real document with real headers — which is the whole point of
    //    moving off srcdoc. Fetched from INSIDE the page so the __Host- session cookie rides
    //    along (page.request does not carry it).
    const sealed: SealedProbe = await page.evaluate(async (url: string) => {
      const res = await fetch(url, { credentials: 'same-origin' });
      return {
        status: res.status,
        contentType: res.headers.get('content-type'),
        csp: res.headers.get('content-security-policy'),
        nosniff: res.headers.get('x-content-type-options'),
        cacheControl: res.headers.get('cache-control'),
        body: await res.text(),
      };
    }, src);

    expect(sealed.status).toBe(200);
    expect(sealed.contentType).toContain('text/html');
    expect(sealed.nosniff).toBe('nosniff');
    expect(sealed.cacheControl).toContain('no-store');
    expect(sealed.csp).toContain("default-src 'none'");
    expect(sealed.csp).toContain("connect-src 'none'");
    expect(sealed.csp).toContain("base-uri 'none'");
    expect(sealed.csp).toContain("form-action 'none'");
    expect(sealed.csp).toContain("frame-ancestors 'self'");
    proofs += 1;

    // 3) The scrub ran: no <base>, no meta refresh, no fetch-on-parse <link>.
    expect(sealed.body).not.toMatch(/<base\b/i);
    expect(sealed.body).not.toMatch(/http-equiv\s*=\s*["']?refresh/i);
    expect(sealed.body).not.toMatch(
      /rel\s*=\s*["']?(preload|prefetch|dns-prefetch|preconnect|modulepreload)/i,
    );
    // …and the policy precedes the shim, so the shim itself runs under it.
    const metaAt = sealed.body.search(/http-equiv\s*=\s*["']?Content-Security-Policy/i);
    const shimAt = sealed.body.indexOf('__aura_artifact_probe__');
    expect(metaAt).toBeGreaterThanOrEqual(0);
    expect(shimAt).toBeGreaterThan(metaAt);
    proofs += 1;

    // 4) THE POINT: the document actually renders. Count real element nodes in the frame's
    //    body — a blank artifact (the React #299 failure this lane was built around) yields 0.
    const rendered = frame.contentFrame();
    const painted = await rendered.locator('body').evaluate((body: HTMLElement) => ({
      elements: body.querySelectorAll('*').length,
      text: (body.innerText ?? '').trim().length,
    }));
    testInfo.annotations.push({ type: 'painted', description: JSON.stringify(painted) });
    expect(painted.elements).toBeGreaterThan(0);
    expect(painted.text).toBeGreaterThan(0);
    proofs += 1;

    // 5) The Source tab shows the markup as TEXT — parsed by nothing.
    await dialog.getByRole('tab', { name: 'Source' }).click();
    const source = dialog.locator('pre');
    await expect(source).toBeVisible({ timeout: 15_000 });
    await expect(source).toContainText('<', { timeout: 10_000 });
    expect(await dialog.locator('iframe').count()).toBe(0);
    proofs += 1;

    // 6) …and back to the rendered tab, which must re-frame the same sealed route.
    await dialog.getByRole('tab', { name: 'Preview' }).click();
    await expect(dialog.locator('iframe')).toHaveAttribute('src', src, {
      timeout: 15_000,
    });
    proofs += 1;

    expect(proofs).toBeGreaterThanOrEqual(8);
  });
});
