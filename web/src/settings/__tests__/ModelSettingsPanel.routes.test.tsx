import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';
import { resolveProvider } from '../modelSettingsDefs';

const SETTINGS_BODY = {
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
    {
      key: 'AURA_LLM_BASE_URL',
      label: 'Primary LLM base URL',
      kind: 'string',
      secret: false,
      value: 'https://openrouter.ai/api/v1',
      has_value: true,
      overridden: false,
    },
    {
      key: 'OPENROUTER_API_KEY',
      label: 'OpenRouter API key',
      kind: 'string',
      secret: true,
      value: 'sk-should-never-render',
      has_value: true,
      overridden: true,
    },
  ],
};

describe('ModelSettingsPanel routes', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it.each([
    ['ollama', 'https://ignored.invalid/v1', 'ollama'],
    ['llamacpp', 'https://ignored.invalid/v1', 'local'],
    ['openrouter', 'http://localhost:11434/v1', 'cloud'],
    ['', 'http://host.docker.internal:11434/v1', 'ollama'],
    ['', 'http://ollama/v1', 'ollama'],
    ['', 'http://aura-llm:8084/v1', 'local'],
    ['', '', 'cloud'],
    ['', 'not a URL', 'local'],
  ] as const)('resolves provider %s at %s as %s', (stored, baseURL, expected) => {
    expect(resolveProvider(stored, baseURL)).toBe(expected);
  });

  it('edits model routing without rendering secret values', async () => {
    const calls: { url: string; init: RequestInit | undefined }[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        calls.push({ url, init });
        if (init?.method === 'PUT') {
          return Promise.resolve(new Response('{"ok":true}', { status: 200 }));
        }
        return Promise.resolve(
          new Response(JSON.stringify(SETTINGS_BODY), {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          }),
        );
      }),
    );

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <ModelSettingsPanel onComplete={vi.fn()} />
      </QueryClientProvider>,
    );

    expect(await screen.findByRole('heading', { name: 'Model routing' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Cloud' }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.queryByDisplayValue('sk-should-never-render')).toBeNull();
    expect(screen.queryByText('sk-should-never-render')).toBeNull();
    expect(screen.getAllByText('Configured').length).toBeGreaterThan(0);

    fireEvent.click(screen.getByRole('button', { name: 'Local' }));
    fireEvent.change(screen.getByLabelText('Primary model'), {
      target: { value: 'qwen2.5-coder:14b' },
    });
    fireEvent.change(screen.getByLabelText('Primary base URL'), {
      target: { value: 'http://aura-vllm-chat:8000/v1' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    await waitFor(() => {
      expect(calls.some((call) => call.url === '/api/settings/llm-profile')).toBe(true);
    });
    const profileCall = calls.find((call) => call.url === '/api/settings/llm-profile');
    const profileBody = profileCall?.init?.body;
    expect(typeof profileBody).toBe('string');
    expect(JSON.parse(typeof profileBody === 'string' ? profileBody : '{}')).toEqual({
      settings: {
        AURA_LLM_PROVIDER: 'llamacpp',
        AURA_LLM_BASE_URL: 'http://aura-vllm-chat:8000/v1',
        AURA_LLM_MODEL: 'qwen2.5-coder:14b',
      },
    });
  });
});
