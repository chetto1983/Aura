import type { ReactElement } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { chooseOption, optionNames, shownOption } from '../../test/chooseOption';
import { ModelSettingsPanel } from '../ModelSettingsPanel';

// The routing pane's OpenRouter rows on the Cloud route: the management key an admin types, and
// the monthly cap of the services key Aura mints from it. The services key itself is minted by
// Aura and the API refuses to write it, so the pane never shows it, although the daemon lists it.

interface FetchCall {
  readonly url: string;
  readonly method: string;
  readonly body: string | undefined;
}

interface StoredRow {
  readonly key: string;
  readonly value: string;
  readonly secret: boolean;
}

// An OpenRouter route with no AURA_LLM_PROVIDER row: the panel resolves Cloud from the URL.
const ROUTING_ROWS: readonly StoredRow[] = [
  { key: 'AURA_LLM_MODEL', value: 'deepseek/deepseek-v4-flash:nitro', secret: false },
  { key: 'AURA_LLM_BASE_URL', value: 'https://openrouter.ai/api/v1', secret: false },
  { key: 'OPENROUTER_API_KEY', value: '', secret: true },
];

function routingList(rows: readonly StoredRow[]) {
  return {
    restart_required: false,
    settings: rows.map((row) => ({
      ...row,
      label: row.key,
      kind: 'string',
      has_value: true,
      overridden: true,
    })),
  };
}

const MANAGEMENT_KEY_LABEL = 'OpenRouter management key';
const SERVICES_CAP_LABEL = 'Services key monthly cap (USD)';
const SORT_LABEL = 'OpenRouter: sort providers by';

function requestURL(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  return input instanceof URL ? input.href : input.url;
}

function stubSettingsAPI(rows: readonly StoredRow[] = ROUTING_ROWS): FetchCall[] {
  const calls: FetchCall[] = [];
  const list = routingList(rows);
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = requestURL(input);
      const method = init?.method ?? 'GET';
      const body = typeof init?.body === 'string' ? init.body : undefined;
      calls.push({ url, method, body });
      const payload = method === 'PUT' ? { ok: true } : list;
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

  it('saves the services cap, then the management key, each as its own row', async () => {
    const calls = stubSettingsAPI();
    await mount(<ModelSettingsPanel groups={['routing']} onComplete={vi.fn()} />);

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

  // Image and video generation bill on OpenRouter whatever the chat runs on, so the key that
  // mints every person's own key stays reachable on a local chat route too.
  it('shows the OpenRouter rows on every route', async () => {
    stubSettingsAPI();
    await mount(<ModelSettingsPanel groups={['routing']} />);
    const shown = () =>
      [MANAGEMENT_KEY_LABEL, SERVICES_CAP_LABEL].map(
        (label) => screen.queryByLabelText(label) !== null,
      );

    for (const route of ['Cloud', 'Local', 'Ollama']) {
      fireEvent.click(screen.getByRole('radio', { name: route }));
      expect(shown()).toEqual([true, true]);
    }
  });

  // Measured on the lab VM 2026-11-28: OpenRouter's price-weighted default kept a long thread on
  // a provider that took 23-46 s. The sort rides the hot profile, so a save reaches the next call.
  it('saves the provider sort through the hot-profile batch', async () => {
    const calls = stubSettingsAPI();
    await mount(<ModelSettingsPanel groups={['routing']} onComplete={vi.fn()} />);

    expect(await optionNames(SORT_LABEL)).toEqual([
      'OpenRouter default',
      'Price',
      'Throughput',
      'Latency',
    ]);
    fireEvent.click(screen.getByRole('option', { name: 'Throughput' }));
    expect(screen.getByText(/Throughput is not :nitro/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    expect(await screen.findByText('Runtime settings saved.')).toBeTruthy();
    expect(calls.filter((call) => call.method === 'PUT')).toEqual([
      {
        url: '/api/settings/llm-profile',
        method: 'PUT',
        body: JSON.stringify({ settings: { AURA_OPENROUTER_PROVIDER_SORT: 'throughput' } }),
      },
    ]);
  });

  // The select draws "" through a stand-in value (Radix reserves the empty string), so going back
  // to OpenRouter's default must still send "" and never the stand-in.
  it('sends an empty sort when the choice goes back to the OpenRouter default', async () => {
    const calls = stubSettingsAPI([
      ...ROUTING_ROWS,
      { key: 'AURA_OPENROUTER_PROVIDER_SORT', value: 'throughput', secret: false },
    ]);
    await mount(<ModelSettingsPanel groups={['routing']} onComplete={vi.fn()} />);

    expect(shownOption(SORT_LABEL)).toBe('Throughput');
    await chooseOption(SORT_LABEL, 'OpenRouter default');
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    expect(await screen.findByText('Runtime settings saved.')).toBeTruthy();
    expect(calls.filter((call) => call.method === 'PUT')).toEqual([
      {
        url: '/api/settings/llm-profile',
        method: 'PUT',
        body: JSON.stringify({ settings: { AURA_OPENROUTER_PROVIDER_SORT: '' } }),
      },
    ]);
  });

  it('never shows the services key Aura mints', async () => {
    stubSettingsAPI();
    await mount(<ModelSettingsPanel groups={['routing']} />);
    expect(screen.queryByLabelText('OpenRouter API key')).toBeNull();
  });
});
