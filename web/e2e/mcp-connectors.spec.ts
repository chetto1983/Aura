import { expect } from '@playwright/test';
import { test } from './fixtures';

test('MCP page exposes only guided mail and database connectors', async ({ authedPage: page }) => {
  await page.goto('/mcp');

  await expect(page.getByRole('heading', { name: /connettori|connectors/i })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'mail_mcp', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'database', exact: true })).toBeVisible();

  await expect(page.getByLabel(/provider/i).first()).toBeVisible();
  await expect(page.getByLabel(/tipo database|database type/i)).toBeVisible();
  await expect(page.getByLabel(/percorso sqlite|sqlite path/i)).toBeVisible();

  await expect(page.getByText('Google Workspace')).toHaveCount(0);
  await expect(page.getByText('Gmail dedicated')).toHaveCount(0);
  await expect(page.getByText('Outlook Assistant')).toHaveCount(0);
  await expect(page.getByText('ExecuteAutomation Database')).toHaveCount(0);
});
