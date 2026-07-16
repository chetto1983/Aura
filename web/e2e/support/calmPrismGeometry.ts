import { expect, type Locator, type Page } from '@playwright/test';

export interface BoundingBox {
  readonly x: number;
  readonly y: number;
  readonly width: number;
  readonly height: number;
}

export async function boxFor(locator: Locator): Promise<BoundingBox> {
  await locator.scrollIntoViewIfNeeded();
  const box = await locator.boundingBox();
  if (box === null) throw new Error('required rectangle is not rendered');
  return box;
}

export async function expectInsideViewport(locator: Locator, page: Page, label = 'rectangle') {
  const box = await boxFor(locator);
  const viewport = page.viewportSize();
  if (viewport === null) throw new Error('viewport is unavailable');
  const evidence = `${label}: ${JSON.stringify({ box, viewport })}`;
  expect(box.x, evidence).toBeGreaterThanOrEqual(0);
  expect(box.y, evidence).toBeGreaterThanOrEqual(0);
  expect(box.x + box.width, evidence).toBeLessThanOrEqual(viewport.width);
  expect(box.y + box.height, evidence).toBeLessThanOrEqual(viewport.height);
}

export function intersects(a: BoundingBox, b: BoundingBox) {
  return a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y;
}

async function expectPairSeparate(first: Locator, second: Locator) {
  await second.scrollIntoViewIfNeeded();
  const firstBox = await boxFor(first);
  const secondBox = await boxFor(second);
  expect(
    intersects(firstBox, secondBox),
    `rectangles intersect: ${JSON.stringify({ firstBox, secondBox })}`,
  ).toBe(false);
}

function userBubble(page: Page) {
  return page
    .locator('div.rounded-3xl.bg-surface-2')
    .filter({ hasText: 'Compare the release evidence, the attached runbook' })
    .first();
}

function attachment(page: Page) {
  return page
    .getByText('international-customer-reconciliation-operational-readiness-runbook-final.docx')
    .locator('xpath=ancestor::*[@data-slot="card"][1]');
}

async function owningContent(row: Locator) {
  const root = row.locator('xpath=..');
  const assistant = root.locator('[data-message-content]');
  if ((await assistant.count()) > 0) return assistant.first();
  return root.locator('xpath=./*[not(@data-message-actions)][1]');
}

export async function expectNoHorizontalOverflow(page: Page) {
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true);
}

export async function expectCalmPrismGeometry(page: Page) {
  const controls = page.locator('[data-chat-workspace-controls]');
  const firstContent = page.locator('[data-message-content]').first();
  const firstAction = page.locator('[data-message-actions]').first();
  const firstProse = page.locator('[data-message-prose]').first();
  await expectPairSeparate(controls, userBubble(page));
  await expectPairSeparate(controls, attachment(page));
  await expectPairSeparate(controls, firstAction);
  await expectPairSeparate(controls, firstProse);
  await expectPairSeparate(controls, firstContent);

  const actionRows = page.locator('[data-message-actions]');
  for (const [index, row] of (await actionRows.all()).entries()) {
    const content = await owningContent(row);
    await row.locator('xpath=..').scrollIntoViewIfNeeded();
    const rowBox = await row.boundingBox();
    const contentBox = await content.boundingBox();
    if (rowBox === null || contentBox === null) throw new Error('message geometry hook missing');
    expect(
      intersects(rowBox, contentBox),
      `message action ${String(index)} intersects content: ${JSON.stringify({ rowBox, contentBox })}`,
    ).toBe(false);
    await expectInsideViewport(row, page, `message action ${String(index)}`);
  }
  for (const block of await page.locator('[data-message-prose]').all()) {
    await expectInsideViewport(block, page);
  }
  await expectInsideViewport(controls, page);
  await expectNoHorizontalOverflow(page);
}

export async function expectCoarseTargets(page: Page) {
  const targets = page.locator('[data-required-touch-target]:visible');
  expect(await targets.count()).toBeGreaterThan(0);
  for (const [index, target] of (await targets.all()).entries()) {
    const box = await boxFor(target);
    const context = await target.evaluate((element) => ({
      tag: element.tagName,
      label: element.getAttribute('aria-label'),
      text: element.textContent?.trim() ?? '',
      className: element.getAttribute('class') ?? '',
    }));
    const evidence = `touch target ${String(index)}: ${JSON.stringify({ box, context })}`;
    expect(box.width, evidence).toBeGreaterThanOrEqual(44);
    expect(box.height, evidence).toBeGreaterThanOrEqual(44);
  }
}

export async function expectActionVisibility(page: Page, coarse: boolean) {
  const message = page.locator('[data-message-role="assistant"]').first();
  const row = message.locator('[data-message-actions]');
  await row.scrollIntoViewIfNeeded();
  if (coarse) {
    expect(await row.evaluate((element) => getComputedStyle(element).opacity)).toBe('1');
    return;
  }
  expect(await row.evaluate((element) => getComputedStyle(element).opacity)).toBe('0');
  await row.hover({ force: true });
  await expect.poll(() => row.evaluate((element) => getComputedStyle(element).opacity)).toBe('1');
  await page.mouse.move(0, 0);
  await row.getByRole('button').first().focus();
  await expect.poll(() => row.evaluate((element) => getComputedStyle(element).opacity)).toBe('1');
}
