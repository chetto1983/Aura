import { test, expect } from '@playwright/test';

async function seedAuth(page: import('@playwright/test').Page) {
  await page.addInitScript(() => {
    window.localStorage.setItem('aura_token', 'test-token');
  });
}

test.describe('dashboard form labels', () => {
  test('maintenance filters expose accessible names', async ({ page }) => {
    await seedAuth(page);
    await page.route('**/api/maintenance/issues**', async (route) => {
      await route.fulfill({ json: [] });
    });

    await page.goto('/maintenance');

    await expect(page.getByRole('combobox', { name: /status/i })).toBeVisible();
    await expect(page.getByRole('combobox', { name: /severity/i })).toBeVisible();
  });

  test('MCP tool arguments editor is associated with its label', async ({ page }) => {
    await seedAuth(page);
    await page.route('**/api/mcp/servers', async (route) => {
      await route.fulfill({
        json: [
          {
            name: 'demo',
            transport: 'stdio',
            tool_count: 1,
            tools: [
              {
                name: 'echo',
                description: 'Echo test input',
                input_schema: { type: 'object', properties: { text: { type: 'string' } } },
              },
            ],
          },
        ],
      });
    });

    await page.goto('/mcp');
    await page.getByRole('button', { name: /demo/i }).click();
    await page.getByRole('button', { name: /run/i }).click();

    await expect(page.getByLabel(/arguments/i)).toBeVisible();
  });
});
