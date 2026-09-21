import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';

// The backends pane's cloud model fields -- speech-to-text, text-to-speech and embeddings --
// pick from OpenRouter's lists the way the image and video fields do: same combobox, same
// saved-route rule, same "Use local sidecar" for the empty choice.

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

function settingsBody(
  provider: string,
  baseURL: string,
  stt = '',
  tts = '',
  ttsVoice = '',
  embed = '',
  embedCloudBaseURL = '',
) {
  return {
    restart_required: false,
    restart_keys: [],
    settings: [
      setting('AURA_LLM_PROVIDER', provider),
      setting('AURA_LLM_BASE_URL', baseURL),
      setting('AURA_STT_CLOUD_MODEL', stt),
      setting('AURA_TTS_MODEL', tts),
      setting('AURA_TTS_CLOUD_VOICE', ttsVoice),
      setting('AURA_EMBED_MODEL', embed),
      setting('AURA_EMBED_CLOUD_BASE_URL', embedCloudBaseURL),
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
  models: [
    {
      kind: 'speech',
      id: 'microsoft/mai-voice-2-flash',
      voices: ['en-US-Harper:MAI-Voice-2'],
      has_price: false,
    },
    {
      kind: 'speech',
      id: 'qwen/qwen-audio-3.0-tts-flash',
      voices: ['loongjohn', 'longanhuan_v3.6'],
      has_price: false,
    },
  ],
};

const EMBEDDINGS_BODY = {
  models: [
    { kind: 'embeddings', id: 'liquid/lfm-2.5-embedding-350m:free', has_price: false },
    { kind: 'embeddings', id: 'qwen/qwen3-embedding-8b', has_price: false },
  ],
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
      if (url.startsWith('/api/settings/embeddings-models')) {
        gets.push(url);
        return Promise.resolve(json(EMBEDDINGS_BODY));
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

describe('ModelSettingsPanel backend models', () => {
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
        within(fieldCard('Text-to-speech model')).getByText(/2 models published here/),
      ).toBeTruthy();
    });
    expect(gets).toEqual([
      '/api/settings/transcription-models',
      '/api/settings/speech-models',
      '/api/settings/embeddings-models',
    ]);

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
        'longanhuan_v3.6',
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
        { key: 'AURA_TTS_CLOUD_VOICE', value: '' },
      ]);
    });
  });

  it('selects a supported OpenRouter voice with the TTS model and saves both', async () => {
    const { writes } = stubFetch(settingsBody('openrouter', 'https://openrouter.ai/api/v1'));
    renderBackends();

    const tts = await screen.findByLabelText('Text-to-speech model');
    await waitFor(() => {
      expect(
        within(fieldCard('Text-to-speech model')).getByText(/2 models published here/),
      ).toBeTruthy();
    });
    fireEvent.click(tts);
    fireEvent.click(screen.getByRole('option', { name: /qwen\/qwen-audio-3\.0-tts-flash/ }));

    const voice = screen.getByLabelText('Text-to-speech cloud voice');
    expect(voice.textContent).toContain('loongjohn');
    fireEvent.click(voice);
    fireEvent.click(screen.getByRole('option', { name: /longanhuan_v3\.6/ }));

    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));
    await waitFor(() => {
      expect(writes).toEqual([
        { key: 'AURA_TTS_MODEL', value: 'qwen/qwen-audio-3.0-tts-flash' },
        { key: 'AURA_TTS_CLOUD_VOICE', value: 'longanhuan_v3.6' },
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

  // The embedding field used to be a free-text box, which is how a cloud model name reached
  // the LOCAL sidecar: llama.cpp ignores the model and answers with its own vectors, so the
  // typo never surfaced. Picking from the published list removes the typo, and the empty
  // choice is the local sidecar rather than a blank the operator has to interpret.
  it('picks the embedding model from the OpenRouter list and clears it to the local sidecar', async () => {
    const { writes } = stubFetch(
      settingsBody(
        'openrouter',
        'https://openrouter.ai/api/v1',
        '',
        '',
        '',
        'qwen/qwen3-embedding-8b',
      ),
    );
    renderBackends();

    const embed = await screen.findByLabelText('Embedding model');
    await waitFor(() => {
      expect(
        within(fieldCard('Embedding model')).getByText(/2 models published here/),
      ).toBeTruthy();
    });
    fireEvent.click(embed);
    expect(
      screen.getByRole('option', { name: /liquid\/lfm-2\.5-embedding-350m:free/ }),
    ).toBeTruthy();

    fireEvent.click(screen.getByRole('option', { name: /Use local sidecar/ }));
    expect(await screen.findByLabelText('Embedding base URL')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));
    await waitFor(() => {
      expect(writes).toEqual([{ key: 'AURA_EMBED_MODEL', value: '' }]);
    });
  });

  it('uses a manual cloud endpoint without leaving its /v1 suffix to double up', async () => {
    const { writes } = stubFetch(
      settingsBody(
        'openrouter',
        'https://openrouter.ai/api/v1',
        '',
        '',
        '',
        'qwen/qwen3-embedding-8b',
      ),
    );
    renderBackends();
    await screen.findByRole('button', { name: 'Manual endpoint' });
    fireEvent.click(screen.getByRole('button', { name: 'Manual endpoint' }));

    const base = screen.getByLabelText('Embedding cloud base URL');
    expect(base.getAttribute('aria-invalid')).toBe('true');
    expect(base.getAttribute('aria-describedby')).toBe('embedding-manual-url-error');
    expect(screen.getByRole('button', { name: 'Save runtime settings' }).hasAttribute('disabled')).toBe(
      true,
    );

    fireEvent.change(base, { target: { value: 'https://embed.example/v1/' } });
    expect(base.hasAttribute('aria-invalid')).toBe(false);
    expect(base.hasAttribute('aria-describedby')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    await waitFor(() => {
      expect(writes).toEqual([
        { key: 'AURA_EMBED_CLOUD_BASE_URL', value: 'https://embed.example' },
      ]);
    });
  });

  it('clears the cloud route values when returning to the local sidecar', async () => {
    const { writes } = stubFetch(
      settingsBody(
        'openrouter',
        'https://openrouter.ai/api/v1',
        '',
        '',
        '',
        'qwen/qwen3-embedding-8b',
        'https://embed.example',
      ),
    );
    renderBackends();
    await screen.findByRole('button', { name: 'Local' });
    fireEvent.click(screen.getByRole('button', { name: 'Local' }));
    expect(await screen.findByLabelText('Embedding base URL')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    await waitFor(() => {
      expect(writes).toEqual([
        { key: 'AURA_EMBED_CLOUD_BASE_URL', value: '' },
        { key: 'AURA_EMBED_MODEL', value: '' },
      ]);
    });
  });
});
