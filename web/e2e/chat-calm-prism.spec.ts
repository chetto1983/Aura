import { mkdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { expect, test, type Page } from '@playwright/test';
import {
  expectActionVisibility,
  expectCalmPrismGeometry,
  expectCoarseTargets,
  expectNoHorizontalOverflow,
} from './support/calmPrismGeometry';
import { collectBrowserHealth } from './support/browserHealth';
import {
  ERROR_TOKEN,
  installCalmPrismFixture,
  openCalmPrism,
  revealCalmPrismMatrix,
  type CalmPrismTheme,
} from './support/calmPrismFixture';

const DEFAULT_ORIGIN = 'http://127.0.0.1:9080';
const CAPTURE_LABEL = process.env.AURA_VISUAL_LABEL;
const BUILD_COMPARISONS = process.env.AURA_VISUAL_COMPARE === '1';

const CASES = [
  { name: 'desktop-dark', viewport: { width: 1440, height: 1000 }, theme: 'dark' },
  { name: 'desktop-light', viewport: { width: 1440, height: 1000 }, theme: 'light' },
  { name: 'mobile-dark', viewport: { width: 393, height: 852 }, theme: 'dark' },
  { name: 'mobile-light', viewport: { width: 393, height: 852 }, theme: 'light' },
] as const;

const isMobileCase = (name: string) => name.startsWith('mobile-');

function origin(baseURL: string | undefined) {
  return new URL(baseURL ?? DEFAULT_ORIGIN).origin;
}

async function openMatrix(
  page: Page,
  theme: CalmPrismTheme,
  verifyContract = true,
  includeApprovals = true,
) {
  await installCalmPrismFixture(page, theme, false, includeApprovals);
  await openCalmPrism(page, false, verifyContract);
  await revealCalmPrismMatrix(page, includeApprovals);
}

function verificationDirectory() {
  const path = resolve(
    process.cwd(),
    '..',
    'docs',
    'superpowers',
    'verification',
    '2026-07-15-aura-calm-prism-chat',
  );
  mkdirSync(path, { recursive: true });
  return path;
}

test.describe('Calm Prism Chrome contracts', () => {
  test.skip(CAPTURE_LABEL !== undefined || BUILD_COMPARISONS);

  test('empty thread exposes all localized starters', async ({ page, baseURL }) => {
    await page.setViewportSize({ width: 1440, height: 1000 });
    const health = collectBrowserHealth(page, origin(baseURL));
    await installCalmPrismFixture(page, 'dark', true);
    await openCalmPrism(page, true);
    const starters = page.getByRole('group', { name: 'Suggestions' });
    for (const label of [
      'Research a topic',
      'Analyze a file',
      'Create an artifact',
      'Automate a task',
    ]) {
      await expect(starters.getByRole('button', { name: label })).toBeVisible();
    }
    await expectNoHorizontalOverflow(page);
    health.assertClean();
  });

  test('reasoning, tool, approval, typed result, artifact, and footer matrix', async ({
    page,
    baseURL,
  }, testInfo) => {
    const mobile = testInfo.project.name === 'mobile-chrome';
    await page.setViewportSize(
      mobile ? { width: 393, height: 852 } : { width: 1440, height: 1000 },
    );
    const health = collectBrowserHealth(page, origin(baseURL), [
      {
        method: 'POST',
        pathname: `/api/approvals/${ERROR_TOKEN}/resolve`,
        status: 409,
      },
    ]);
    await openMatrix(page, 'dark');
    await expect(page.getByRole('button', { name: 'Hide reasoning' })).toBeVisible();
    await expect(page.getByText('Checking release evidence', { exact: false })).toBeVisible();
    await expect(page.getByText('Running', { exact: true })).toBeVisible();
    await expect(page.getByText('Done', { exact: true })).toBeVisible();
    await expect(page.getByText('Error', { exact: true })).toBeVisible();
    await expect(page.getByText('Approval required', { exact: true })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Pilot workspace' })).toBeVisible();
    await expect(page.getByText('Expired — auto-resolved.')).toBeVisible();
    await expect(page.getByText('Artifact', { exact: true })).toBeVisible();
    await expect(page.getByText(/calm-prism-release-readiness.*\.xlsx/)).toBeVisible();

    const errorCard = page.locator(`[data-approval-token="${ERROR_TOKEN}"]`);
    await errorCard.getByPlaceholder('Type your answer').fill('Proceed with the pilot only.');
    await errorCard.getByRole('button', { name: 'Answer' }).click();
    await expect(errorCard.getByText("Couldn't resume this run", { exact: false })).toBeVisible();

    const footer = page.getByRole('contentinfo');
    const detail = footer.locator('[id^="footer-telemetry-detail-"]');
    if (mobile) {
      await expect(detail).toBeHidden();
      await expect(footer.getByTestId('footer-disclosure-cue')).toBeVisible();
      await footer.getByRole('button', { name: 'Show telemetry details' }).click();
      await expect(detail).toBeVisible();
      await footer.getByRole('button', { name: 'Hide telemetry details' }).click();
      await expect(detail).toBeHidden();
    } else {
      await expect(detail).toBeVisible();
    }
    health.assertClean();
  });

  test('zoom, forced-colors, keyboard, and accessibility-tree smoke', async ({
    page,
    baseURL,
  }, testInfo) => {
    test.skip(testInfo.project.name !== 'chrome');
    await page.setViewportSize({ width: 720, height: 500 });
    const health = collectBrowserHealth(page, origin(baseURL));
    await openMatrix(page, 'dark');
    await page.emulateMedia({
      colorScheme: 'dark',
      forcedColors: 'active',
      reducedMotion: 'reduce',
    });

    expect(await page.evaluate(() => matchMedia('(forced-colors: active)').matches)).toBe(true);
    expect(await page.evaluate(() => matchMedia('(prefers-reduced-motion: reduce)').matches)).toBe(
      true,
    );
    await expectNoHorizontalOverflow(page);

    const settled = page.getByTestId('footer-settled-status');
    await expect(settled).toContainText('Run complete.');
    await expect(settled).toHaveCount(1);
    const approval = page.getByText('Approval required', { exact: true });
    const composer = page.getByPlaceholder('Ask Aura');
    const composerHandle = await composer.elementHandle();
    if (composerHandle === null) throw new Error('composer missing from reading-order smoke');
    expect(
      await approval.evaluate(
        (node, input) =>
          Boolean(node.compareDocumentPosition(input) & Node.DOCUMENT_POSITION_FOLLOWING),
        composerHandle,
      ),
    ).toBe(true);
    await expect(composer).toBeDisabled();

    const cdp = await page.context().newCDPSession(page);
    const accessibilityTree = (await cdp.send('Accessibility.getFullAXTree')) as {
      nodes: { name?: { value?: string }; role?: { value?: string } }[];
    };
    await cdp.detach();
    expect(
      accessibilityTree.nodes.filter(
        (node) =>
          node.role?.value === 'StaticText' &&
          node.name?.value?.startsWith('Run complete.') === true,
      ),
    ).toHaveLength(1);
    const approvalIndex = accessibilityTree.nodes.findIndex(
      (node) => node.name?.value === 'Approval required',
    );
    const composerIndex = accessibilityTree.nodes.findIndex(
      (node) => node.role?.value === 'textbox' && node.name?.value === 'Ask Aura',
    );
    expect(approvalIndex).toBeGreaterThanOrEqual(0);
    expect(composerIndex).toBeGreaterThan(approvalIndex);

    const artifactsToggle = page.getByRole('button', { name: 'Toggle the artifacts panel' });
    await artifactsToggle.focus();
    await expect(artifactsToggle).toBeFocused();
    await artifactsToggle.press('Enter');
    await expect(page.getByRole('dialog', { name: 'Artifacts' })).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(page.getByRole('dialog', { name: 'Artifacts' })).toBeHidden();
    await expect(artifactsToggle).toBeFocused();
    health.assertClean();
  });

  test('geometry, containment, touch targets, and action discovery', async ({
    page,
    baseURL,
  }, testInfo) => {
    const mobile = testInfo.project.name === 'mobile-chrome';
    await page.setViewportSize(
      mobile ? { width: 393, height: 852 } : { width: 1440, height: 1000 },
    );
    const health = collectBrowserHealth(page, origin(baseURL));
    await openMatrix(page, 'dark', true, false);
    await expectCalmPrismGeometry(page);
    await expectActionVisibility(page, mobile);
    if (mobile) await expectCoarseTargets(page);
    health.assertClean();
  });

  for (const width of [320, 768]) {
    test(`reflows without overflow at ${String(width)}px`, async ({ page, baseURL }, testInfo) => {
      test.skip(testInfo.project.name !== 'chrome');
      await page.setViewportSize({ width, height: 1000 });
      const health = collectBrowserHealth(page, origin(baseURL));
      await openMatrix(page, 'dark', true, false);
      await expectCalmPrismGeometry(page);
      health.assertClean();
    });
  }

  for (const visual of CASES) {
    test(`Calm Prism ${visual.name}`, async ({ page, baseURL }, testInfo) => {
      test.skip(isMobileCase(visual.name) !== (testInfo.project.name === 'mobile-chrome'));
      await page.setViewportSize(visual.viewport);
      const health = collectBrowserHealth(page, origin(baseURL));
      await openMatrix(page, visual.theme);
      await expect(page).toHaveScreenshot(`calm-prism-${visual.name}.png`, {
        animations: 'disabled',
        caret: 'hide',
        fullPage: false,
      });
      health.assertClean();
    });
  }
});

test.describe('Calm Prism evidence capture', () => {
  test.skip(CAPTURE_LABEL === undefined || BUILD_COMPARISONS);
  for (const visual of CASES.filter((entry) => entry.theme === 'dark')) {
    test(`capture ${visual.name}`, async ({ page, baseURL }, testInfo) => {
      test.skip(isMobileCase(visual.name) !== (testInfo.project.name === 'mobile-chrome'));
      await page.setViewportSize(visual.viewport);
      const health = collectBrowserHealth(page, origin(baseURL));
      await openMatrix(page, visual.theme, CAPTURE_LABEL !== 'reference');
      const form = isMobileCase(visual.name) ? 'mobile' : 'desktop';
      await page.screenshot({
        path: resolve(verificationDirectory(), `${CAPTURE_LABEL ?? 'unknown'}-${form}-dark.png`),
        animations: 'disabled',
        caret: 'hide',
        fullPage: false,
      });
      health.assertClean();
    });
  }
});

test.describe('Calm Prism combined comparisons', () => {
  test.skip(!BUILD_COMPARISONS || CAPTURE_LABEL !== undefined);
  test('builds labelled same-input comparison PNGs', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name !== 'chrome');
    const directory = verificationDirectory();
    for (const entry of [
      { form: 'desktop', width: 1440, height: 1000 },
      { form: 'mobile', width: 393, height: 852 },
    ]) {
      const reference = readFileSync(resolve(directory, `reference-${entry.form}-dark.png`));
      const implementation = readFileSync(
        resolve(directory, `implementation-${entry.form}-dark.png`),
      );
      await page.setViewportSize({ width: entry.width * 2, height: entry.height + 44 });
      await page.setContent(`
        <style>
          * { box-sizing: border-box; }
          html, body { margin: 0; background: #111; }
          main { display: grid; grid-template-columns: repeat(2, ${String(entry.width)}px); }
          figure { margin: 0; width: ${String(entry.width)}px; }
          figcaption { height: 44px; padding: 10px 16px; background: #111; color: white;
            font: 600 16px/24px Arial, sans-serif; }
          img { display: block; width: ${String(entry.width)}px; height: ${String(entry.height)}px; }
        </style>
        <main>
          <figure><figcaption>Reference</figcaption><img alt="Reference" src="data:image/png;base64,${reference.toString('base64')}"></figure>
          <figure><figcaption>Calm Prism</figcaption><img alt="Calm Prism" src="data:image/png;base64,${implementation.toString('base64')}"></figure>
        </main>`);
      await expect(page.getByAltText('Reference')).toBeVisible();
      await expect(page.getByAltText('Calm Prism')).toBeVisible();
      await page.screenshot({
        path: resolve(directory, `comparison-${entry.form}-dark.png`),
        fullPage: true,
      });
    }
  });
});
