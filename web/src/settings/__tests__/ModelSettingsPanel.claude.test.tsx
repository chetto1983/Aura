import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import i18n from '../../i18n/i18n';
import { chooseOption } from '../../test/chooseOption';
import { ModelSettingsPanel } from '../ModelSettingsPanel';
import {
  CLAUDE_BASE_URL,
  PROVIDER_OPTIONS,
  resolveProvider,
  routeForProvider,
} from '../modelSettingsDefs';

// The Claude route on the routing pane: the house key the admin types (sealed, never shown), and
// the account's models, which the daemon can only list once that key is stored. A first setup
// therefore saves the key alone, then the route once a model is picked.

const FAKE_KEY = 'sk-ant-test-fake';
const KEY_LABEL = 'Claude API key';
const SAVE = 'Save runtime settings';
const SORT_LABEL = 'OpenRouter: sort providers by';
const KEY_SAVED =
  "Claude API key saved. Pick a model from the account's list, then save again to switch the chat to Claude.";
const ROUTE_HELD =
  'Saved, except the model route: once the account lists the model you pick, save again.';
const NO_KEY_REFUSAL = 'add the Claude Console API key in Settings before selecting a Claude model';
const CLOUD = {
  provider: 'openrouter',
  base_url: 'https://openrouter.ai/api/v1',
  model: 'deepseek/deepseek-v4-flash:nitro',
};
const MODELS = [
  {
    id: 'claude-sonnet-5-5',
    display_name: 'Claude Sonnet 5.5',
    context_window: 1_000_000,
    input_per_1m: 2,
    output_per_1m: 10,
    has_price: true,
  },
  { id: 'claude-haiku-5-5', display_name: 'Claude Haiku 5.5', has_price: false },
];

interface Call {
  readonly method: string;
  readonly url: string;
  readonly body: unknown;
}

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status });
}

function row(key: string, value: string, secret = false, stored = value !== '') {
  return {
    key,
    label: key,
    value,
    kind: 'string',
    secret,
    has_value: stored,
    overridden: stored,
    applied: 'live',
  };
}

const MINTED = { services_label: 'sk-or-v1-abc...xyz', identities_minted: [], limits_aligned: [] };

function setup({ keyStored = false, onClaude = false, keyRefused = false } = {}) {
  let stored = keyStored;
  let refused = keyRefused;
  let route = onClaude
    ? { provider: 'anthropic', base_url: CLAUDE_BASE_URL, model: 'claude-sonnet-5-5' }
    : CLOUD;
  const calls: Call[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = urlOf(input);
      const method = init?.method ?? 'GET';
      const body: unknown = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined;
      calls.push({ method, url, body });
      if (url === '/api/settings/ANTHROPIC_API_KEY' && method === 'PUT') {
        stored = true;
        refused = false;
        return Promise.resolve(json(row('ANTHROPIC_API_KEY', '', true, true)));
      }
      if (url === '/api/settings/AURA_OPENROUTER_MANAGEMENT_KEY' && method === 'PUT')
        return Promise.resolve(
          json({
            ...row('AURA_OPENROUTER_MANAGEMENT_KEY', '', true, true),
            openrouter_keys: MINTED,
          }),
        );
      if (url === '/api/settings/ANTHROPIC_API_KEY' && method === 'DELETE') {
        stored = false;
        return Promise.resolve(
          json({ key: 'ANTHROPIC_API_KEY', deleted: true, restart_required: false }),
        );
      }
      if (url === '/api/settings/llm-profile') {
        const { settings } = body as { settings: Record<string, string> };
        const next = {
          provider: settings.AURA_LLM_PROVIDER ?? route.provider,
          base_url: settings.AURA_LLM_BASE_URL ?? route.base_url,
          model: settings.AURA_LLM_MODEL ?? route.model,
        };
        // The daemon prepares the whole merged route, so any batch on a Claude route needs the key.
        if (next.provider === 'anthropic' && !stored)
          return Promise.resolve(json({ error: NO_KEY_REFUSAL }, 400));
        route = next;
        return Promise.resolve(json({ updated: 3, restart_required: false }));
      }
      if (url === '/api/settings/llm-routes') return Promise.resolve(json({ routes: [CLOUD] }));
      if (url.startsWith('/api/settings/llm-models?provider=anthropic'))
        return Promise.resolve(
          !stored
            ? json({ error: 'no Claude API key is set' }, 502)
            : refused
              ? json({ error: 'Claude refused the API key' }, 502)
              : json({ models: MODELS }),
        );
      if (url !== '/api/settings') return Promise.resolve(json({ models: [] }));
      return Promise.resolve(
        json({
          restart_required: false,
          settings: [
            row('AURA_LLM_PROVIDER', route.provider),
            row('AURA_LLM_BASE_URL', route.base_url),
            row('AURA_LLM_MODEL', route.model),
            row('ANTHROPIC_API_KEY', '', true, stored),
          ],
        }),
      );
    }),
  );
  const onComplete = vi.fn();
  render(
    <QueryClientProvider
      client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
    >
      <ModelSettingsPanel groups={['routing']} onComplete={onComplete} />
    </QueryClientProvider>,
  );
  const writes = () => calls.filter((call) => call.method !== 'GET');
  const catalogueAsked = () =>
    calls.some((call) => call.url.startsWith('/api/settings/llm-models?provider=anthropic'));
  return { calls, writes, catalogueAsked, onComplete };
}

