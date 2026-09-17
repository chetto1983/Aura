import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Mock } from 'vitest';
import {
  StudioError,
  assetDownloadUrl,
  assetStreamUrl,
  createStudioImage,
  createStudioVideo,
  fetchStudioModels,
  finalizeStudioUpload,
  listStudioHistory,
  listStudioLibrary,
} from '../studioApi';

// The shapes here are the ones internal/agui/studio_dto.go declares — `default` (not
// `default_model`), a `{records:[…]}` history envelope, a `{assets:[…]}` library envelope,
// and a `{code, error}` refusal. A client that matched the plan's prose instead would talk
// to a server that does not exist, so each assertion pins the Go tag rather than the prose.

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function stubFetch(...responses: readonly Response[]): Mock {
  const queue = [...responses];
  const fetchMock = vi.fn(() => Promise.resolve(queue.shift() ?? jsonResponse({})));
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

function callOf(fetchMock: Mock, index = 0): { readonly url: string; readonly init: RequestInit } {
  const call = fetchMock.mock.calls[index] as [string, RequestInit] | undefined;
  if (call === undefined) throw new Error(`fetch was not called ${String(index + 1)} time(s)`);
  return { url: call[0], init: call[1] };
}

function sentBody(init: RequestInit): unknown {
  return JSON.parse(init.body as string);
}

async function refusalOf(call: Promise<unknown>): Promise<StudioError> {
  const err: unknown = await call.then(
    () => undefined,
    (reason: unknown) => reason,
  );
  if (!(err instanceof StudioError)) throw new Error(`expected a StudioError, got ${String(err)}`);
  return err;
}

const videoBody = { model: 'google/veo-3.1-lite', prompt: 'a cat in a hat' } as const;

const record = {
  id: 'job-1',
  kind: 'video',
  status: 'pending',
  model: 'google/veo-3.1-lite',
  prompt: 'a cat in a hat',
  used: { duration: 4, resolution: '720p' },
  created_at: '2026-09-17T10:00:00Z',
};

describe('studio API client', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('reads the models envelope by the key the server writes', async () => {
    const fetchMock = stubFetch(
      jsonResponse({
        default: 'google/veo-3.1-lite',
        models: [{ id: 'google/veo-3.1-lite', audio: true, seed: true }],
      }),
    );

    const out = await fetchStudioModels('video');

    const { url, init } = callOf(fetchMock);
    expect(url).toBe('/api/studio/models?kind=video');
    expect(init.method).toBe('GET');
    expect(init.credentials).toBe('same-origin');
    expect('signal' in init).toBe(false);
    // `default`, not `default_model`: studio_dto.go tags it `json:"default"`.
    expect(out.default).toBe('google/veo-3.1-lite');
    expect(out.models.map((model) => model.id)).toEqual(['google/veo-3.1-lite']);
  });

  it('carries an abort signal only when the caller gives one', async () => {
    const fetchMock = stubFetch(jsonResponse({ default: '', models: [] }));
    const controller = new AbortController();

    await fetchStudioModels('image', controller.signal);

    const { url, init } = callOf(fetchMock);
    expect(url).toBe('/api/studio/models?kind=image');
    expect(init.signal).toBe(controller.signal);
  });

  it('asks history for one page and names the cursor and kind only when it has them', async () => {
    const fetchMock = stubFetch(
      jsonResponse({ records: [record] }),
      jsonResponse({ records: [] }),
      jsonResponse({ records: [] }),
    );

    const rows = await listStudioHistory(undefined, undefined);

    expect(callOf(fetchMock).url).toBe('/api/studio/history?limit=24');
    // The records array, not the envelope: a caller mapping over the envelope gets nothing.
    expect(rows.map((row) => row.id)).toEqual(['job-1']);

    await listStudioHistory('video', 'job-9');
    expect(callOf(fetchMock, 1).url).toBe('/api/studio/history?limit=24&kind=video&before=job-9');

    // A cursor is server data, so it is escaped rather than pasted: an unescaped '&' would
    // forge a second query parameter.
    await listStudioHistory(undefined, 'a b&kind=image');
    expect(callOf(fetchMock, 2).url).toBe('/api/studio/history?limit=24&before=a+b%26kind%3Dimage');
  });

  it('reads the library envelope', async () => {
    const fetchMock = stubFetch(
      jsonResponse({
        assets: [{ id: 'asset-1', file_name: 'cat.png', mime_type: 'image/png' }],
      }),
    );

    const refs = await listStudioLibrary();

    expect(callOf(fetchMock).url).toBe('/api/studio/library');
    expect(refs.map((ref) => ref.file_name)).toEqual(['cat.png']);
  });

  it('posts a video body the server can strict-decode', async () => {
    const fetchMock = stubFetch(jsonResponse(record, 201));

    const created = await createStudioVideo({
      model: 'google/veo-3.1-lite',
      prompt: 'a cat in a hat',
      duration: 4,
      resolution: '720p',
      aspect_ratio: '16:9',
      first_frame_asset_id: 'asset-first',
    });

    const { url, init } = callOf(fetchMock);
    expect(url).toBe('/api/studio/videos');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
    expect(init.headers).toMatchObject({ 'Content-Type': 'application/json' });
    // Exact: the route refuses a body carrying a field it does not declare.
    expect(sentBody(init)).toEqual({
      model: 'google/veo-3.1-lite',
      prompt: 'a cat in a hat',
      duration: 4,
      resolution: '720p',
      aspect_ratio: '16:9',
      first_frame_asset_id: 'asset-first',
    });
    expect(created.id).toBe('job-1');
  });

  it('posts an image body to the image route', async () => {
    const fetchMock = stubFetch(jsonResponse({ ...record, kind: 'image' }, 201));

    const created = await createStudioImage({
      model: 'google/gemini-2.5-flash-image',
      prompt: 'a hat on a cat',
      aspect_ratio: '1:1',
      reference_asset_ids: ['ref-1'],
    });

    const { url, init } = callOf(fetchMock);
    expect(url).toBe('/api/studio/images');
    expect(sentBody(init)).toEqual({
      model: 'google/gemini-2.5-flash-image',
      prompt: 'a hat on a cat',
      aspect_ratio: '1:1',
      reference_asset_ids: ['ref-1'],
    });
    expect(created.kind).toBe('image');
  });

  it('finalizes an upload by an escaped id', async () => {
    const fetchMock = stubFetch(
      jsonResponse({ id: 'a/b', file_name: 'dog.png', mime_type: 'image/png' }),
    );

    const ref = await finalizeStudioUpload('a/b');

    const { url, init } = callOf(fetchMock);
    expect(url).toBe('/api/studio/uploads/a%2Fb/finalize');
    expect(init.method).toBe('POST');
    expect(ref.file_name).toBe('dog.png');
  });

  it('turns a refusal into its status, code and sentence', async () => {
    stubFetch(jsonResponse({ code: 'unsupported', error: 'That model takes no end frame.' }, 422));

    const err = await refusalOf(createStudioVideo(videoBody));

    expect(err.status).toBe(422);
    // `code` holds the code and `error` holds the sentence — the reverse of the plan's prose.
    expect(err.code).toBe('unsupported');
    expect(err.message).toBe('That model takes no end frame.');
  });

  it.each([
    { status: 409, code: 'no_key', error: 'No OpenRouter key.' },
    { status: 402, code: 'no_credit', error: 'Out of credit.' },
    { status: 502, code: 'outcome_unknown', error: 'The outcome is unknown.' },
    { status: 422, code: 'content_blocked', error: 'That prompt was refused.' },
  ])('carries the $code refusal as $status', async ({ status, code, error }) => {
    stubFetch(jsonResponse({ code, error }, status));

    const err = await refusalOf(createStudioImage({ model: 'm', prompt: 'p' }));

    expect(err.status).toBe(status);
    expect(err.code).toBe(code);
    expect(err.message).toBe(error);
  });

  it('leaves the bare status when the failure is not JSON', async () => {
    // The unwired Studio and the auth gate answer http.Error text, not a studioErrorDTO.
    stubFetch(new Response('studio unavailable', { status: 503 }));

    const err = await refusalOf(listStudioLibrary());

    expect(err.status).toBe(503);
    expect(err.code).toBe('');
    expect(err.message).toBe('HTTP 503');
  });

  it('points at the owned asset routes with an escaped id', () => {
    expect(assetDownloadUrl('a/b')).toBe('/api/assets/a%2Fb/download');
    expect(assetStreamUrl('a/b')).toBe('/api/assets/a%2Fb/stream');
  });
});
