import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';

// The cloud speech-to-text and text-to-speech fields pick from OpenRouter's lists the way
// the image and video fields do: same combobox, same saved-route rule.

function setting(key: string, value: string) {
  return {
    key,
    label: key,
    kind: 'string',
    secret: false,
    value,
    has_value: value !== '',
    overridden: true,
    applied: 'restart',
  };
}

function settingsBody(provider: string, baseURL: string, stt = '', tts = '') {
  return {
    restart_required: false,
    restart_keys: [],
    settings: [
      setting('AURA_LLM_PROVIDER', provider),
      setting('AURA_LLM_BASE_URL', baseURL),
      setting('AURA_STT_CLOUD_MODEL', stt),
      setting('AURA_TTS_MODEL', tts),
    ],
  };
}

const TRANSCRIPTION_BODY = {
  models: [
    { kind: 'transcription', id: 'microsoft/mai-transcribe-2', has_price: false },
    { kind: 'transcription', id: 'qwen/qwen3-asr-1.7b', has_price: false },
  ],
};
const SPEECH_BODY = {
  models: [{ kind: 'speech', id: 'microsoft/mai-voice-2-flash', has_price: false }],
};

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

interface SettingWrite {
  readonly key: string;
  readonly value: string;
}

function stubFetch(settings: unknown): {
  readonly gets: string[];
  readonly writes: SettingWrite[];
} {
  const gets: string[] = [];
  const writes: SettingWrite[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (init?.method === 'PUT') {
        if (typeof init.body !== 'string') throw new Error('expected a JSON string body');
        const body = JSON.parse(init.body) as { readonly value: string };
        writes.push({ key: decodeURIComponent(url.split('/').at(-1) ?? ''), value: body.value });
        return Promise.resolve(json({}));
      }
      if (url.startsWith('/api/settings/transcription-models')) {
        gets.push(url);
        return Promise.resolve(json(TRANSCRIPTION_BODY));
      }
      if (url.startsWith('/api/settings/speech-models')) {
        gets.push(url);
        return Promise.resolve(json(SPEECH_BODY));
      }
      if (url.startsWith('/api/settings/'))
        return Promise.resolve(json({ models: [], routes: [] }));
      return Promise.resolve(json(settings));
    }),
  );
  return { gets, writes };
}

function renderBackends() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ModelSettingsPanel groups={['backends']} />
    </QueryClientProvider>,
  );
}

function fieldCard(label: string): HTMLElement {
  const card = screen.getByText(label).closest('div.rounded-md');
  if (!(card instanceof HTMLElement)) throw new Error(`no field card for ${label}`);
  return card;
}

describe('ModelSettingsPanel voice models', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('offers the OpenRouter speech-to-text and text-to-speech lists on the Cloud route', async () => {
    const { gets } = stubFetch(settingsBody('openrouter', 'https://openrouter.ai/api/v1'));
    renderBackends();

    const stt = await screen.findByLabelText('Speech-to-text cloud model');
    await waitFor(() => {
      expect(
        within(fieldCard('Speech-to-text cloud model')).getByText(/2 models published here/),
      ).toBeTruthy();
    });
    await waitFor(() => {
      expect(
        within(fieldCard('Text-to-speech model')).getByText(/1 model published here/),
      ).toBeTruthy();
    });
    expect(gets).toEqual(['/api/settings/transcription-models', '/api/settings/speech-models']);

    fireEvent.click(stt);
    expect(screen.getByRole('option', { name: /qwen\/qwen3-asr-1\.7b/ })).toBeTruthy();
  });

  it('clears either cloud voice model through the local-sidecar choice', async () => {
    const { writes } = stubFetch(
      settingsBody(
        'openrouter',
        'https://openrouter.ai/api/v1',
        'google/chirp-3',
        'qwen/qwen-audio-3.0-tts-flash',
      ),
    );
    renderBackends();

    const stt = await screen.findByLabelText('Speech-to-text cloud model');
    await waitFor(() => {
      expect(
        within(fieldCard('Speech-to-text cloud model')).getByText(/2 models published here/),
      ).toBeTruthy();
    });
    fireEvent.click(stt);
    fireEvent.click(screen.getByRole('option', { name: /Use local sidecar/ }));
    expect(stt.textContent).toContain('Use local sidecar');

    const tts = screen.getByLabelText('Text-to-speech model');
    fireEvent.click(tts);
    fireEvent.click(screen.getByRole('option', { name: /Use local sidecar/ }));
    expect(tts.textContent).toContain('Use local sidecar');

    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));
    await waitFor(() => {
      expect(writes).toEqual([
        { key: 'AURA_STT_CLOUD_MODEL', value: '' },
        { key: 'AURA_TTS_MODEL', value: '' },
      ]);
    });
  });

  it('asks for no voice catalogue on a local route', async () => {
    const { gets } = stubFetch(settingsBody('llamacpp', 'http://aura-llm:8084/v1'));
    renderBackends();

    await screen.findByLabelText('Speech-to-text cloud model');
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(gets).toEqual([]);
  });
});