function keyField(): HTMLElement {
  const field = document.querySelector<HTMLElement>('[data-setting="ANTHROPIC_API_KEY"]');
  if (field === null) throw new Error('the Claude key field is not on the page');
  return field;
}

async function pickModel(name: RegExp) {
  const model = screen.getByLabelText<HTMLButtonElement>('Primary model');
  await waitFor(() => {
    expect(model.disabled).toBe(false);
  });
  fireEvent.click(model);
  fireEvent.click(await screen.findByRole('option', { name }));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('Claude route on the routing pane', () => {
  it('forces and hides the base URL, and shows the key only on the Claude route', async () => {
    const { catalogueAsked } = setup();
    fireEvent.click(await screen.findByRole('radio', { name: 'Cloud' }));
    expect(screen.queryByLabelText(KEY_LABEL)).toBeNull();
    expect(screen.getByLabelText('Primary base URL')).toBeTruthy();

    fireEvent.click(screen.getByRole('radio', { name: 'Claude' }));
    expect(screen.queryByLabelText('Primary base URL')).toBeNull();
    expect(within(keyField()).getByText('Not set')).toBeTruthy();
    expect(screen.getByLabelText<HTMLButtonElement>('Primary model').disabled).toBe(true);
    expect(screen.getByRole<HTMLButtonElement>('button', { name: SAVE }).disabled).toBe(true);
    expect(catalogueAsked()).toBe(false);
  });

  it('shows a stored key as set and never its value', async () => {
    setup({ keyStored: true, onClaude: true });
    const input = await screen.findByLabelText<HTMLInputElement>(KEY_LABEL);
    expect(input.value).toBe('');
    expect(within(keyField()).getByText('Configured')).toBeTruthy();
  });

  it('first setup saves the key alone, then the route once a model is picked', async () => {
    const { writes, catalogueAsked, onComplete } = setup();
    fireEvent.click(await screen.findByRole('radio', { name: 'Claude' }));
    fireEvent.change(screen.getByLabelText(KEY_LABEL), { target: { value: FAKE_KEY } });
    fireEvent.click(screen.getByRole('button', { name: SAVE }));

    await waitFor(() => {
      expect(within(keyField()).getByText('Configured')).toBeTruthy();
    });
    expect(writes()).toEqual([
      { method: 'PUT', url: '/api/settings/ANTHROPIC_API_KEY', body: { value: FAKE_KEY } },
    ]);
    expect(onComplete).not.toHaveBeenCalled();
    expect(await screen.findByText(KEY_SAVED)).toBeTruthy();
    expect(screen.queryByText('Runtime settings saved.')).toBeNull();
    expect(screen.getByLabelText<HTMLInputElement>(KEY_LABEL).value).toBe('');
    expect(document.body.innerHTML).not.toContain(FAKE_KEY);
    // The route switch survives the key-only save, so the pane is still on Claude.
    expect(screen.getByRole('radio', { name: 'Claude' }).getAttribute('data-state')).toBe('on');
    await waitFor(() => {
      expect(catalogueAsked()).toBe(true);
    });

    await pickModel(/Claude Sonnet 5\.5/);
    fireEvent.click(screen.getByRole('button', { name: SAVE }));
    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledTimes(1);
    });
    expect(writes().slice(1)).toEqual([
      {
        method: 'PUT',
        url: '/api/settings/llm-profile',
        body: {
          settings: {
            AURA_LLM_MODEL: 'claude-sonnet-5-5',
            AURA_LLM_BASE_URL: CLAUDE_BASE_URL,
            AURA_LLM_PROVIDER: 'anthropic',
          },
        },
      },
    ]);
  });

  it('with a stored key, saves the profile alone', async () => {
    const { writes } = setup({ keyStored: true });
    fireEvent.click(await screen.findByRole('radio', { name: 'Claude' }));
    await pickModel(/Claude Sonnet 5\.5/);
    fireEvent.click(screen.getByRole('button', { name: SAVE }));
    expect(await screen.findByText('Runtime settings saved.')).toBeTruthy();
    expect(writes().map((call) => call.url)).toEqual(['/api/settings/llm-profile']);
  });

  it('saves a replaced key before the profile that needs it', async () => {
    const { writes } = setup({ keyStored: true });
    fireEvent.click(await screen.findByRole('radio', { name: 'Claude' }));
    fireEvent.change(screen.getByLabelText(KEY_LABEL), { target: { value: FAKE_KEY } });
    await pickModel(/Claude Sonnet 5\.5/);
    fireEvent.click(screen.getByRole('button', { name: SAVE }));
    expect(await screen.findByText('Runtime settings saved.')).toBeTruthy();
    expect(writes().map((call) => call.url)).toEqual([
      '/api/settings/ANTHROPIC_API_KEY',
      '/api/settings/llm-profile',
    ]);
  });

  // The management key's PUT runs the reconciler even when the route waits for a model, so the
  // keys it minted reach the first-run step through the save that completes.
  it('reports what a held save minted when the route is saved', async () => {
    const { writes, onComplete } = setup();
    fireEvent.click(await screen.findByRole('radio', { name: 'Claude' }));
    fireEvent.change(screen.getByLabelText(KEY_LABEL), { target: { value: FAKE_KEY } });
    fireEvent.change(screen.getByLabelText('OpenRouter management key'), {
      target: { value: 'sk-or-v1-test-fake' },
    });
    fireEvent.click(screen.getByRole('button', { name: SAVE }));
    await waitFor(() => {
      expect(within(keyField()).getByText('Configured')).toBeTruthy();
    });
    expect(writes().map((call) => call.url)).toEqual([
      '/api/settings/ANTHROPIC_API_KEY',
      '/api/settings/AURA_OPENROUTER_MANAGEMENT_KEY',
    ]);
    expect(onComplete).not.toHaveBeenCalled();

    await pickModel(/Claude Sonnet 5\.5/);
    fireEvent.click(screen.getByRole('button', { name: SAVE }));
    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledTimes(1);
    });
    expect(onComplete).toHaveBeenCalledWith({ openRouterKeys: [MINTED] });
  });

  // A minted services key needs the restart the first-run step triggers, so Skip reports it too.
  it('reports what a held save minted when the step is skipped', async () => {
    const { onComplete } = setup();
    fireEvent.click(await screen.findByRole('radio', { name: 'Claude' }));
    fireEvent.change(screen.getByLabelText('OpenRouter management key'), {
      target: { value: 'sk-or-v1-test-fake' },
    });
    fireEvent.click(screen.getByRole('button', { name: SAVE }));
    // No key went out with it, so the message names the route, not the key.
    expect(await screen.findByText(ROUTE_HELD)).toBeTruthy();
    expect(onComplete).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Skip runtime setup' }));
    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledWith({ openRouterKeys: [MINTED] });
    });
  });

  // Only the route waits for a model the account lists: the rows beside it, the speed row in the
  // hot profile among them, save on their own.
  it('saves the rows beside a route that waits for its model', async () => {
    const { writes, onComplete } = setup();
    fireEvent.click(await screen.findByRole('radio', { name: 'Claude' }));
    const save = screen.getByRole<HTMLButtonElement>('button', { name: SAVE });
    expect(save.disabled).toBe(true);
    fireEvent.change(screen.getByLabelText('OpenRouter management key'), {
      target: { value: 'sk-or-v1-test-fake' },
    });
    await chooseOption(SORT_LABEL, 'Throughput');
    expect(save.disabled).toBe(false);
    fireEvent.click(save);
    await waitFor(() => {
      expect(writes()).toHaveLength(2);
    });
    expect(writes()).toEqual([
      {
        method: 'PUT',
        url: '/api/settings/AURA_OPENROUTER_MANAGEMENT_KEY',
        body: { value: 'sk-or-v1-test-fake' },
      },
      {
        method: 'PUT',
        url: '/api/settings/llm-profile',
        body: { settings: { AURA_OPENROUTER_PROVIDER_SORT: 'throughput' } },
      },
    ]);
    expect(onComplete).not.toHaveBeenCalled();
    expect(screen.getByRole('radio', { name: 'Claude' }).getAttribute('data-state')).toBe('on');
  });

  // A stored Claude route with no key refuses any profile batch, the speed row's included: the
  // rows beside it are written first, so the refusal costs only the speed row.
  it('writes the rows beside a stored route that refuses the speed row', async () => {
    const { writes } = setup({ onClaude: true });
    fireEvent.change(await screen.findByLabelText('OpenRouter management key'), {
      target: { value: 'sk-or-v1-test-fake' },
    });
    await chooseOption(SORT_LABEL, 'Throughput');
    fireEvent.click(screen.getByRole('button', { name: SAVE }));
    expect(await screen.findByText(new RegExp(NO_KEY_REFUSAL))).toBeTruthy();
    expect(writes().map((call) => call.url)).toEqual([
      '/api/settings/AURA_OPENROUTER_MANAGEMENT_KEY',
      '/api/settings/llm-profile',
    ]);
  });

  it('lists the models again once a refused key is replaced', async () => {
    setup({ keyStored: true, onClaude: true, keyRefused: true });
    expect(await screen.findByText(/Claude refused the API key/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText(KEY_LABEL), { target: { value: FAKE_KEY } });
    fireEvent.click(screen.getByRole('button', { name: SAVE }));
    expect(await screen.findByText('2 models published here')).toBeTruthy();
    expect(screen.queryByText(/Claude refused the API key/)).toBeNull();
  });

  it('drops a typed key when the operator leaves the Claude route', async () => {
    const { writes, onComplete } = setup();
    fireEvent.click(await screen.findByRole('radio', { name: 'Claude' }));
    fireEvent.change(screen.getByLabelText(KEY_LABEL), { target: { value: FAKE_KEY } });
    fireEvent.click(screen.getByRole('radio', { name: 'Cloud' }));
    // Back on the saved Cloud route nothing is left to write, so the button only continues.
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));
    await waitFor(() => {
      expect(onComplete).toHaveBeenCalledTimes(1);
    });
    expect(writes()).toEqual([]);
  });

  it('Reset deletes the key', async () => {
    const { writes } = setup({ keyStored: true, onClaude: true });
    await screen.findByLabelText(KEY_LABEL);
    fireEvent.click(within(keyField()).getByRole('button', { name: 'Reset' }));
    await waitFor(() => {
      expect(within(keyField()).getByText('Not set')).toBeTruthy();
    });
    expect(writes()).toEqual([
      { method: 'DELETE', url: '/api/settings/ANTHROPIC_API_KEY', body: undefined },
    ]);
  });

  it('lists only the account models, with no custom entry and no invented price', async () => {
    setup({ keyStored: true, onClaude: true });
    await screen.findByLabelText(KEY_LABEL);
    const model = screen.getByLabelText<HTMLButtonElement>('Primary model');
    await waitFor(() => {
      expect(model.disabled).toBe(false);
    });
    fireEvent.click(model);
    const haiku = await screen.findByRole('option', { name: /Claude Haiku 5\.5/ });
    expect(haiku.textContent).toContain('price not published');
    expect(haiku.textContent).not.toContain('no token charge');
    fireEvent.change(screen.getByPlaceholderText('Search models...'), {
      target: { value: 'claude-made-up' },
    });
    expect(screen.queryByText(/as typed/)).toBeNull();
  });

  it('speaks Italian and English', async () => {
    await i18n.changeLanguage('it');
    try {
      setup();
      fireEvent.click(await screen.findByRole('radio', { name: 'Claude' }));
      fireEvent.change(screen.getByLabelText('Chiave API di Claude'), {
        target: { value: FAKE_KEY },
      });
      fireEvent.click(screen.getByRole('button', { name: 'Salva impostazioni runtime' }));
      expect(
        await screen.findByText(
          'Chiave API di Claude salvata. Scegli un modello dall’elenco dell’account, poi salva di nuovo per passare la chat a Claude.',
        ),
      ).toBeTruthy();
    } finally {
      await i18n.changeLanguage('en');
    }
    expect(await screen.findByLabelText(KEY_LABEL)).toBeTruthy();
  });

  it('maps the anthropic provider to the Claude button with no invented model', () => {
    const option = PROVIDER_OPTIONS.find((candidate) => candidate.id === 'claude');
    if (option === undefined) throw new Error('Claude provider missing');
    expect(option.provider).toBe('anthropic');
    expect(resolveProvider('anthropic', CLAUDE_BASE_URL)).toBe('claude');
    expect(
      routeForProvider(option, [], { provider: 'openrouter', baseURL: CLOUD.base_url, model: 'x' }),
    ).toEqual({ baseURL: CLAUDE_BASE_URL, model: '', source: 'fallback' });
  });
});
