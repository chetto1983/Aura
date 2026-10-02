import { expect, test, type BrowserContext, type Page } from '@playwright/test';
import { gotoAuthenticated } from './auth';

// Hermetic browser smoke: real Settings, BrowserLivePage and input/idempotency code.
// Aura sessions/settings plus the VM stream and OpenAI authorization are fixtures;
// this does not exercise a real OAuth grant, VM browser, callback or inference.
async function installFixture(context: BrowserContext, immediateApproval = false) {
  const sessions = [
    'chatgpt-0123456789abcdef01234567',
    'chatgpt-abcdef0123456789abcdef01',
  ] as const;
  let connected = false;
  let pending = false;
  let activeRoute = '';
  let loginCount = 0;
  let profile = {
    AURA_LLM_PROVIDER: 'openrouter',
    AURA_LLM_BASE_URL: 'https://openrouter.ai/api/v1',
    AURA_LLM_MODEL: 'cloud/model',
  };
  const writes: (typeof profile)[] = [];
  const cancellations: string[] = [];
  const mutationKeys: string[] = [];
  await context.addInitScript(() => {
    const NativeSource = window.EventSource;
    class FixtureSource extends EventTarget {
      private readonly timer: ReturnType<typeof setTimeout>;
      constructor() {
        super();
        this.timer = setTimeout(() => {
          const canvas = document.createElement('canvas');
          canvas.width = 500;
          canvas.height = 300;
          const painter = canvas.getContext('2d');
          if (painter !== null) {
            painter.fillStyle = 'white';
            painter.fillRect(0, 0, 500, 300);
            painter.fillStyle = 'black';
            painter.font = '24px sans-serif';
            painter.fillText('OpenAI consent fixture', 50, 100);
            painter.fillText('Approve', 50, 200);
          }
          this.dispatchEvent(
            new MessageEvent('message', {
              data: JSON.stringify({ type: 'url', url: 'https://auth.openai.com/oauth/authorize' }),
            }),
          );
          this.dispatchEvent(
            new MessageEvent('message', {
              data: JSON.stringify({
                type: 'frame',
                data: canvas.toDataURL('image/jpeg').split(',')[1],
                metadata: { deviceWidth: 500 },
              }),
            }),
          );
        }, 20);
      }
      close() {
        clearTimeout(this.timer);
      }
    }
    // Keep the application EventSource API; replace only these two fixture streams.
    function source(url: string | URL, options?: EventSourceInit) {
      const path = new URL(url, window.location.origin).pathname;
      return /^\/api\/browser\/sessions\/chatgpt-[a-f0-9]{24}\/stream$/.test(path)
        ? new FixtureSource()
        : new NativeSource(url, options);
    }
    Object.defineProperty(window, 'EventSource', { configurable: true, value: source });
  });
  await context.route(
    (url) => url.pathname.startsWith('/api/'),
    async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const path = url.pathname;
      let json: unknown = {};
      if (path === '/api/me')
        json = {
          identity_id: 'browser-fixture',
          name: 'operator@example.com',
          capabilities: ['identity.create', 'identity.delete', 'agent.run', 'governance.write'],
        };
      if (path === '/api/auth/config')
        json = { provider: 'authula', auth_base_path: '/auth', bootstrap_required: false };
      if (path === '/api/conversations' || path === '/api/approvals') json = [];
      if (path === '/api/settings')
        json = {
          restart_required: false,
          settings: Object.entries(profile).map(([key, value]) => ({
            key,
            value,
            label: key,
            kind: 'string',
            secret: false,
            has_value: true,
            overridden: true,
            applied: 'live',
          })),
        };
      if (path === '/api/settings/llm-routes')
        json = {
          routes: [
            {
              provider: profile.AURA_LLM_PROVIDER,
              base_url: profile.AURA_LLM_BASE_URL,
              model: profile.AURA_LLM_MODEL,
            },
          ],
        };
      if (path === '/api/settings/llm-models')
        json = {
          models:
            url.searchParams.get('provider') === 'chatgpt'
              ? [
                  {
                    id: 'account-reasoning',
                    display_name: 'Account reasoning',
                    context_window: 128_000,
                    has_price: false,
                  },
                ]
              : [{ id: 'cloud/model', has_price: false }],
        };
      if (path.endsWith('-models') && path !== '/api/settings/llm-models') json = { models: [] };
      if (path === '/api/settings/chatgpt/status')
        json = {
          connected,
          plan_enabled: connected,
          email: connected ? 'operator@example.com' : '',
          status: pending ? 'authorization_required' : connected ? 'approved' : 'disconnected',
        };
      if (path === '/api/settings/chatgpt/login') {
        mutationKeys.push(request.headers()['idempotency-key'] ?? '');
        if (request.method() === 'POST') {
          activeRoute = `/browser/${sessions[loginCount++] ?? sessions[0]}`;
          if (immediateApproval) {
            connected = true;
            pending = false;
            json = { auth_url: '', status: 'approved' };
          } else {
            pending = true;
            json = { auth_url: activeRoute, status: 'authorization_required' };
          }
        } else {
          expect(request.method()).toBe('DELETE');
          const body = request.postDataJSON() as { auth_url: string };
          cancellations.push(body.auth_url);
          if (body.auth_url === activeRoute) pending = false;
          json = { cancelled: body.auth_url === activeRoute };
        }
      }
      if (/^\/api\/browser\/sessions\/chatgpt-[a-f0-9]{24}\/input$/.test(path)) {
        const event = request.postDataJSON() as { eventType: string };
        if (event.eventType === 'mouseReleased') {
          connected = true;
          pending = false;
        }
      }
      if (path === '/api/settings/llm-profile') {
        expect(request.method()).toBe('PUT');
        const body = request.postDataJSON() as { settings: typeof profile };
        expect(body.settings).toEqual({
          AURA_LLM_PROVIDER: 'chatgpt',
          AURA_LLM_BASE_URL: 'https://api.openai.com/v1',
          AURA_LLM_MODEL: 'account-reasoning',
        });
        profile = body.settings;
        writes.push(profile);
        json = { updated: 3, restart_required: false };
      }
      if (path === '/api/settings/chatgpt') {
        expect(request.method()).toBe('DELETE');
        mutationKeys.push(request.headers()['idempotency-key'] ?? '');
        connected = false;
        pending = false;
        json = { disconnected: true };
      }
      await route.fulfill({ json });
    },
  );
  return { writes, cancellations, mutationKeys, sessions };
}

