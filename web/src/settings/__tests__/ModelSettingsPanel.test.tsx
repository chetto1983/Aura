import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';

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
    {
      key: 'AURA_LLM_MAX_TOKENS',
      label: 'Max response tokens',
      kind: 'int',
      secret: false,
      value: '4096',
      has_value: true,
      overridden: false,
    },
    {
      key: 'AURA_MODEL_CONTEXT_WINDOW',
      label: 'Context window tokens',
      kind: 'int',
      secret: false,
      value: '1000000',
      has_value: true,
      overridden: false,
    },
    {
      key: 'AURA_MODEL_MAX_OUTPUT_TOKENS',
      label: 'Reserved output tokens',
      kind: 'int',
      secret: false,
      value: '32768',
      has_value: true,
      overridden: false,
    },
  ],
};

describe('ModelSettingsPanel', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('edits cloud/local model routing without rendering secret values', async () => {
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

    render(<ModelSettingsPanel onComplete={vi.fn()} />);

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
      expect(calls.some((call) => call.url === '/api/settings/AURA_LLM_BASE_URL')).toBe(true);
      expect(calls.some((call) => call.url === '/api/settings/AURA_LLM_MODEL')).toBe(true);
    });
    const baseURLCall = calls.find((call) => call.url === '/api/settings/AURA_LLM_BASE_URL');
    expect(baseURLCall?.init?.body).toBe(
      JSON.stringify({ value: 'http://aura-vllm-chat:8000/v1' }),
    );
  });
});
