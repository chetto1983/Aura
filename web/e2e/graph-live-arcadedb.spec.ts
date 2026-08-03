import { expect, test, type Request } from '@playwright/test';
import { gotoAuthenticated } from './auth';
import { collectBrowserHealth } from './support/browserHealth';

const live = process.env.AURA_E2E_LIVE_GRAPH === '1';

function graphIntent(request: Request): Record<string, unknown> | undefined {
  if (new URL(request.url()).pathname !== '/api/graph/query') return undefined;
  const body = request.postData();
  if (body === null) return undefined;
  return JSON.parse(body) as Record<string, unknown>;
}

function countFrom(text: string): number {
  const match = /\((\d+)\)/.exec(text);
  return Number(match?.[1] ?? Number.NaN);
}

test.describe('live ArcadeDB memory graph', () => {
  test.skip(!live, 'set AURA_E2E_LIVE_GRAPH=1 against a rebuilt Aura');

  test('draws the authenticated identity graph and expands a RID cumulatively', async ({
    page,
    baseURL,
  }, testInfo) => {
    test.setTimeout(45_000);
    const origin = new URL(baseURL ?? 'http://127.0.0.1:9080').origin;
    const health = collectBrowserHealth(page, origin);
    const intents: Record<string, unknown>[] = [];
    page.on('request', (request) => {
      const intent = graphIntent(request);
      if (intent !== undefined) intents.push(intent);
    });

    await gotoAuthenticated(page, '/');
    await page.getByRole('button', { name: 'Graph', exact: true }).first().click();

    await expect(page.getByRole('img', { name: /Memory graph:/ })).toBeVisible({
      timeout: 15_000,
    });
    const evidence = page.locator('.graph-workspace__evidence');
    const nodeHeading = evidence.getByText(/^Nodes \(\d+\)/);
    const edgeHeading = evidence.getByText(/^Connections \(\d+\)$/);
    await expect(nodeHeading).toBeVisible();
    await expect(edgeHeading).toBeVisible();
    const beforeNodes = countFrom((await nodeHeading.textContent()) ?? '');
    const beforeEdges = countFrom((await edgeHeading.textContent()) ?? '');
    expect(beforeNodes).toBeGreaterThan(0);
    expect(beforeEdges).toBeGreaterThan(0);

    await evidence.getByRole('button').first().click();
    const inspector = page.getByRole('complementary', { name: 'Select a node' });
    const expandResponse = page.waitForResponse((response) => {
      const intent = graphIntent(response.request());
      return intent?.op === 'expand';
    });
    await inspector.getByRole('button', { name: 'Expand neighbors' }).click();
    expect((await expandResponse).status()).toBe(200);

    await expect
      .poll(async () => countFrom((await nodeHeading.textContent()) ?? ''))
      .toBeGreaterThanOrEqual(beforeNodes);
    await inspector.getByRole('button', { name: 'Show ArcadeDB SQL' }).click();
    await expect(inspector.locator('pre')).toContainText('SELECT');

    expect(intents[0]).toMatchObject({ op: 'overview' });
    expect(intents[0]).not.toHaveProperty('session');
    expect(intents[0]).not.toHaveProperty('seed_id');
    const expansion = intents.find((intent) => intent.op === 'expand');
    expect(expansion?.node_id).toMatch(/^#[0-9]+:[0-9]+$/);
    health.assertClean();

    await testInfo.attach('live-arcadedb-graph.png', {
      body: await page.screenshot({ fullPage: true }),
      contentType: 'image/png',
    });
  });
});