async function openLogin(page: Page) {
  const popupPromise = page.waitForEvent('popup');
  await page.getByRole('button', { name: 'Continue with ChatGPT' }).click();
  const popup = await popupPromise;
  await expect(popup).toHaveURL(/\/browser\/chatgpt-[a-f0-9]{24}$/);
  expect(new URL(popup.url()).origin).toBe(new URL(page.url()).origin);
  await expect(popup.getByRole('heading', { name: 'Live browser' })).toBeVisible();
  await expect(
    popup.getByRole('img', { name: 'The page open in your sandbox browser' }),
  ).toBeVisible();
  return popup;
}

test('ChatGPT VM sign-in saves an account slug and restores its route', async ({
  page,
  context,
}) => {
  const fixture = await installFixture(context);
  await gotoAuthenticated(page, '/?settings=model');
  await expect(page.getByRole('heading', { name: 'Model routing' })).toBeVisible();
  await page.getByRole('radio', { name: 'ChatGPT' }).click();
  const model = page.getByRole('combobox', { name: 'Primary model', exact: true });
  await expect(model).toBeDisabled();
  const popup = await openLogin(page);
  const closed = popup.waitForEvent('close', { timeout: 10_000 });
  await popup.getByRole('application').click();
  await expect(page.getByText('ChatGPT connected')).toBeVisible();
  await closed;
  expect(fixture.cancellations).toHaveLength(0);
  await expect(model).toBeEnabled();
  await page.getByRole('radio', { name: 'ChatGPT' }).click();
  await expect(model).toBeEnabled();
  await model.click();
  await page.getByRole('option', { name: /Account reasoning/ }).click();
  await page.getByRole('button', { name: 'Save runtime settings' }).click();
  await expect(page.getByText('Runtime settings saved.')).toBeVisible();
  expect(fixture.writes).toHaveLength(1);
  await page.reload({ waitUntil: 'domcontentloaded' });
  await expect(page.getByRole('combobox', { name: 'Primary model', exact: true })).toHaveText(
    /Account reasoning/,
  );
  await page.getByRole('radio', { name: 'Cloud' }).click();
  await page.getByRole('radio', { name: 'ChatGPT' }).click();
  await expect(page.getByRole('combobox', { name: 'Primary model', exact: true })).toHaveText(
    /Account reasoning/,
  );
  await page.getByRole('button', { name: 'Disconnect ChatGPT' }).click();
  await expect(page.getByRole('combobox', { name: 'Primary model', exact: true })).toBeDisabled();
  expect(fixture.mutationKeys).toHaveLength(2);
  for (const key of fixture.mutationKeys) expect(key).toMatch(/^[\da-f-]{36}$/);
  expect(fixture.mutationKeys[0]).not.toBe(fixture.mutationKeys[1]);
});

test('closing the VM popup or cancelling Settings ends only that pending sign-in', async ({
  page,
  context,
}) => {
  const fixture = await installFixture(context);
  await gotoAuthenticated(page, '/?settings=model');
  await page.getByRole('radio', { name: 'ChatGPT' }).click();
  const firstPopup = await openLogin(page);
  await firstPopup.close();
  await expect(
    page.getByText('Sign-in cancelled. Continue with ChatGPT to try again.'),
  ).toBeVisible();
  await expect.poll(() => fixture.cancellations).toEqual([`/browser/${fixture.sessions[0]}`]);
  const secondPopup = await openLogin(page);
  const closed = secondPopup.waitForEvent('close', { timeout: 10_000 });
  await page.getByRole('button', { name: 'Cancel sign-in' }).click();
  await closed;
  await expect
    .poll(() => fixture.cancellations)
    .toEqual(fixture.sessions.map((session) => `/browser/${session}`));
  await expect(page.getByRole('combobox', { name: 'Primary model', exact: true })).toBeDisabled();
});

test('a returning approved account closes the blank popup and loads its catalog', async ({
  page,
  context,
}) => {
  const fixture = await installFixture(context, true);
  await gotoAuthenticated(page, '/?settings=model');
  await page.getByRole('radio', { name: 'ChatGPT' }).click();
  const popupPromise = page.waitForEvent('popup');
  await page.getByRole('button', { name: 'Continue with ChatGPT' }).click();
  const popup = await popupPromise;
  await expect.poll(() => popup.isClosed()).toBe(true);
  await expect(page.getByText('ChatGPT connected')).toBeVisible();
  const model = page.getByRole('combobox', { name: 'Primary model', exact: true });
  await expect(model).toBeEnabled();
  await model.click();
  await page.getByRole('option', { name: /Account reasoning/ }).click();
  await expect(model).toHaveText(/Account reasoning/);
  expect(fixture.cancellations).toHaveLength(0);
  expect(fixture.mutationKeys).toHaveLength(1);
});
