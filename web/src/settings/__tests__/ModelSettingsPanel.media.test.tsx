import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import i18n from '../../i18n/i18n';
import { ModelSettingsPanel } from '../ModelSettingsPanel';
import type { ModelSettingsGroup } from '../modelSettingsDefs';

function setting(key: string, value: string) {
  return {
    key,
    label: key,
    kind: 'string',
    secret: false,
    value,
    has_value: value !== '',
    overridden: true,
    applied: 'live',
  };
}

function settingsBody(provider: string, baseURL: string) {
  return {
    restart_required: false,
    restart_keys: [],
    settings: [
      setting('AURA_LLM_PROVIDER', provider),
      setting('AURA_LLM_BASE_URL', baseURL),
      setting('AURA_LLM_MODEL', 'z-ai/glm-5.3'),
      setting('AURA_IMAGE_MODEL', 'microsoft/mai-image-2.6'),
      setting('AURA_VIDEO_MODEL', 'minimax/hailuo-3-max'),
    ],
  };
}

const IMAGE_BODY = {
  models: [
    { kind: 'image', id: 'microsoft/mai-image-2.6', has_price: false, reference_max: 5 },
    {
      kind: 'image',
      id: 'black-forest-labs/flux-3-pro',
      has_price: true,
      image_min_usd: 0.04,
      image_max_usd: 0.04,
    },
  ],
};

const VIDEO_BODY = {
  models: [
    {
      kind: 'video',
      id: 'minimax/hailuo-3-max',
      has_price: true,
      second_min_usd: 0.05,
      second_max_usd: 0.08,
      duration_min: 5,
      duration_max: 15,
      resolutions: ['768p', '480p'],
      image_to_video: true,
    },
    { kind: 'video', id: 'google/veo-4', has_price: false, duration_min: 8, duration_max: 8 },
  ],
};

interface Call {
  readonly method: string;
  readonly url: string;
  readonly body: string | undefined;
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function stubFetch(settings: unknown, imageAnswer: () => Response = () => json(IMAGE_BODY)) {
  const calls: Call[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      const method = init?.method ?? 'GET';
      calls.push({ method, url, body: typeof init?.body === 'string' ? init.body : undefined });
      if (method === 'PUT') return Promise.resolve(json(setting('AURA_IMAGE_MODEL', 'x')));
      if (url.startsWith('/api/settings/image-models')) return Promise.resolve(imageAnswer());
      if (url.startsWith('/api/settings/video-models')) return Promise.resolve(json(VIDEO_BODY));
      if (url.startsWith('/api/settings/llm-'))
        return Promise.resolve(json({ models: [], routes: [] }));
      return Promise.resolve(json(settings));
    }),
  );
  return calls;
}

function renderPanel(groups: readonly ModelSettingsGroup[] = ['routing']) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <ModelSettingsPanel groups={groups} />
    </QueryClientProvider>,
  );
}

function mediaGets(calls: readonly Call[]): readonly string[] {
  return calls
    .filter((call) => /\/api\/settings\/(image|video)-models/.test(call.url))
    .map((call) => call.url);
}

function fieldCard(label: string): HTMLElement {
  const card = screen.getByText(label).closest('div.rounded-md');
  if (!(card instanceof HTMLElement)) throw new Error(`no field card for ${label}`);
  return card;
}

