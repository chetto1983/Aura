import { expect, test, type Locator, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// graph.spec.ts — the Phase 27 Graph Explorer E2E. It proves the operator can open the Frame-06
// workspace (the live 'graph' surface), the Sigma WebGL canvas renders in a REAL browser (the
// only place WebGL runs — jsdom can't, Pitfall 4), the a11y parallel DOM (node/edge list) and
// the path strip are present, and the inspector opens from a node-list selection (the non-hover
// access path). The two /api/graph/* routes are mocked at the page-network layer so only the
// served SPA + auth come from `aura serve`. Runs on desktop chromium + mobile chrome.

const CONV_ID = '88888888-8888-8888-8888-888888888888';

const POPULATED = {
  nodes: [
    {
      id: '#1:0',
      caption: 'Alpha Entity',
      labels: ['Entity'],
      entity_type: 'PERSON',
      degree: 2,
      props: { name: 'Alpha Entity' },
    },
    {
      id: '#1:1',
      caption: 'Beta Document',
      labels: ['Document'],
      degree: 1,
      props: { url: 'https://docs.example.test/b', title: 'Beta Document' },
      ref_id: 'src-2',
      citations: ['Cited source X'],
    },
  ],
  edges: [{ id: '#5:0', source: '#1:0', target: '#1:1', rel_type: 'FACT', caption: 'cites' }],
  schema: { labels: ['Entity'], rel_types: ['FACT'], entity_types: ['PERSON'] },
  query: 'SELECT FROM `FACT` LIMIT 200',
};

const SCHEMA = {
  labels: ['Entity', 'Document'],
  rel_types: ['MENTIONS'],
  entity_types: ['PERSON'],
};

async function installGraphRoutes(page: Page) {
  await page.route('**/api/conversations*', (route) => {
    if (route.request().url().includes(`/api/conversations/${CONV_ID}`)) {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: CONV_ID,
          title: 'Graph thread',
          status: 'active',
          total_input_tokens: 0,
          total_output_tokens: 0,
          total_cached_tokens: 0,
          total_cost_usd: 0,
        }),
      });
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: '[]' });
  });
  await page.route('**/api/conversations/*/rot-events', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/api/approvals', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: '[]' }),
  );
  await page.route('**/threads/*/messages', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ type: 'MESSAGES_SNAPSHOT', messages: [] }),
    }),
  );
  await page.route('**/api/graph/schema', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(SCHEMA) }),
  );
  await page.route('**/api/graph/query', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(POPULATED),
    }),
  );
}

async function openGraphSurface(page: Page) {
  await installGraphRoutes(page);
  await gotoAuthenticated(page, `/c/${CONV_ID}`);
  // Switch to the live 'graph' surface via the mode control (text "Graph").
  await page.getByRole('button', { name: 'Graph', exact: true }).first().click();
}

async function dragHandleBy(page: Page, handle: Locator, deltaX: number) {
  const box = await handle.boundingBox();
  if (box === null) throw new Error('resize handle has no bounding box');
  const y = box.y + box.height / 2;
  await page.mouse.move(box.x + box.width / 2, y);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + deltaX, y, { steps: 10 });
  await page.mouse.up();
}

test.describe('Phase 27 — Graph Explorer (desktop + mobile)', () => {
  test('opens the WebGL canvas with the a11y node/edge list and path strip', async ({
    page,
  }, testInfo) => {
    await openGraphSurface(page);

    // The canvas region renders with role="img" + an accessible name naming the counts.
    const canvas = page.getByRole('img', { name: /Memory graph:/ });
    await expect(canvas).toBeVisible({ timeout: 15000 });

    // The a11y parallel DOM (the non-WebGL fallback + SR surface) lists the nodes + edges.
    await expect(page.getByText(/Nodes \(2\)/)).toBeVisible();
    await expect(page.getByText(/Connections \(1\)/)).toBeVisible();

    // The path strip is present (D-10).
    await expect(page.getByText('Selected path')).toBeVisible();
    await expect(page.getByText('No path selected')).toBeVisible();

    await testInfo.attach('graph-explorer.png', {
      body: await page.screenshot({ fullPage: true }),
      contentType: 'image/png',
    });
  });

  test('selecting a node from the list opens the read-only inspector with its citations', async ({
    page,
  }) => {
    await openGraphSurface(page);
    await expect(page.getByRole('img', { name: /Memory graph:/ })).toBeVisible({
      timeout: 15000,
    });

    // Select the Document node from the node list (the non-hover access path).
    await page.getByRole('button', { name: /Beta Document/ }).click();

    // The inspector region (aria-label "Select a node") shows the node + its citations list.
    const inspector = page.getByRole('complementary', { name: 'Select a node' });
    await expect(inspector.getByText('Citations')).toBeVisible();
    await expect(inspector.getByText('Cited source X')).toBeVisible();

    // Read-only actions include expansion and SQL inspection; add-note does not.
    await expect(inspector.getByRole('button', { name: 'Pin path' })).toBeVisible();
    await expect(inspector.getByRole('button', { name: 'Expand neighbors' })).toBeVisible();
    await expect(inspector.getByRole('button', { name: 'Show ArcadeDB SQL' })).toBeVisible();
    await expect(inspector.getByRole('button', { name: /add note/i })).toHaveCount(0);
  });

  test('the filters column is a resizable sidebar, and only on the wide layout', async ({
    page,
  }, testInfo) => {
    await openGraphSurface(page);
    const filters = page.getByLabel('Node types');
    const handle = page.getByRole('separator', { name: 'Resize the filters column' });

    if (testInfo.project.name !== 'chrome') {
      // The narrow regime is a CONTAINER query, not `lg` — the resizer has to be gone there,
      // which `lg:hidden` alone would NOT guarantee.
      await expect(handle).toBeHidden();
      return;
    }

    await expect(filters).toBeVisible({ timeout: 20000 });
    const before = (await filters.boundingBox())?.width ?? 0;
    await dragHandleBy(page, handle, 90);
    const after = (await filters.boundingBox())?.width ?? 0;
    expect(after).toBeGreaterThan(before + 60);
    expect(after).toBeLessThan(before + 120);
  });

  test('Show ArcadeDB SQL reveals the read-only query — never an editable input', async ({
    page,
  }) => {
    await openGraphSurface(page);
    await expect(page.getByRole('img', { name: /Memory graph:/ })).toBeVisible({
      timeout: 15000,
    });

    await page.getByRole('button', { name: /Beta Document/ }).click();
    const inspector = page.getByRole('complementary', { name: 'Select a node' });
    await inspector.getByRole('button', { name: 'Show ArcadeDB SQL' }).click();
    await expect(inspector.getByText('SELECT FROM `FACT` LIMIT 200')).toBeVisible();
    // The SQL preview is display-only — it renders inside a <pre>, never an editable input.
    // (Scope to the inspector: the page-level "Ask Aura" chat composer is a legitimate textbox,
    // mirroring the displays.spec swarm-card precedent — it is not a graph query field.)
    await expect(inspector.locator('textarea, input')).toHaveCount(0);
    await expect(inspector.locator('pre')).toBeVisible();
  });
});
