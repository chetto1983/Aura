import { expect, test, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// first-run-route.spec.ts — the management-key design's first-run route step, on desktop and
// mobile, against mocked routes. The daemon says the admin's route is required, the admin types
// the services cap and the management key, the save mints both keys, Aura restarts once and the
// setup closes. Every settings write is fulfilled here, so the spec writes nothing to the stack
// it runs against.

const ADMIN_ID = '11111111-1111-4111-8111-111111111111';
const MANAGEMENT_KEY = 'sk-or-v1-e2e-management-key-must-not-render';

interface Recorded {
  readonly writes: string[];
  restarts: number;
}

async function installRouteStepRoutes(page: Page, recorded: Recorded) {
  let healthAfterRestart = 0;
  await page.route('**/api/onboarding/status', (route) =>
    route.fulfill({
      json: {
        required: false,
        completed: true,
        skipped: false,
        routeRequired: !recorded.writes.includes('AURA_OPENROUTER_MANAGEMENT_KEY'),
      },
    }),
  );
  await page.route('**/api/me', (route) =>
    route.fulfill({
      json: {
        identity_id: ADMIN_ID,
        name: 'admin@aura.local',
        capabilities: ['identity.create', 'identity.delete', 'agent.run', 'governance.write'],
      },
    }),
  );
  await page.route('**/api/settings', (route) =>
    route.fulfill({
      json: {
        restart_required: false,
        restart_supported: true,
        settings: [
          { key: 'AURA_LLM_PROVIDER', value: 'openrouter' },
          { key: 'AURA_LLM_BASE_URL', value: 'https://openrouter.ai/api/v1' },
          { key: 'AURA_LLM_MODEL', value: 'deepseek/deepseek-v4-flash:nitro' },
        ].map((row) => ({
          ...row,
          label: row.key,
          kind: 'string',
          secret: false,
          has_value: true,
          overridden: true,
          applied: 'live',
        })),
      },
    }),
  );
  await page.route('**/api/settings/llm-routes', (route) =>
    route.fulfill({ json: { routes: [] } }),
  );
  await page.route('**/api/settings/llm-models**', (route) =>
    route.fulfill({ json: { models: [] } }),
  );
  await page.route('**/api/settings/AURA_OPENROUTER_*', (route) => {
    const key = new URL(route.request().url()).pathname.split('/').pop() ?? '';
    recorded.writes.push(key);
    const minted = key === 'AURA_OPENROUTER_MANAGEMENT_KEY';
    return route.fulfill({
      json: {
        key,
        label: key,
        kind: 'string',
        secret: minted,
        value: '',
        has_value: true,
        overridden: true,
        applied: 'live',
        openrouter_keys: minted
          ? {
              identities_minted: [ADMIN_ID],
              minted_labels: { [ADMIN_ID]: 'sk-or-v1-adm...e2e' },
              services_label: 'sk-or-v1-srv...e2e',
              limits_aligned: [],
            }
          : { skipped: 'management_key_unset', identities_minted: [], limits_aligned: [] },
      },
    });
  });
  await page.route('**/api/admin/restart', (route) => {
    recorded.restarts += 1;
    return route.fulfill({ status: 202, json: { restarting: true } });
  });
  // The restarted daemon: one refused health check, then healthy again.
  await page.route('**/healthz', (route) => {
    if (recorded.restarts === 0) return route.fulfill({ json: { ok: true } });
    healthAfterRestart += 1;
    return healthAfterRestart === 1
      ? route.abort('connectionrefused')
      : route.fulfill({ json: { ok: true } });
  });
}

test('an admin connects OpenRouter in the first-run setup, which restarts Aura once', async ({
  page,
}) => {
  test.setTimeout(60_000);
  const recorded: Recorded = { writes: [], restarts: 0 };
  // gotoAuthenticated signs in and pins a completed profile; the routes installed after it win,
  // and the second navigation reads them.
  await gotoAuthenticated(page, '/');
  await installRouteStepRoutes(page, recorded);
  await page.goto('/', { waitUntil: 'domcontentloaded' });

  const dialog = page.getByRole('dialog', { name: 'Set up your profile' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('heading', { name: 'Model routing' })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Close' })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Skip for now' })).toHaveCount(0);

  await dialog.getByLabel('Services key monthly cap (USD)').fill('10');
  await dialog.getByLabel('OpenRouter management key', { exact: true }).fill(MANAGEMENT_KEY);
  await dialog.getByRole('button', { name: 'Save and continue' }).click();

  await expect(
    dialog.getByText('Your OpenRouter key: sk-or-v1-adm...e2e, no spending limit.'),
  ).toBeVisible();
  await expect(
    dialog.getByText('Services key for speech, embeddings and vision: sk-or-v1-srv...e2e.'),
  ).toBeVisible();
  expect(recorded.writes).toEqual([
    'AURA_OPENROUTER_SERVICES_CAP_USD',
    'AURA_OPENROUTER_MANAGEMENT_KEY',
  ]);

  // The setup resumes once the restarted daemon answers /healthz again.
  await dialog.getByRole('button', { name: 'Continue' }).click({ timeout: 30_000 });
  await expect(dialog).toHaveCount(0);
  expect(recorded.restarts).toBe(1);
  expect(await page.locator('body').innerHTML()).not.toContain(MANAGEMENT_KEY);
});
