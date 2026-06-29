import { afterEach, describe, expect, it, vi } from 'vitest';
import { deleteSetting, fetchSettings, putSetting } from '../settingsApi';

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
});
