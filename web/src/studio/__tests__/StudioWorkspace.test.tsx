import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '../../i18n/i18n';
import StudioWorkspace from '../StudioWorkspace';

// The page, against a stubbed server. Every assertion here is about the whole loop an
// operator runs: what the catalog decides the bar opens on, what the pills read, what the
// exact body posted is, and what is said when the server refuses.

const VIDEO_MODELS = {
  default: 'google/veo-3.1-lite',
  models: [
    {
      id: 'google/veo-3.1-lite',
      name: 'Veo 3.1 Lite',
      description: 'Fast clips with optional sound',
      durations: [4, 8],
      resolutions: ['720p', '1080p'],
      aspect_ratios: ['16:9', '9:16'],
      frame_images: ['first_frame'],
      audio: true,
      seed: true,
      prices: [
        { resolution: '720p', audio: false, usd_per_second: 0.0297 },
        { resolution: '720p', audio: true, usd_per_second: 0.05 },
        { resolution: '1080p', audio: false, usd_per_second: 0.08 },
      ],
    },
    {
      id: 'minimax/hailuo-2.3',
      name: 'Hailuo 2.3',
      description: 'Longer takes',
      durations: [6],
      resolutions: ['768p'],
      aspect_ratios: ['16:9'],
      audio: false,
      seed: false,
      prices: [{ resolution: '768p', audio: false, usd_per_second: 0.05 }],
    },
  ],
};

const IMAGE_MODELS = {
  default: 'openai/gpt-image-1-mini',
  models: [
    {
      id: 'openai/gpt-image-1-mini',
      name: 'GPT Image 1 mini',
      description: 'Cheap stills',
      aspect_ratios: ['3:2', '1:1'],
      audio: false,
      seed: false,
      reference_max: 2,
      image_min_usd: 0.05,
      image_max_usd: 0.05,
    },
  ],
};

const LIBRARY = {
  assets: [{ id: 'asset-ref', file_name: 'moodboard.png', mime_type: 'image/png' }],
};

interface ServerOptions {
  readonly history?: readonly unknown[];
  readonly modelsFailure?: {
    readonly status: number;
    readonly code: string;
    readonly error: string;
  };
  readonly createFailure?: {
    readonly status: number;
    readonly code: string;
    readonly error: string;
  };
  /** Holds the create route open, so the in-flight window is observable. */
  readonly createGate?: Promise<void>;
  /** A catalog that answers 200 and lists nothing for the kind. */
  readonly emptyCatalog?: boolean;
}

interface Call {
  readonly url: string;
  readonly method: string;
  readonly body: unknown;
}

function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

function stubServer(options: ServerOptions = {}) {
  const calls: Call[] = [];
  const json = (body: unknown, status = 200) =>
    Promise.resolve(new Response(JSON.stringify(body), { status }));

  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = urlOf(input);
      const method = init?.method ?? 'GET';
      calls.push({
        url,
        method,
        body: typeof init?.body === 'string' ? JSON.parse(init.body) : undefined,
      });
      if (url.startsWith('/api/studio/models')) {
        if (options.modelsFailure !== undefined) {
          const { status, ...payload } = options.modelsFailure;
          return json(payload, status);
        }
        if (options.emptyCatalog === true) return json({ default: '', models: [] });
        return json(url.includes('kind=image') ? IMAGE_MODELS : VIDEO_MODELS);
      }
      if (url.startsWith('/api/studio/history')) return json({ records: options.history ?? [] });
      if (url.startsWith('/api/studio/library')) return json(LIBRARY);
      if (url.startsWith('/api/studio/videos') || url.startsWith('/api/studio/images')) {
        if (options.createFailure !== undefined) {
          const { status, ...payload } = options.createFailure;
          return json(payload, status);
        }
        const accepted = json(
          {
            id: 'job-new',
            kind: url.includes('images') ? 'image' : 'video',
            status: 'pending',
            model: 'google/veo-3.1-lite',
            prompt: 'a harbour at dawn',
            used: {},
            created_at: '2026-09-17T10:00:00Z',
          },
          201,
        );
        return options.createGate === undefined
          ? accepted
          : options.createGate.then(async () => accepted);
      }
      return Promise.reject(new Error(`unexpected fetch: ${url}`));
    }),
  );
  return calls;
}

function mountPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <StudioWorkspace />
    </QueryClientProvider>,
  );
}

