import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';
import {
  CHATGPT_BASE_URL,
  PROVIDER_OPTIONS,
  resolveProvider,
  routeForProvider,
} from '../modelSettingsDefs';

const MODELS = [
  {
    id: 'account-reasoning',
    display_name: 'Account reasoning',
    context_window: 128_000,
    has_price: false,
  },
];
const CLOUD = {
  provider: 'openrouter',
  base_url: 'https://openrouter.ai/api/v1',
  model: 'cloud/model',
};

function urlOf(input: RequestInfo | URL): string {
  return typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
}

function json(body: unknown) {
  return new Response(JSON.stringify(body), { status: 200 });
}

function setup({
  connected = true,
  savedModel = 'account-reasoning',
  savedProvider = 'chatgpt',
} = {}) {
  let connection = connected;
  let route =
    savedProvider === 'chatgpt'
      ? { provider: 'chatgpt', base_url: CHATGPT_BASE_URL, model: savedModel }
      : CLOUD;
  let memory =
    savedModel === ''
      ? [CLOUD]
      : [CLOUD, { provider: 'chatgpt', base_url: CHATGPT_BASE_URL, model: savedModel }];
  const writes: Record<string, string>[] = [];
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = urlOf(input);
    if (url === '/api/settings/chatgpt') {
      connection = false;
      return Promise.resolve(json({ disconnected: true }));
    }
    if (url === '/api/settings/chatgpt/status')
      return Promise.resolve(
        json({
          connected: connection,
          email: 'operator@example.com',
          plan_enabled: connection,
          status: connection ? 'approved' : 'disconnected',
        }),
      );
    if (url === '/api/settings/llm-routes') return Promise.resolve(json({ routes: memory }));
    if (url.startsWith('/api/settings/llm-models'))
      return Promise.resolve(
        json({
          models: url.includes('provider=chatgpt')
            ? MODELS
            : [{ id: CLOUD.model, has_price: false }],
        }),
      );
    if (url === '/api/settings/llm-profile') {
      if (typeof init?.body !== 'string') throw new Error('Expected a JSON profile body');
      const { settings } = JSON.parse(init.body) as { settings: Record<string, string> };
      writes.push(settings);
      route = {
        provider: settings.AURA_LLM_PROVIDER ?? route.provider,
        base_url: settings.AURA_LLM_BASE_URL ?? route.base_url,
        model: settings.AURA_LLM_MODEL ?? route.model,
      };
      memory = [...memory.filter((row) => row.provider !== route.provider), route];
      return Promise.resolve(
        json({ updated: Object.keys(settings).length, restart_required: false }),
      );
    }
    if (url !== '/api/settings') return Promise.resolve(json({ models: [] }));
    const rows = {
      AURA_LLM_PROVIDER: route.provider,
      AURA_LLM_BASE_URL: route.base_url,
      AURA_LLM_MODEL: route.model,
    };
    return Promise.resolve(
      json({
        restart_required: false,
        settings: Object.entries(rows).map(([key, value]) => ({
          key,
          value,
          kind: 'string',
          secret: false,
          has_value: value !== '',
          overridden: true,
          applied: 'live',
        })),
      }),
    );
  });
  vi.stubGlobal('fetch', fetchMock);
  const renderPanel = () =>
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <ModelSettingsPanel groups={['routing']} />
      </QueryClientProvider>,
    );
  const view = renderPanel();
  return { fetchMock, writes, view, renderPanel };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('ChatGPT provider and account model selection', () => {
  it('restores the saved account slug, displays its name and preserves route memory across switches', async () => {
    setup();
    await screen.findByText('operator@example.com');
    const model = screen.getByLabelText('Primary model');
    await waitFor(() => {
      expect(model.textContent).toContain('Account reasoning');
    });
    expect(screen.queryByLabelText('Primary base URL')).toBeNull();
    fireEvent.click(screen.getByRole('radio', { name: 'Cloud' }));
    expect(screen.queryByRole('button', { name: 'Disconnect ChatGPT' })).toBeNull();
    expect(screen.getByLabelText<HTMLInputElement>('Primary base URL').value).toBe(CLOUD.base_url);
    fireEvent.click(screen.getByRole('radio', { name: 'ChatGPT' }));
    await waitFor(() => {
      expect(screen.getByLabelText('Primary model').textContent).toContain('Account reasoning');
    });
  });

  it('keeps the catalog ready when the already selected ChatGPT provider is clicked', async () => {
    setup();
    const model = await screen.findByLabelText<HTMLButtonElement>('Primary model');
    await waitFor(() => {
      expect(model.textContent).toContain('Account reasoning');
    });
    fireEvent.click(screen.getByRole('radio', { name: 'ChatGPT' }));
    expect(model.disabled).toBe(false);
    expect(
      screen.getByRole<HTMLButtonElement>('button', { name: 'Save runtime settings' }).disabled,
    ).toBe(false);
  });

  it('requires selecting an account model then saves its slug and fixed route', async () => {
    const { writes, fetchMock } = setup({ savedProvider: 'openrouter', savedModel: '' });
    fireEvent.click(await screen.findByRole('radio', { name: 'ChatGPT' }));
    await screen.findByText('operator@example.com');
    const save = screen.getByRole<HTMLButtonElement>('button', { name: 'Save runtime settings' });
    expect(save.disabled).toBe(true);
    const model = screen.getByLabelText<HTMLButtonElement>('Primary model');
    await waitFor(() => {
      expect(model.disabled).toBe(false);
    });
    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([input]) => urlOf(input).includes('provider=chatgpt')),
      ).toBe(true);
    });
    fireEvent.click(model);
    const option = await screen.findByRole('option', { name: /Account reasoning/ });
    expect(option.textContent).toContain('ChatGPT plan limits apply');
    fireEvent.click(option);
    await waitFor(() => {
      expect(save.disabled).toBe(false);
    });
    fireEvent.click(save);
    await waitFor(() => {
      expect(writes).toHaveLength(1);
    });
    expect(writes[0]).toEqual({
      AURA_LLM_PROVIDER: 'chatgpt',
      AURA_LLM_BASE_URL: CHATGPT_BASE_URL,
      AURA_LLM_MODEL: 'account-reasoning',
    });
  });

  it('does not query or expose selectable account models while disconnected', async () => {
    const { fetchMock } = setup({ connected: false });
    await screen.findByText('Connect ChatGPT to see the models available with your plan.');
    expect(screen.getByLabelText<HTMLButtonElement>('Primary model').disabled).toBe(true);
    expect(
      screen.getByRole<HTMLButtonElement>('button', { name: 'Save runtime settings' }).disabled,
    ).toBe(true);
    expect(fetchMock.mock.calls.some(([input]) => urlOf(input).includes('provider=chatgpt'))).toBe(
      false,
    );
  });

  it('clears the account catalog after disconnect and refuses saving the stale model', async () => {
    setup();
    await waitFor(() => {
      expect(screen.getByLabelText('Primary model').textContent).toContain('Account reasoning');
    });
    fireEvent.click(screen.getByRole('button', { name: 'Disconnect ChatGPT' }));
    await screen.findByText('Connect ChatGPT to see the models available with your plan.');
    await waitFor(() => {
      expect(screen.getByLabelText<HTMLButtonElement>('Primary model').disabled).toBe(true);
    });
    expect(screen.getByLabelText('Primary model').textContent).not.toContain('Account reasoning');
  });

  it('has no invented ChatGPT model default and respects the explicit provider', () => {
    const option = PROVIDER_OPTIONS.find((row) => row.id === 'chatgpt');
    if (option === undefined) throw new Error('ChatGPT provider missing');
    expect(resolveProvider('chatgpt', CHATGPT_BASE_URL)).toBe('chatgpt');
    expect(
      routeForProvider(option, [], {
        provider: 'ollama',
        baseURL: 'http://localhost:11434/v1',
        model: 'other',
      }),
    ).toEqual({ baseURL: CHATGPT_BASE_URL, model: '', source: 'fallback' });
  });
});