describe('ModelSettingsPanel media models', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    void i18n.changeLanguage('en');
  });

  it('offers the image and video catalogues on the Cloud route with their prices and capabilities', async () => {
    const calls = stubFetch(settingsBody('openrouter', 'https://openrouter.ai/api/v1'));
    renderPanel();

    const image = await screen.findByLabelText('Image generation model');
    await waitFor(() => {
      expect(
        within(fieldCard('Image generation model')).getByText(/2 models published here/),
      ).toBeTruthy();
    });
    expect(mediaGets(calls)).toEqual(['/api/settings/image-models', '/api/settings/video-models']);

    fireEvent.click(image);
    expect(screen.getByRole('option', { name: /microsoft\/mai-image-2\.6/ }).textContent).toContain(
      'up to 5 reference images',
    );
    expect(screen.getByRole('option', { name: /flux-3-pro/ }).textContent).toContain('$0.04/image');
  });

  it('writes a video row with its per-second range, one duration and image-to-video', async () => {
    stubFetch(settingsBody('openrouter', 'https://openrouter.ai/api/v1'));
    renderPanel();

    const video = await screen.findByLabelText('Video generation model');
    await waitFor(() => {
      expect(
        within(fieldCard('Video generation model')).getByText(/2 models published here/),
      ).toBeTruthy();
    });
    fireEvent.click(video);
    expect(screen.getByRole('option', { name: /hailuo-3-max/ }).textContent).toContain(
      '$0.05–$0.08/s · 5–15 s · 768p, 480p · image-to-video',
    );
    expect(screen.getByRole('option', { name: /veo-4/ }).textContent).toContain('8 s');
  });

  it('forces a catalogue refresh from the field’s Refresh button', async () => {
    const calls = stubFetch(settingsBody('openrouter', 'https://openrouter.ai/api/v1'));
    renderPanel();
    await screen.findByLabelText('Image generation model');
    await waitFor(() => {
      expect(
        within(fieldCard('Image generation model')).getByText(/2 models published here/),
      ).toBeTruthy();
    });

    fireEvent.click(
      within(fieldCard('Image generation model')).getByRole('button', { name: 'Refresh' }),
    );

    await waitFor(() => {
      expect(mediaGets(calls)).toContain('/api/settings/image-models?refresh=1');
    });
    expect(
      mediaGets(calls).filter((url) => url.startsWith('/api/settings/video-models')),
    ).toHaveLength(1);
  });

  it('hides the media rows and asks for no catalogue on a local route', async () => {
    const calls = stubFetch(settingsBody('llamacpp', 'http://aura-llm:8084/v1'));
    renderPanel();

    await screen.findByLabelText('Primary model');
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.queryByLabelText('Image generation model')).toBeNull();
    expect(screen.queryByLabelText('Video generation model')).toBeNull();
    expect(mediaGets(calls)).toEqual([]);
  });

  it('asks for no media catalogue from a pane that does not show the routing fields', async () => {
    const calls = stubFetch(settingsBody('openrouter', 'https://openrouter.ai/api/v1'));
    renderPanel(['tokens']);

    await screen.findByRole('heading', { name: 'Token and turn budget' });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(mediaGets(calls)).toEqual([]);
  });

  it('still saves a typed image model when the catalogue cannot be read', async () => {
    const calls = stubFetch(settingsBody('openrouter', 'https://openrouter.ai/api/v1'), () =>
      json({ error: 'media model catalog unavailable: GET images/models: 503' }, 502),
    );
    renderPanel();

    const image = await screen.findByLabelText('Image generation model');
    await waitFor(() => {
      expect(
        within(fieldCard('Image generation model')).getByText(/GET images\/models: 503/),
      ).toBeTruthy();
    });

    fireEvent.click(image);
    fireEvent.change(screen.getByPlaceholderText('Search models...'), {
      target: { value: 'vendor/private-image' },
    });
    fireEvent.click(screen.getByText(/Use "vendor\/private-image" as typed/));
    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    await waitFor(() => {
      expect(calls.filter((call) => call.method === 'PUT')).toEqual([
        {
          method: 'PUT',
          url: '/api/settings/AURA_IMAGE_MODEL',
          body: JSON.stringify({ value: 'vendor/private-image' }),
        },
      ]);
    });
  });

  it('lists the media models as soon as the Cloud route is saved, with no Refresh', async () => {
    // The daemon answers from the SAVED route: while it is local both catalogues refuse.
    let saved = settingsBody('llamacpp', 'http://aura-llm:8084/v1');
    const calls: Call[] = [];
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
        const url =
          typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
        const method = init?.method ?? 'GET';
        calls.push({ method, url, body: typeof init?.body === 'string' ? init.body : undefined });
        if (method === 'PUT') {
          saved = settingsBody('openrouter', 'https://openrouter.ai/api/v1');
          return Promise.resolve(json({ updated: 3, restart_required: false }));
        }
        const refused = json(
          { error: 'image and video models are listed only on the OpenRouter route' },
          409,
        );
        const local = saved.settings[0]?.value === 'llamacpp';
        if (url.startsWith('/api/settings/image-models'))
          return Promise.resolve(local ? refused : json(IMAGE_BODY));
        if (url.startsWith('/api/settings/video-models'))
          return Promise.resolve(local ? refused : json(VIDEO_BODY));
        if (url.startsWith('/api/settings/llm-'))
          return Promise.resolve(json({ models: [], routes: [] }));
        return Promise.resolve(json(saved));
      }),
    );
    renderPanel();

    await screen.findByLabelText('Primary model');
    fireEvent.click(screen.getByRole('button', { name: 'Cloud' }));
    await screen.findByLabelText('Image generation model');
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(mediaGets(calls)).toEqual([]);
    expect(screen.queryByText(/only on the OpenRouter route/)).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Save runtime settings' }));

    await waitFor(() => {
      expect(
        within(fieldCard('Image generation model')).getByText(/2 models published here/),
      ).toBeTruthy();
    });
    await waitFor(() => {
      expect(
        within(fieldCard('Video generation model')).getByText(/2 models published here/),
      ).toBeTruthy();
    });
    expect(mediaGets(calls)).toEqual(['/api/settings/image-models', '/api/settings/video-models']);
    expect(screen.queryByText(/only on the OpenRouter route/)).toBeNull();
  });

  it('labels an unsaved media model as applying immediately', async () => {
    const body = settingsBody('openrouter', 'https://openrouter.ai/api/v1');
    stubFetch({
      ...body,
      settings: body.settings.filter((row) => !/^AURA_(IMAGE|VIDEO)_MODEL$/.test(row.key)),
    });
    renderPanel();

    await screen.findByLabelText('Image generation model');
    expect(
      within(fieldCard('Image generation model')).getByText('Applies immediately'),
    ).toBeTruthy();
    expect(
      within(fieldCard('Video generation model')).getByText('Applies immediately'),
    ).toBeTruthy();
  });

  it('labels the media rows in Italian', async () => {
    stubFetch(settingsBody('openrouter', 'https://openrouter.ai/api/v1'));
    await i18n.changeLanguage('it');
    renderPanel();

    const video = await screen.findByLabelText('Modello per generare video');
    expect(screen.getByLabelText('Modello per generare immagini')).toBeTruthy();
    await waitFor(() => {
      expect(
        within(fieldCard('Modello per generare video')).getByText(/2 modelli pubblicati qui/),
      ).toBeTruthy();
    });
    fireEvent.click(video);
    expect(screen.getByRole('option', { name: /hailuo-3-max/ }).textContent).toContain(
      'da immagine a video',
    );
    expect(i18n.t('settings.models.references', { count: 1 })).toBe(
      'fino a 1 immagine di riferimento',
    );
    expect(i18n.t('settings.models.references', { count: 5 })).toBe(
      'fino a 5 immagini di riferimento',
    );
  });
});
