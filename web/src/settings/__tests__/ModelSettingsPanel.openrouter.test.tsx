import type { ReactElement } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';
import type { ModelSettingsGroup } from '../modelSettingsDefs';

// The routing pane's two OpenRouter credentials: the inference key, and the management key
// that mints each identity's own key and feeds the spend overview. Both belong to the Cloud
// route alone, and the management key is read once at boot, so it is saved as its own row.

interface FetchCall {
  readonly url: string;
  readonly method: string;
  readonly body: string | undefined;
}

// An OpenRouter route with no AURA_LLM_PROVIDER row: the panel resolves Cloud from the URL.
const ROUTING_LIST = {
  restart_required: false,
  settings: [
    { key: 'AURA_LLM_MODEL', value: 'deepseek/deepseek-v4-flash:nitro', secret: false },
    { key: 'AURA_LLM_BASE_URL', value: 'https://openrouter.ai/api/v1', secret: false },
    { key: 'OPENROUTER_API_KEY', value: '', secret: true },
  ].map((row) => ({ ...row, label: row.key, kind: 'string', has_value: true, overridden: true })),
};

function requestURL(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  return input instanceof URL ? input.href : input.url;
}

function stubSettingsAPI(): FetchCall[] {
  const calls: FetchCall[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const method = init?.method ?? 'GET';
      const body = typeof init?.body === 'string' ? init.body : undefined;
      calls.push({ url: requestURL(input), method, body });
      const payload = method === 'PUT' ? { ok: true } : ROUTING_LIST;
      return Promise.resolve(
        new Response(JSON.stringify(payload), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }),
  );
  return calls;
}

function mount(panel: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}>{panel}</QueryClientProvider>);
  return screen.findByRole('heading', { name: 'Model routing' });
}

describe('ModelSettingsPanel OpenRouter credentials', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // The wizard mounts the panel with no `groups` prop at all, so its case passes none.
  it.each<[string, { readonly groups?: readonly ModelSettingsGroup[] }]>([
    ['the Settings routing pane', { groups: ['routing'] }],
    ['the first-run wizard', {}],
  ])(
    'shows the OpenRouter management key in %s and saves it as its own boot-bound row',
    async (_view, props) => {
      const calls = stubSettingsAPI();
      await mount(<ModelSettingsPanel {...props} onComplete={vi.fn()} />);

      fireEvent.change(screen.getByLabelText('OpenRouter management key'), {
        target: { value: 'sk-or-v1-mgmt' },
      });
      fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

      expect(await screen.findByText('Runtime settings saved.')).toBeTruthy();
      // The daemon reads it once at boot to wire key minting and the spend overview, so it
      // is not a hot profile row: it gets its own PUT and the restart banner applies it.
      expect(calls.filter((call) => call.method === 'PUT')).toEqual([
        {
          url: '/api/settings/AURA_OPENROUTER_MANAGEMENT_KEY',
          method: 'PUT',
          body: JSON.stringify({ value: 'sk-or-v1-mgmt' }),
        },
      ]);
    },
  );

  it('shows the OpenRouter credentials only while Cloud is the route', async () => {
    stubSettingsAPI();
    await mount(<ModelSettingsPanel groups={['routing']} />);
    const shown = () =>
      ['OpenRouter API key', 'OpenRouter management key'].map(
        (label) => screen.queryByLabelText(label) !== null,
      );

    expect(shown()).toEqual([true, true]);
    for (const route of ['Local', 'Ollama']) {
      fireEvent.click(screen.getByRole('button', { name: route }));
      expect(shown()).toEqual([false, false]);
    }
    fireEvent.click(screen.getByRole('button', { name: 'Cloud' }));
    expect(shown()).toEqual([true, true]);
  });
});
