import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  checkTelegramAvailability,
  fetchLLMModels,
  fetchLLMRoutes,
  createTelegramLink,
  deleteSetting,
  fetchSettings,
  fetchTelegramLinkStatus,
  putLLMProfile,
  putSetting,
} from '../settingsApi';

describe('settingsApi', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reads the Postgres-backed settings list using same-origin credentials', async () => {
    const body = {
      restart_required: true,
      settings: [
        {
          key: 'AURA_LLM_MODEL',
          label: 'Primary LLM model',
          kind: 'string',
          secret: false,
          value: 'deepseek/deepseek-v4-flash:nitro',
          has_value: true,
          overridden: true,
        },
      ],
    };
    const fetchMock = vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify(body), { status: 200 })),
    );
    vi.stubGlobal('fetch', fetchMock);

    await expect(fetchSettings()).resolves.toEqual(body);
    expect(fetchMock).toHaveBeenCalledWith('/api/settings', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  });

  it('writes and deletes one encoded setting key at a time', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response('{"ok":true}', { status: 200 })));
    vi.stubGlobal('fetch', fetchMock);

    await putSetting('AURA_LLM_BASE_URL', 'http://aura-vllm-chat:8000/v1');
    await deleteSetting('AURA_MODEL_CONTEXT_WINDOW');

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/settings/AURA_LLM_BASE_URL', {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({ value: 'http://aura-vllm-chat:8000/v1' }),
    });
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/settings/AURA_MODEL_CONTEXT_WINDOW', {
      method: 'DELETE',
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  });

  it('writes one model profile mutation as a batch', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response('{"ok":true}', { status: 200 })));
    vi.stubGlobal('fetch', fetchMock);
    const settings = {
      AURA_LLM_PROVIDER: 'llamacpp',
      AURA_LLM_BASE_URL: 'http://aura-llm:8084/v1',
      AURA_LLM_MODEL: 'gemma-4-12b',
    };

    await putLLMProfile(settings);

    expect(fetchMock).toHaveBeenCalledWith('/api/settings/llm-profile', {
      method: 'PUT',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({ settings }),
    });
  });

  it('checks Telegram availability and mints a QR link with same-origin credentials', async () => {
    const fetchMock = vi.fn((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (url.includes('/status')) {
        return Promise.resolve(new Response('{"linked":true}', { status: 200 }));
      }
      if (url.endsWith('/link')) {
        return Promise.resolve(
          new Response(
            '{"sessionToken":"sess-1","deepLink":"https://t.me/AuraBot?start=x","qrSvg":"<svg/>"}',
            { status: 200 },
          ),
        );
      }
      return Promise.resolve(
        new Response('{"configured":true,"available":true,"botUsername":"AuraBot"}', {
          status: 200,
        }),
      );
    });
    vi.stubGlobal('fetch', fetchMock);

    await checkTelegramAvailability('123:secret');
    await createTelegramLink();
    await fetchTelegramLinkStatus('sess-1');

    expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/settings/telegram/check', {
      method: 'POST',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify({ token: '123:secret' }),
    });
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/settings/telegram/link', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
    expect(fetchMock).toHaveBeenNthCalledWith(3, '/api/settings/telegram/sess-1/status', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  });

  it('reads the per-provider route memory', async () => {
    const routes = [
      {
        provider: 'llamacpp',
        base_url: 'http://host.docker.internal:8084/v1',
        model: 'gemma-4-12b',
      },
    ];
    const fetchMock = vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify({ routes }), { status: 200 })),
    );
    vi.stubGlobal('fetch', fetchMock);

    await expect(fetchLLMRoutes()).resolves.toEqual(routes);
    expect(fetchMock).toHaveBeenCalledWith('/api/settings/llm-routes', {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
    });
  });

  it('treats a daemon without the route memory as having none, not as an error', async () => {
    // A daemon older than migration 0117 answers 404/503 here. The panel then falls back
    // to its built-in routes; it must not surface a load failure over a missing feature.
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('nope', { status: 404 }))),
    );
    await expect(fetchLLMRoutes()).resolves.toEqual([]);
  });

  it('reads an empty route memory without inventing rows', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('{}', { status: 200 }))),
    );
    await expect(fetchLLMRoutes()).resolves.toEqual([]);
  });

  it('probes the route in the form for the models it publishes', async () => {
    const models = [{ id: 'gemma-4-12b', context_window: 131072, has_price: false }];
    const fetchMock = vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify({ models }), { status: 200 })),
    );
    vi.stubGlobal('fetch', fetchMock);

    await expect(
      fetchLLMModels('llamacpp', 'http://host.docker.internal:8084/v1'),
    ).resolves.toEqual(models);
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/settings/llm-models?provider=llamacpp&base_url=http%3A%2F%2Fhost.docker.internal%3A8084%2Fv1',
      { headers: { Accept: 'application/json' }, credentials: 'same-origin' },
    );
  });

  it('carries the server reason when a probe is refused', async () => {
    // "GET /models returned 401" is what tells the operator to fix the key rather than
    // the host; collapsing it to a status code throws that away.
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({ error: 'model catalog unavailable: GET /models returned 401' }),
            {
              status: 502,
            },
          ),
        ),
      ),
    );
    await expect(fetchLLMModels('openrouter', 'https://openrouter.ai/api/v1')).rejects.toThrow(
      /401/,
    );
  });

  it('reads a catalogue with no models as empty', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(new Response('{}', { status: 200 }))),
    );
    await expect(fetchLLMModels('ollama', 'http://host.docker.internal:11434/v1')).resolves.toEqual(
      [],
    );
  });
});
