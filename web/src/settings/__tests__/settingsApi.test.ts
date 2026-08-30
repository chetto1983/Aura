import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  checkTelegramAvailability,
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
});