function prompt(): HTMLElement {
  return screen.getByRole('textbox', { name: 'Prompt' });
}

async function openedOnVideo() {
  await screen.findByPlaceholderText('Describe the shot');
}

function posts(calls: readonly Call[]): readonly Call[] {
  return calls.filter((call) => call.method === 'POST');
}

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('StudioWorkspace', () => {
  it('opens on the headline, the deployment video model and its cheapest estimate', async () => {
    stubServer();
    mountPage();
    await openedOnVideo();

    expect(screen.getByRole('heading', { name: 'Bring your idea to life' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain('Veo 3.1 Lite');
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('16:9 · 720p · 4s');
    // 0.0297/s silent at 720p over the shortest declared clip.
    expect(screen.getByText('≈ $0.12')).toBeTruthy();
  });

  it('keeps the prompt across a mode switch and reprices with the image catalog', async () => {
    stubServer();
    mountPage();
    await openedOnVideo();
    fireEvent.change(prompt(), { target: { value: 'a harbour at dawn' } });

    fireEvent.click(screen.getByRole('radio', { name: 'Image' }));
    await screen.findByPlaceholderText('Describe the image');

    expect((prompt() as HTMLTextAreaElement).value).toBe('a harbour at dawn');
    expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain(
      'GPT Image 1 mini',
    );
    // The image model declares no duration and neither sound nor seed.
    expect(screen.getByRole('button', { name: 'Options' }).textContent).toBe('3:2');
    expect(screen.queryByRole('button', { name: 'Advanced' })).toBeNull();
    expect(screen.getByText('≈ $0.05')).toBeTruthy();
  });

  it('posts one video with exactly the options the pills read, and refetches the history', async () => {
    const calls = stubServer();
    mountPage();
    await openedOnVideo();
    fireEvent.change(prompt(), { target: { value: 'a harbour at dawn' } });
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });

    await waitFor(() => {
      expect(posts(calls)).toHaveLength(1);
    });
    const post = posts(calls)[0];
    expect(post?.url).toBe('/api/studio/videos');
    expect(post?.body).toEqual({
      model: 'google/veo-3.1-lite',
      prompt: 'a harbour at dawn',
      duration: 4,
      resolution: '720p',
      aspect_ratio: '16:9',
      audio: false,
    });

    // The accepted record makes every history list stale, so the panel re-reads.
    await waitFor(() => {
      expect(
        calls.filter((call) => call.url.startsWith('/api/studio/history')).length,
      ).toBeGreaterThan(1);
    });
  });

  it('holds Generate shut while the request is in flight, so a clip is not bought twice', async () => {
    let release = (): void => undefined;
    const gate = new Promise<void>((resolve) => {
      release = resolve;
    });
    const calls = stubServer({ createGate: gate });
    mountPage();
    await openedOnVideo();
    fireEvent.change(prompt(), { target: { value: 'a harbour at dawn' } });
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });

    const sending = await screen.findByRole('button', { name: /Sending/ });
    expect(sending.hasAttribute('disabled')).toBe(true);
    // The shortcut is shut too, not merely the button: the same request must not be sent
    // twice while the first one is still open.
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });
    expect(posts(calls)).toHaveLength(1);

    release();
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Generate/ }).hasAttribute('disabled')).toBe(false);
    });
    expect(posts(calls)).toHaveLength(1);
  });

  it('posts an image with the reference it was given', async () => {
    const calls = stubServer();
    mountPage();
    await openedOnVideo();
    fireEvent.click(screen.getByRole('radio', { name: 'Image' }));
    await screen.findByPlaceholderText('Describe the image');

    // Attach a reference from the identity's own library.
    fireEvent.keyDown(screen.getByRole('button', { name: 'References' }), { key: 'Enter' });
    fireEvent.click(screen.getByRole('menuitem', { name: 'Your images' }));
    fireEvent.click(await screen.findByRole('button', { name: 'moodboard.png' }));

    fireEvent.change(prompt(), { target: { value: 'a quiet room' } });
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });

    await waitFor(() => {
      expect(posts(calls)).toHaveLength(1);
    });
    expect(posts(calls)[0]?.url).toBe('/api/studio/images');
    expect(posts(calls)[0]?.body).toEqual({
      model: 'openai/gpt-image-1-mini',
      prompt: 'a quiet room',
      aspect_ratio: '3:2',
      reference_asset_ids: ['asset-ref'],
    });
  });

  it('shows the localized sentence when the server refuses the generation', async () => {
    stubServer({
      createFailure: { status: 409, code: 'no_key', error: 'openrouter credential missing' },
    });
    mountPage();
    await openedOnVideo();
    fireEvent.change(prompt(), { target: { value: 'a harbour at dawn' } });
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('This deployment has no OpenRouter key');
    // The server's own wording is not what an operator is shown for a code Aura knows.
    expect(alert.textContent).not.toContain('credential missing');
  });

  it('posts nothing at all while the prompt is empty', async () => {
    const calls = stubServer();
    mountPage();
    await openedOnVideo();

    expect(screen.getByRole('button', { name: /Generate/ }).hasAttribute('disabled')).toBe(true);
    fireEvent.keyDown(prompt(), { key: 'Enter', ctrlKey: true });
    fireEvent.click(screen.getByRole('button', { name: /Generate/ }));
    await waitFor(() => {
      expect(screen.getByText('≈ $0.12')).toBeTruthy();
    });
    expect(posts(calls)).toHaveLength(0);
  });

  it('remembers the chosen model per kind, and survives a localStorage that throws', async () => {
    stubServer();
    const first = mountPage();
    await openedOnVideo();
    fireEvent.click(screen.getByRole('combobox', { name: 'Model' }));
    fireEvent.click(screen.getByRole('option', { name: /Hailuo 2.3/ }));
    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain('Hailuo 2.3');
    });
    expect(localStorage.getItem('aura.studio.model.video')).toBe('minimax/hailuo-2.3');
    first.unmount();

    const second = mountPage();
    await openedOnVideo();
    // The remembered model, not the deployment default.
    expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain('Hailuo 2.3');
    second.unmount();

    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    mountPage();
    await openedOnVideo();
    fireEvent.click(screen.getByRole('combobox', { name: 'Model' }));
    fireEvent.click(screen.getByRole('option', { name: /Hailuo 2.3/ }));
    await waitFor(() => {
      expect(screen.getByRole('combobox', { name: 'Model' }).textContent).toContain('Hailuo 2.3');
    });
    vi.restoreAllMocks();
  });

  it('explains a deployment the Studio cannot run on, and offers no bar', async () => {
    stubServer({
      modelsFailure: {
        status: 409,
        code: 'local_route',
        error: 'the deployment routes models locally',
      },
    });
    mountPage();

    expect(await screen.findByText(/routes its models locally/)).toBeTruthy();
    expect(screen.queryByRole('textbox', { name: 'Prompt' })).toBeNull();
    expect(screen.queryByRole('button', { name: /Generate/ })).toBeNull();
  });

  it('says a catalog with no model for this kind is empty, rather than showing a bare page', async () => {
    stubServer({ emptyCatalog: true });
    mountPage();

    expect(await screen.findByText('No model is available for this kind.')).toBeTruthy();
    expect(screen.queryByRole('textbox', { name: 'Prompt' })).toBeNull();
  });

  it('shows the newest generation by default and follows a card that is clicked', async () => {
    stubServer({
      history: [
        {
          id: 'job-new',
          kind: 'image',
          status: 'completed',
          model: 'openai/gpt-image-1-mini',
          prompt: 'the newest',
          used: { aspect_ratio: '1:1' },
          cost_usd: 0.05,
          asset_id: 'asset-new',
          created_at: '2026-09-17T12:00:00Z',
        },
        {
          id: 'job-old',
          kind: 'video',
          status: 'failed',
          model: 'google/veo-3.1-lite',
          prompt: 'the older one',
          used: {},
          created_at: '2026-09-17T09:00:00Z',
          error: { code: 'no_credit', message: 'provider answered 402' },
        },
      ],
    });
    mountPage();
    await openedOnVideo();

    // The newest row is on the stage without anything being clicked.
    expect(screen.getByRole('link', { name: /Download/ }).getAttribute('href')).toBe(
      '/api/assets/asset-new/download',
    );
    fireEvent.click(screen.getByRole('button', { name: /the older one/ }));
    expect(screen.getByRole('alert').textContent).toContain(
      'The OpenRouter account is out of credit.',
    );
    expect(screen.queryByRole('link', { name: /Download/ })).toBeNull();
  });
});
