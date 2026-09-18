import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { vi } from 'vitest';
import '../../i18n/i18n';
import StudioWorkspace from '../StudioWorkspace';
import { mediaQueryList } from '../../test/mediaQuery';

// studioPageHarness — the fixtures and the stubbed server the Studio page's two test files
// share. Not a test file itself: vitest collects only *.test.*, and keeping the harness out
// of both keeps each under the LOC cap without duplicating a catalog in two places.

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

/** A finished image that named a reference, so Reuse has an id to resolve. */
export const WITH_INPUTS = {
  id: 'job-inputs',
  kind: 'image',
  status: 'completed',
  model: 'openai/gpt-image-1-mini',
  prompt: 'a reference-driven still',
  used: { aspect_ratio: '1:1', reference_asset_ids: ['asset-ref'] },
  cost_usd: 0.05,
  asset_id: 'asset-out',
  created_at: '2026-09-17T12:00:00Z',
};

/** The same, with nothing to resolve. */
export const NO_INPUTS = { ...WITH_INPUTS, id: 'job-bare', used: { aspect_ratio: '1:1' } };

export interface ServerOptions {
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
  /** Holds GET /api/studio/library open, so the pre-resolution window is observable. */
  readonly libraryGate?: Promise<void>;
  readonly libraryFails?: boolean;
  /** The identity's images, for resolving a reused record's asset ids. */
  readonly library?: readonly unknown[];
  readonly historyFails?: boolean;
  readonly historyGate?: Promise<void>;
}

export interface Call {
  readonly url: string;
  readonly method: string;
  readonly body: unknown;
}

export function urlOf(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input;
  if (input instanceof URL) return input.href;
  return input.url;
}

export function stubServer(options: ServerOptions = {}) {
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
      if (url.startsWith('/api/studio/history')) {
        if (options.historyFails === true) {
          return json({ code: '', error: 'the history could not be read' }, 500);
        }
        const page = json({ records: options.history ?? [] });
        return options.historyGate === undefined
          ? page
          : options.historyGate.then(async () => page);
      }
      if (url.startsWith('/api/studio/library')) {
        if (options.libraryFails === true) {
          return json({ code: '', error: 'the library could not be read' }, 500);
        }
        const page = json({ assets: options.library ?? LIBRARY.assets });
        return options.libraryGate === undefined
          ? page
          : options.libraryGate.then(async () => page);
      }
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

export function mountPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <StudioWorkspace />
    </QueryClientProvider>,
  );
}

export function prompt(): HTMLElement {
  return screen.getByRole('textbox', { name: 'Prompt' });
}

export async function openedOnVideo() {
  await screen.findByPlaceholderText('Describe the video scene you want to generate');
}

export function posts(calls: readonly Call[]): readonly Call[] {
  return calls.filter((call) => call.method === 'POST');
}

/** jsdom has no layout, so the width the history panel reads IS this stub. The page tests
 *  run side by side; the drawer at narrow widths is StudioHistory's own test. */
export function viewport(sideBySide: boolean) {
  window.matchMedia = (query: string) => mediaQueryList(query, sideBySide);
}
