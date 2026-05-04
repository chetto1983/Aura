import { test, expect } from '@playwright/test';

test.describe('/summaries evidence drill-down', () => {
  test.beforeEach(async ({ page }) => {
    await page.route('**/api/auth/whoami', async (route) => {
      await route.fulfill({ json: { user_id: 'e2e' } });
    });
    await page.route('**/api/summaries**', async (route) => {
      if (route.request().method() !== 'GET') return route.fallback();
      await route.fulfill({
        json: [
          {
            id: 101,
            chat_id: 1,
            fact: 'Remember the review queue needs source-backed proposal evidence.',
            action: 'patch',
            target_slug: 'aura-memory',
            similarity: 0.91,
            source_turn_ids: [42],
            category: 'project',
            related_slugs: ['aura'],
            provenance: {
              origin_tool: 'search_memory',
              origin_reason: 'E2E proposal from memory evidence',
              evidence: [
                {
                  kind: 'source',
                  id: 'src_0123456789abcdef',
                  title: 'memory-note.pdf',
                  page: 2,
                  snippet: 'source-backed proposal evidence',
                },
                {
                  kind: 'wiki',
                  id: 'aura-memory',
                  title: 'Aura Memory',
                },
                {
                  kind: 'archive',
                  id: 'conversation:42',
                  snippet: 'the user asked Aura to remember it',
                },
              ],
            },
            status: 'pending',
            created_at: '2026-05-04T10:00:00Z',
          },
        ],
      });
    });
    await page.route('**/api/sources', async (route) => {
      await route.fulfill({
        json: [
          {
            id: 'src_0123456789abcdef',
            kind: 'pdf',
            filename: 'memory-note.pdf',
            status: 'ingested',
            created_at: '2026-05-04T10:00:00Z',
            page_count: 2,
            wiki_pages: ['aura-memory'],
          },
        ],
      });
    });
    await page.route('**/api/wiki/page**', async (route) => {
      await route.fulfill({
        json: {
          slug: 'aura-memory',
          title: 'Aura Memory',
          body_md: 'Evidence-backed review queue notes.',
          frontmatter: {},
        },
      });
    });
    await page.route('**/api/conversations/42', async (route) => {
      await route.fulfill({
        json: {
          id: 42,
          chat_id: 1,
          user_id: 1,
          turn_index: 7,
          role: 'user',
          content: 'Please remember source-backed proposal evidence.',
          created_at: '2026-05-04T10:00:00Z',
        },
      });
    });
    await page.route('**/api/conversations**', async (route) => {
      if (!new URL(route.request().url()).pathname.endsWith('/api/conversations')) {
        return route.fallback();
      }
      await route.fulfill({
        json: [
          {
            id: 42,
            chat_id: 1,
            user_id: 1,
            turn_index: 7,
            role: 'user',
            content: 'Please remember source-backed proposal evidence.',
            created_at: '2026-05-04T10:00:00Z',
          },
        ],
      });
    });

    await page.goto('/login');
    await page.evaluate(() => window.localStorage.setItem('aura_token', 'e2e-token'));
  });

  test('evidence chips link to source, wiki, and archive context', async ({ page }) => {
    await page.goto('/summaries');

    await expect(page.getByText('Origin:')).toBeVisible();
    const source = page.getByRole('link', { name: /open evidence source:src_0123456789abcdef/i });
    const wiki = page.getByRole('link', { name: /open evidence wiki:aura-memory/i });
    const archive = page.getByRole('link', { name: /open evidence archive:conversation:42/i });

    await expect(source).toHaveAttribute('href', '/sources#source-src_0123456789abcdef');
    await expect(wiki).toHaveAttribute('href', '/wiki/aura-memory');
    await expect(archive).toHaveAttribute('href', '/conversations#turn-42');
  });

  test('archive evidence opens the conversation drawer', async ({ page }) => {
    await page.goto('/summaries');

    await page.getByRole('link', { name: /open evidence archive:conversation:42/i }).click();

    await expect(page).toHaveURL(/\/conversations#turn-42$/);
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByRole('dialog')).toContainText('source-backed proposal evidence');
  });
});
