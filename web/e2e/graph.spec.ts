import { expect, test, type Page } from '@playwright/test';
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
      id: 'n1',
      caption: 'Alpha Entity',
      labels: ['Entity'],
      entity_type: 'PERSON',
      degree: 2,
      props: { name: 'Alpha Entity' },
    },
    {
      id: 'n2',
      caption: 'Beta Document',
      labels: ['Document'],
      degree: 1,
      props: { url: 'https://docs.example.test/b', title: 'Beta Document' },
      ref_id: 'src-2',
      citations: ['Cited source X'],
    },
  ],
  edges: [{ id: 'e1', source: 'n1', target: 'n2', rel_type: 'MENTIONS' }],
  schema: { labels: ['Entity', 'Document'], rel_types: ['MENTIONS'], entity_types: ['PERSON'] },
  query: 'MATCH (e:Entity)-[r]-(n) RETURN e, r, n',
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

test.describe('Phase 27 — Graph Explorer (desktop + mobile)', () => {
  test('opens the WebGL canvas with the a11y node/edge list and path strip', async ({
    page,
  }, testInfo) => {
    await openGraphSurface(page);

    // The canvas region renders with role="img" + an accessible name naming the counts.
    const canvas = page.getByRole('img', { name: /Evidence graph:/ });
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
    await expect(page.getByRole('img', { name: /Evidence graph:/ })).toBeVisible({
      timeout: 15000,
    });

    // Select the Document node from the node list (the non-hover access path).
    await page.getByRole('button', { name: /Beta Document/ }).click();

    // The inspector region (aria-label "Select a node") shows the node + its citations list.
    const inspector = page.getByRole('complementary', { name: 'Select a node' });
    await expect(inspector.getByText('Citations')).toBeVisible();
    await expect(inspector.getByText('Cited source X')).toBeVisible();

    // Read-only: pin path / show Cypher exist; add-note does NOT (T-27-02).
    await expect(inspector.getByRole('button', { name: 'Pin path' })).toBeVisible();
    await expect(inspector.getByRole('button', { name: 'Show Cypher' })).toBeVisible();
    await expect(inspector.getByRole('button', { name: /add note/i })).toHaveCount(0);
  });

  test('Show Cypher reveals the read-only query — never an editable input (D-04/D-09)', async ({
    page,
  }) => {
    await openGraphSurface(page);
    await expect(page.getByRole('img', { name: /Evidence graph:/ })).toBeVisible({
      timeout: 15000,
    });

    await page.getByRole('button', { name: /Beta Document/ }).click();
    const inspector = page.getByRole('complementary', { name: 'Select a node' });
    await inspector.getByRole('button', { name: 'Show Cypher' }).click();
    await expect(inspector.getByText('MATCH (e:Entity)-[r]-(n) RETURN e, r, n')).toBeVisible();
    // The Cypher preview is display-only — it renders inside a <pre>, never an editable input.
    // (Scope to the inspector: the page-level "Ask Aura" chat composer is a legitimate textbox,
    // mirroring the displays.spec swarm-card scoping precedent — it is NOT a Cypher field.)
    await expect(inspector.locator('textarea, input')).toHaveCount(0);
    await expect(inspector.locator('pre')).toBeVisible();
  });
});
