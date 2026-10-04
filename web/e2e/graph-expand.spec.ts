import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { installGraphShellRoutes } from './support/graphRoutes';

// Expanding a node grows the graph the reader is looking at. Measured on the local stack on
// 2026-10-04 before the fix (prd.md §9): selecting a node moved all 75 nodes, and expanding
// one swapped the canvas for a loading line, laid every node out again from random positions,
// and said nothing when its neighbours were already drawn. The backend is stubbed; the
// renderer, its layout and the double click are the real ones.

const CONV_ID = '77777777-7777-7777-7777-777777777777';

const node = (id: string, caption: string) => ({ id, caption, labels: ['Entity'], degree: 1 });
const fact = (id: string, source: string, target: string) => ({
  id,
  source,
  target,
  rel_type: 'FACT',
});
const SCHEMA = { labels: ['Entity'], rel_types: ['FACT'] };

const OVERVIEW = {
  nodes: [node('#1:0', 'Alpha'), node('#1:1', 'Bravo'), node('#1:2', 'Charlie')],
  edges: [fact('#5:0', '#1:0', '#1:1'), fact('#5:1', '#1:1', '#1:2')],
  schema: SCHEMA,
  query: 'SELECT FROM `FACT` LIMIT 200',
};

const ALPHA_NEIGHBOURS = {
  nodes: [
    node('#1:0', 'Alpha'),
    node('#1:1', 'Bravo'),
    node('#1:3', 'Delta'),
    node('#1:4', 'Echo'),
  ],
  edges: [fact('#5:0', '#1:0', '#1:1'), fact('#5:2', '#1:0', '#1:3'), fact('#5:3', '#1:0', '#1:4')],
  schema: SCHEMA,
  query: 'SELECT expand(bothE()) FROM #1:0 LIMIT 200',
};

type Positions = Record<string, { x: number; y: number }>;

interface CytoscapeNode {
  id: () => string;
  position: () => { x: number; y: number };
  renderedPosition: () => { x: number; y: number };
}
interface CytoscapeCore {
  nodes: () => { map: <T>(callback: (node: CytoscapeNode) => T) => T[] };
  getElementById: (id: string) => CytoscapeNode;
}

// Cytoscape registers its instance on the container it draws into.
async function positions(page: Page): Promise<Positions> {
  return page.evaluate(() => {
    const container = document.querySelector('[data-testid="arcade-graph-canvas"]') as
      (HTMLElement & { _cyreg?: { cy?: CytoscapeCore } }) | null;
    const cy = container?._cyreg?.cy;
    if (cy === undefined) return {};
    return Object.fromEntries(cy.nodes().map((n) => [n.id(), { ...n.position() }]));
  });
}

// The first layout animates; read positions once two reads a beat apart agree.
async function settledPositions(page: Page): Promise<Positions> {
  let previous = await positions(page);
  for (let attempt = 0; attempt < 20; attempt++) {
    await page.waitForTimeout(250);
    const current = await positions(page);
    if (JSON.stringify(current) === JSON.stringify(previous) && Object.keys(current).length > 0) {
      return current;
    }
    previous = current;
  }
  return previous;
}

function expectUnmoved(before: Positions, after: Positions) {
  for (const [id, at] of Object.entries(before)) {
    const now = after[id];
    expect(now, `node ${id} is still drawn`).toBeDefined();
    expect(Math.hypot((now?.x ?? 0) - at.x, (now?.y ?? 0) - at.y), `node ${id} moved`).toBeLessThan(
      1,
    );
  }
}

async function openGraph(page: Page) {
  await installGraphShellRoutes(page, CONV_ID);
  await page.route('**/api/graph/query', async (route) => {
    const intent = route.request().postDataJSON() as { op?: string };
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(intent.op === 'expand' ? ALPHA_NEIGHBOURS : OVERVIEW),
    });
  });
  await gotoAuthenticated(page, `/c/${CONV_ID}`);
  await page.getByRole('button', { name: 'Graph', exact: true }).first().click();
  await expect(page.getByRole('img', { name: /Memory graph:/ })).toBeVisible({ timeout: 15_000 });
}

async function selectFromList(page: Page, caption: string) {
  await page.getByRole('listitem').filter({ hasText: caption }).getByRole('button').first().click();
}

async function expectGrownAround(page: Page, before: Positions) {
  await expect(page.getByRole('status').filter({ hasText: '2 new neighbors added' })).toBeVisible();
  const after = await settledPositions(page);
  expect(Object.keys(after).sort()).toEqual(['#1:0', '#1:1', '#1:2', '#1:3', '#1:4']);
  expectUnmoved(before, after);
  const path = page.getByLabel('Selected path');
  for (const caption of ['Alpha', 'Bravo', 'Delta', 'Echo'])
    await expect(path).toContainText(caption);
  await expect(path).not.toContainText('Charlie');
}

test.describe('Graph expansion grows the graph in place', () => {
  test('selecting a node moves nothing; Expand adds the neighbours around it', async ({ page }) => {
    await openGraph(page);
    const before = await settledPositions(page);

    await selectFromList(page, 'Alpha');
    await page.waitForTimeout(600);
    expectUnmoved(before, await positions(page));

    await page.getByRole('button', { name: 'Expand neighbors' }).click();
    await expectGrownAround(page, before);
  });

  test('a double click on a node expands it', async ({ page }, testInfo) => {
    test.skip(
      testInfo.project.name !== 'chrome',
      'a double click is a pointer gesture; the touch profiles expand from the inspector, covered above',
    );
    await openGraph(page);
    const before = await settledPositions(page);
    const point = await page.evaluate(() => {
      const container = document.querySelector('[data-testid="arcade-graph-canvas"]') as
        (HTMLElement & { _cyreg?: { cy?: CytoscapeCore } }) | null;
      const alpha = container?._cyreg?.cy?.getElementById('#1:0');
      if (container === null || alpha === undefined) return null;
      const box = container.getBoundingClientRect();
      return { x: box.left + alpha.renderedPosition().x, y: box.top + alpha.renderedPosition().y };
    });
    if (point === null) throw new Error('the canvas has no Alpha node');

    await page.mouse.dblclick(point.x, point.y);
    await expectGrownAround(page, before);
  });
});
