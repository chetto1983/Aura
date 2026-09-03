import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';
import {
  LOCAL_BASE_URL,
  OLLAMA_BASE_URL,
  OLLAMA_MODEL,
  PROVIDER_OPTIONS,
  routeForProvider,
} from '../modelSettingsDefs';

// The measured bug: the stored local route is host.docker.internal, the constant compiled
// into the bundle is aura-llm, and a round trip Cloud -> Local used to overwrite the first
// with the second.
const STORED_LOCAL_URL = 'http://host.docker.internal:8084/v1';
const STORED_CLOUD_MODEL = 'z-ai/glm-5.3';

const SETTINGS_BODY = {
  restart_required: false,
  restart_keys: [],
  settings: [
    {
      key: 'AURA_LLM_PROVIDER',
      label: 'Primary provider',
      kind: 'string',
      secret: false,
      value: 'llamacpp',
      has_value: true,
      overridden: true,
      applied: 'live',
    },
    {
      key: 'AURA_LLM_BASE_URL',
      label: 'Primary LLM base URL',
      kind: 'string',
      secret: false,
      value: STORED_LOCAL_URL,
      has_value: true,
      overridden: true,
      applied: 'live',
    },
    {
      key: 'AURA_LLM_MODEL',
      label: 'Primary LLM model',
      kind: 'string',
      secret: false,
      value: 'gemma-4-12b',
      has_value: true,
      overridden: true,
      applied: 'live',
    },
  ],
};

const ROUTES_BODY = {
  routes: [
    { provider: 'llamacpp', base_url: STORED_LOCAL_URL, model: 'gemma-4-12b' },
    { provider: 'openrouter', base_url: 'https://openrouter.ai/api/v1', model: STORED_CLOUD_MODEL },
  ],
};

function stubRoutedFetch() {
  const calls: string[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      calls.push(url);
      const body = url.startsWith('/api/settings/llm-routes')
        ? ROUTES_BODY
        : url.startsWith('/api/settings/llm-models')
          ? { models: [] }
          : SETTINGS_BODY;
      return Promise.resolve(
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );
    }),
  );
  return calls;
}

function renderPanel() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ModelSettingsPanel groups={['routing']} />
    </QueryClientProvider>,
  );
}

describe('provider switching reads the route from the daemon', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('restores each provider’s remembered route instead of a compiled-in constant', async () => {
    stubRoutedFetch();
    renderPanel();

    const baseURL = await screen.findByLabelText<HTMLInputElement>('Primary base URL');
    const model = screen.getByLabelText('Primary model');
    expect(baseURL.value).toBe(STORED_LOCAL_URL);

    fireEvent.click(screen.getByRole('button', { name: 'Cloud' }));
    await waitFor(() => {
      expect(baseURL.value).toBe('https://openrouter.ai/api/v1');
    });
    expect(model.textContent).toContain(STORED_CLOUD_MODEL);

    fireEvent.click(screen.getByRole('button', { name: 'Local' }));
    await waitFor(() => {
      expect(baseURL.value).toBe(STORED_LOCAL_URL);
    });
    expect(baseURL.value).not.toBe(LOCAL_BASE_URL);
    expect(model.textContent).toContain('gemma-4-12b');
  });

  it('falls back to the built-in route only for a provider nobody has configured', async () => {
    stubRoutedFetch();
    renderPanel();

    const baseURL = await screen.findByLabelText<HTMLInputElement>('Primary base URL');
    fireEvent.click(screen.getByRole('button', { name: 'Ollama' }));
    await waitFor(() => {
      expect(baseURL.value).toBe(OLLAMA_BASE_URL);
    });
    expect(screen.getByLabelText('Primary model').textContent).toContain(OLLAMA_MODEL);
  });

  it('asks the daemon for the route memory', async () => {
    const calls = stubRoutedFetch();
    renderPanel();
    await screen.findByLabelText('Primary base URL');
    await waitFor(() => {
      expect(calls.some((url) => url.startsWith('/api/settings/llm-routes'))).toBe(true);
    });
  });
});

describe('routeForProvider precedence', () => {
  const local = PROVIDER_OPTIONS.find((option) => option.id === 'local');
  if (local === undefined) throw new Error('the local provider option must exist');

  it('prefers the stored row', () => {
    expect(
      routeForProvider(local, [{ provider: 'llamacpp', base_url: STORED_LOCAL_URL, model: 'm' }], {
        provider: 'openrouter',
        baseURL: 'https://openrouter.ai/api/v1',
        model: 'z-ai/glm-5.3',
      }),
    ).toEqual({ baseURL: STORED_LOCAL_URL, model: 'm', source: 'stored' });
  });

  it('falls back to the loaded route when the memory has no row for that provider', () => {
    // A daemon routed by environment alone has never written a row; the route it is
    // serving is still a real one and beats the constant.
    expect(
      routeForProvider(local, [], {
        provider: '',
        baseURL: STORED_LOCAL_URL,
        model: 'gemma-4-12b',
      }),
    ).toEqual({ baseURL: STORED_LOCAL_URL, model: 'gemma-4-12b', source: 'loaded' });
  });

  it('uses the constant only when nothing else knows the provider', () => {
    expect(
      routeForProvider(local, [], {
        provider: 'openrouter',
        baseURL: 'https://openrouter.ai/api/v1',
        model: 'z-ai/glm-5.3',
      }).source,
    ).toBe('fallback');
  });

  it('ignores a half-empty stored row', () => {
    expect(
      routeForProvider(local, [{ provider: 'llamacpp', base_url: STORED_LOCAL_URL, model: '  ' }], {
        provider: '',
        baseURL: '',
        model: '',
      }).source,
    ).toBe('fallback');
  });
});
