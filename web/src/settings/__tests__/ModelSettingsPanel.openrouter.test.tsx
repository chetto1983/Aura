import type { ReactElement } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';
import type { ModelSettingsGroup } from '../modelSettingsDefs';

// The routing pane's OpenRouter rows on the Cloud route: the management key an admin types, and
// the monthly cap of the services key Aura mints from it. The services key itself is minted by
// Aura and the API refuses to write it, so the pane never shows it, although the daemon lists it.

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

const MANAGEMENT_KEY_LABEL = 'OpenRouter management key';
const SERVICES_CAP_LABEL = 'Services key monthly cap (USD)';

function requestURL(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  return input instanceof URL ? input.href : input.url;
}

function stubSettingsAPI(putPayload: (url: string) => unknown = () => ({ ok: true })): FetchCall[] {
  const calls: FetchCall[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = requestURL(input);
      const method = init?.method ?? 'GET';
      const body = typeof init?.body === 'string' ? init.body : undefined;
      calls.push({ url, method, body });
      const payload = method === 'PUT' ? putPayload(url) : ROUTING_LIST;
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

describe('ModelSettingsPanel OpenRouter rows', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  // The wizard mounts the panel with no `groups` prop at all, so its case passes none.
  it.each<[string, { readonly groups?: readonly ModelSettingsGroup[] }]>([
    ['the Settings routing pane', { groups: ['routing'] }],
    ['the first-run wizard', {}],
  ])('saves the services cap, then the management key, in %s', async (_view, props) => {
    const calls = stubSettingsAPI();
    await mount(<ModelSettingsPanel {...props} onComplete={vi.fn()} />);

    fireEvent.change(screen.getByLabelText(MANAGEMENT_KEY_LABEL), {
      target: { value: 'sk-or-v1-mgmt' },
    });
    fireEvent.change(screen.getByLabelText(SERVICES_CAP_LABEL), { target: { value: '10' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    expect(await screen.findByText('Runtime settings saved.')).toBeTruthy();
    // Neither row is in the hot profile batch, so each gets its own PUT. The cap goes first: the
    // management key's PUT runs the reconciler, which then finds the cap and mints both keys.
    expect(calls.filter((call) => call.method === 'PUT')).toEqual([
      {
        url: '/api/settings/AURA_OPENROUTER_SERVICES_CAP_USD',
        method: 'PUT',
        body: JSON.stringify({ value: '10' }),
      },
      {
        url: '/api/settings/AURA_OPENROUTER_MANAGEMENT_KEY',
        method: 'PUT',
        body: JSON.stringify({ value: 'sk-or-v1-mgmt' }),
      },
    ]);
  });

  it('shows the OpenRouter rows only while Cloud is the route', async () => {
    stubSettingsAPI();
    await mount(<ModelSettingsPanel groups={['routing']} />);
    const shown = () =>
      [MANAGEMENT_KEY_LABEL, SERVICES_CAP_LABEL].map(
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

  it('never shows the services key Aura mints', async () => {
    stubSettingsAPI();
    await mount(<ModelSettingsPanel groups={['routing']} />);
    expect(screen.queryByLabelText('OpenRouter API key')).toBeNull();
  });
});
