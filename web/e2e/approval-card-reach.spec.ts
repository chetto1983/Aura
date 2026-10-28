import { expect, test, type Locator, type Page } from '@playwright/test';
import { collectBrowserHealth } from './support/browserHealth';
import { CONV_ID, installCalmPrismFixture, openCalmPrism } from './support/calmPrismFixture';

// The operator's report of 2026-10-28: on a 360 px phone, with the telemetry footer open, a
// plugin-install approval showed its question and its answer field, and nothing below them —
// no Answer, no Decline, no composer. Playwright's click() scrolls every ancestor, an
// overflow-hidden one included, so a spec that clicks would pass where a finger cannot. This
// one scrolls only the way a finger can (the wheel over the card) and acts at the pixel.

const PLUGIN_TOKEN = 'plugin-install-reach-token';
const PHONE = { width: 360, height: 800 };

// The card of the report, as internal/approvaltext/presentation_plugin.go presents it.
function pluginInstall() {
  return {
    token: PLUGIN_TOKEN,
    conversation_id: CONV_ID,
    kind: 'approval',
    question: 'Approve plugin install?',
    priority: 0,
    presentation: {
      key: 'approval.plugin.install',
      params: {
        plugin: '"broker-analysis" "0.3.0"',
        source:
          'chetto1983/broker-analysis-mcp/broker-analysis@9886eb305a85b6291ab72fa43b16f77382b768ae',
        skills:
          '(5): "custom-analysis", "etf-selection", "fundamental-analysis", "risk-analysis", "technical-analysis"',
        connectors:
          '(1): "broker-analysis" (box): "uvx" "--from" "git+https://github.com/chetto1983/broker-analysis-mcp@v0.3.0" "broker-analysis-mcp"',
        skipped: '(0): none',
        mcp: 'enabled',
        notes: '(0): none',
        digest: '4fca0747d8c474b14d3125a8a9cd42b5692ac995d93ee24dc466ac8ec411e72b',
      },
    },
  };
}

// reachable is a finger's test: the centre of the element is inside the viewport and the
// element is what a tap there hits, so neither a clipping ancestor nor an overlay hides it.
async function reachable(target: Locator): Promise<boolean> {
  return target.evaluate((element) => {
    const box = element.getBoundingClientRect();
    const x = box.left + box.width / 2;
    const y = box.top + box.height / 2;
    if (x < 0 || y < 0 || x > innerWidth || y > innerHeight) return false;
    const hit = document.elementFromPoint(x, y);
    return hit !== null && element.contains(hit);
  });
}

async function scrollUntilReachable(page: Page, over: Locator, target: Locator) {
  const box = await over.boundingBox();
  if (box === null) throw new Error('the card is not rendered');
  await page.mouse.move(box.x + box.width / 2, Math.max(box.y, 0) + 24);
  for (let wheel = 0; wheel < 12 && !(await reachable(target)); wheel += 1) {
    await page.mouse.wheel(0, 160);
    await page.waitForTimeout(50);
  }
  expect(await reachable(target), 'scrolling over the card never brought it within reach').toBe(
    true,
  );
}

async function tap(page: Page, target: Locator) {
  const box = await target.boundingBox();
  if (box === null) throw new Error('the target is not rendered');
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
}

test('a long approval keeps its answer within a finger on a phone', async ({
  page,
  baseURL,
}, testInfo) => {
  await page.setViewportSize(PHONE);
  const health = collectBrowserHealth(page, new URL(baseURL ?? 'http://127.0.0.1:9080').origin);
  await installCalmPrismFixture(page, 'dark');
  await page.route('**/api/approvals', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([pluginInstall()]),
    }),
  );
  const resolved: unknown[] = [];
  await page.route(`**/api/approvals/${PLUGIN_TOKEN}/resolve`, (route) => {
    resolved.push(route.request().postDataJSON());
    return route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ outcome: 'approved', remaining: 0 }),
    });
  });
  await openCalmPrism(page);

  const footer = page.getByRole('contentinfo');
  await footer.getByRole('button', { name: 'Show telemetry details' }).click();
  const card = page.locator(`[data-approval-token="${PLUGIN_TOKEN}"]`);
  await expect(card.getByText('broker-analysis-mcp', { exact: false })).toBeVisible({
    timeout: 15_000,
  });
  await expect.soft(page.getByPlaceholder('Ask Aura'), 'the locked composer').toBeInViewport({
    ratio: 1,
  });

  const answer = card.getByRole('button', { name: 'Answer' });
  await scrollUntilReachable(page, card.getByText('Approval required', { exact: true }), answer);
  for (const name of ['Decline', 'Cancel run']) {
    expect(await reachable(card.getByRole('button', { name })), name).toBe(true);
  }
  await testInfo.attach('approval-reached.png', {
    body: await page.screenshot(),
    contentType: 'image/png',
  });
  const field = card.getByPlaceholder('Type your answer');
  expect(await reachable(field), 'the answer field').toBe(true);
  await tap(page, field);
  await page.keyboard.type('ok');
  await tap(page, answer);

  await expect(card.getByText('Answered.')).toBeVisible();
  expect(resolved).toEqual([{ action: 'accept', content: 'ok' }]);
  health.assertClean();
});
