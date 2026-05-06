import { test, expect, type Page } from './fixtures';

const APP_ROUTES = [
  { path: '/', heading: /dashboard/i },
  { path: '/wiki', heading: /^wiki$/i },
  { path: '/wiki/e2e-route-page', heading: /e2e route page/i },
  { path: '/graph', heading: /graph/i },
  { path: '/sources', heading: /sources/i },
  { path: '/tasks', heading: /scheduled tasks|tasks/i },
  { path: '/swarm', heading: /swarm/i },
  { path: '/skills', heading: /skills/i },
  { path: '/mcp', heading: /mcp/i },
  { path: '/pending', heading: /pending/i },
  { path: '/conversations', heading: /conversations/i },
  { path: '/summaries', heading: /summaries|review/i },
  { path: '/maintenance', heading: /maintenance/i },
  { path: '/settings', heading: /^settings$/i },
];

async function installDeterministicPageRoutes(page: Page) {
  await page.route('**/api/wiki/page?slug=e2e-route-page', async (route) => {
    await route.fulfill({
      json: {
        slug: 'e2e-route-page',
        title: 'E2E route page',
        body_md: 'Route coverage fixture.',
        frontmatter: { category: 'test', tags: ['e2e'] },
      },
    });
  });
  await page.route('**/api/conversations/stats', async (route) => {
    await route.fulfill({
      json: {
        total_rows: 1,
        distinct_chats: 1,
        oldest_at: '2026-05-05T10:00:00Z',
        newest_at: '2026-05-05T10:00:00Z',
      },
    });
  });
  await page.route('**/api/conversations**', async (route) => {
    if (!new URL(route.request().url()).pathname.endsWith('/api/conversations')) {
      await route.fallback();
      return;
    }
    await route.fulfill({
      json: [{
        id: 101,
        chat_id: 42,
        role: 'user',
        content: 'Route audit conversation fixture',
        tool_calls_count: 0,
        created_at: '2026-05-05T10:00:00Z',
      }],
    });
  });
}

async function expectNoFrontendAuditArtifacts(page: Page) {
  const mainText = await page.locator('main').innerText();
  expect(mainText).not.toMatch(/\{[A-Za-z][A-Za-z0-9_]*\}/);
  expect(mainText).not.toMatch(/\{\{[A-Za-z][A-Za-z0-9_]*\}\}/);
  await expect(page.getByText(/failed to load/i)).toHaveCount(0);
  await expect(page.getByText(/page not found/i)).toHaveCount(0);
}

test.describe('all dashboard pages', () => {
  for (const route of APP_ROUTES) {
    test(`${route.path} renders without missing locale artifacts`, async ({ authedPage: page }) => {
      await installDeterministicPageRoutes(page);
      await page.goto(route.path);

      await expect(page.getByRole('navigation')).toBeVisible({ timeout: 5_000 });
      await expect(page.getByRole('heading', { name: route.heading }).first()).toBeVisible({ timeout: 8_000 });
      await expectNoFrontendAuditArtifacts(page);
    });
  }

  test('/graph exposes normal user graph controls', async ({ authedPage: page }) => {
    await page.route('**/api/wiki/graph', async (route) => {
      await route.fulfill({
        json: {
          nodes: [
            { id: 'alpha-page', title: 'Alpha page', category: 'notes' },
            { id: 'beta-source', title: 'Beta source', category: 'sources' },
          ],
          edges: [{ source: 'alpha-page', target: 'beta-source', type: 'wikilink' }],
        },
      });
    });

    await page.goto('/graph');

    await expect(page.getByLabel(/search graph nodes/i)).toBeVisible();
    await expect(page.getByRole('button', { name: /fit/i })).toBeVisible();
    await expect(page.getByText(/2 nodes \/ 1 links/i)).toBeVisible();

    await page.getByLabel(/search graph nodes/i).fill('alpha');
    await expect(page.getByText(/1 matches/i)).toBeVisible();
  });
});
