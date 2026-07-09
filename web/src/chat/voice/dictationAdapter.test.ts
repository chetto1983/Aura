import { afterEach, describe, expect, it, vi, type Mock } from 'vitest';
import type { DictationAdapter } from '@assistant-ui/react';
import { createDictationAdapter } from './dictationAdapter';
import { stubGetUserMedia, stubMediaRecorder, type GetUserMediaStub } from './voiceMocks';

// Drain the adapter's async pipeline (getUserMedia → MediaRecorder, or onstop → POST).
async function tick(): Promise<void> {
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 0);
  });
}

// A single POST /api/stt response — the STT JSON route answers {text}. voiceMocks ships the
// MediaRecorder/getUserMedia doubles (reused here); the STT fetch shape is local to this lane.
function mockSttFetch(options: { text?: string; status?: number } = {}): Mock<typeof fetch> {
  return vi.fn<typeof fetch>(() =>
    Promise.resolve(
      new Response(JSON.stringify({ text: options.text ?? 'hello world' }), {
        status: options.status ?? 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  );
}

describe('createDictationAdapter', () => {
  let media: GetUserMediaStub;

  afterEach(() => {
    media.restore();
    vi.unstubAllGlobals();
  });

  function setup(fetchMock: typeof fetch): DictationAdapter {
    stubMediaRecorder();
    media = stubGetUserMedia();
    vi.stubGlobal('fetch', fetchMock);
    return createDictationAdapter();
  }

  it('disables the input during dictation (brief record window, no interim stream)', () => {
    const adapter = setup(mockSttFetch());
    expect(adapter.disableInputDuringDictation).toBe(true);
  });

  it('records → POSTs the blob to /api/stt → inserts via onSpeech(isFinal:true), THEN onSpeechEnd', async () => {
    const fetchMock = mockSttFetch({ text: 'hello world' });
    const adapter = setup(fetchMock);
    const events: string[] = [];
    const speechResults: DictationAdapter.Result[] = [];

    const session = adapter.listen();
    session.onSpeechStart(() => events.push('start'));
    session.onSpeech((result) => {
      events.push('speech');
      speechResults.push(result);
    });
    session.onSpeechEnd(() => events.push('end'));

    await tick();
    expect(session.status).toEqual({ type: 'running' });
    expect(events).toContain('start');

    await session.stop();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/stt');
    expect(init.method).toBe('POST');
    expect(init.body).toBeInstanceOf(FormData);
    // The transcript is inserted via onSpeech(isFinal:true) — the ONLY path the core writes.
    expect(speechResults).toEqual([{ transcript: 'hello world', isFinal: true }]);
    // onSpeech (insert) MUST fire before onSpeechEnd (cleanup) — RESEARCH Landmine #1.
    expect(events.indexOf('speech')).toBeLessThan(events.indexOf('end'));
    expect(session.status).toEqual({ type: 'ended', reason: 'stopped' });
  });

  it('an empty transcript inserts nothing and still ends clean (reason:stopped)', async () => {
    const adapter = setup(mockSttFetch({ text: '' }));
    const onSpeech = vi.fn();
    const onSpeechEnd = vi.fn();

    const session = adapter.listen();
    session.onSpeech(onSpeech);
    session.onSpeechEnd(onSpeechEnd);

    await tick();
    await session.stop();

    expect(onSpeech).not.toHaveBeenCalled(); // nothing to insert
    expect(onSpeechEnd).toHaveBeenCalledWith({ transcript: '' }); // cleanup still fires
    expect(session.status).toEqual({ type: 'ended', reason: 'stopped' });
  });

  it('a non-ok /api/stt ends the session reason:error with NO onSpeech (Composer degrades)', async () => {
    const adapter = setup(mockSttFetch({ status: 400 }));
    const onSpeech = vi.fn();
    const onSpeechEnd = vi.fn();

    const session = adapter.listen();
    session.onSpeech(onSpeech);
    session.onSpeechEnd(onSpeechEnd);

    await tick();
    await session.stop();

    expect(onSpeech).not.toHaveBeenCalled();
    expect(onSpeechEnd).not.toHaveBeenCalled();
    expect(session.status).toEqual({ type: 'ended', reason: 'error' });
  });

  it('cancel() stops the tracks and ends cancelled without POSTing', async () => {
    const fetchMock = mockSttFetch();
    const adapter = setup(fetchMock);

    const session = adapter.listen();
    await tick();
    session.cancel();

    expect(media.trackStop).toHaveBeenCalled();
    expect(session.status).toEqual({ type: 'ended', reason: 'cancelled' });

    await tick();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // stubCustomMedia installs a caller-supplied getUserMedia (reject / deferred) and, like
  // stubGetUserMedia, assigns `media` so the shared afterEach restores navigator.mediaDevices.
  function stubCustomMedia(
    getUserMedia: Mock<(constraints?: MediaStreamConstraints) => Promise<MediaStream>>,
    trackStop: Mock<() => void> = vi.fn(),
  ): void {
    stubMediaRecorder();
    const descriptor = Object.getOwnPropertyDescriptor(navigator, 'mediaDevices');
    Object.defineProperty(navigator, 'mediaDevices', { configurable: true, value: { getUserMedia } });
    media = {
      getUserMedia,
      trackStop,
      restore: () => {
        if (descriptor === undefined) Reflect.deleteProperty(navigator, 'mediaDevices');
        else Object.defineProperty(navigator, 'mediaDevices', descriptor);
      },
    };
  }

  it('the session starts in status {type:starting} before the mic opens', async () => {
    const adapter = setup(mockSttFetch());
    const session = adapter.listen();
    // Synchronous, before the async getUserMedia resolves — the exact starting seam.
    expect(session.status).toEqual({ type: 'starting' });
    await tick();
    await session.stop(); // drain the pipeline so no work escapes the test
  });

  it('a getUserMedia rejection ends the session error and still settles stop() (no hang)', async () => {
    stubCustomMedia(vi.fn(() => Promise.reject(new Error('mic denied'))));
    vi.stubGlobal('fetch', mockSttFetch());
    const adapter = createDictationAdapter();

    const session = adapter.listen();
    await tick();
    expect(session.status).toEqual({ type: 'ended', reason: 'error' });
    // The catch fires settle(), so the composer's `await session.stop()` never hangs.
    await expect(session.stop()).resolves.toBeUndefined();
  });

  it('cancel before the mic opens ends cancelled, stops the late stream, and never POSTs', async () => {
    const trackStop = vi.fn();
    const stream = { getTracks: () => [{ stop: trackStop }] } as unknown as MediaStream;
    let resolveGum: (s: MediaStream) => void = () => undefined;
    const getUserMedia = vi.fn<(constraints?: MediaStreamConstraints) => Promise<MediaStream>>(
      () =>
        new Promise<MediaStream>((resolve) => {
          resolveGum = resolve;
        }),
    );
    stubCustomMedia(getUserMedia, trackStop);
    const fetchMock = mockSttFetch();
    vi.stubGlobal('fetch', fetchMock);
    const adapter = createDictationAdapter();

    const session = adapter.listen();
    session.cancel(); // the mic has not opened yet (recorder undefined)
    expect(session.status).toEqual({ type: 'ended', reason: 'cancelled' });

    resolveGum(stream); // getUserMedia now resolves — the cancelled guard stops the late tracks
    await tick();
    expect(trackStop).toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('a fetch that throws (network error) ends the session error with no onSpeech', async () => {
    const adapter = setup(vi.fn<typeof fetch>(() => Promise.reject(new Error('network down'))));
    const onSpeech = vi.fn();
    const session = adapter.listen();
    session.onSpeech(onSpeech);

    await tick();
    await session.stop();

    expect(onSpeech).not.toHaveBeenCalled();
    expect(session.status).toEqual({ type: 'ended', reason: 'error' });
  });

  it('a non-string /api/stt text inserts nothing and ends clean (stopped)', async () => {
    const fetchMock = vi.fn<typeof fetch>(() =>
      Promise.resolve(
        new Response(JSON.stringify({ text: 42 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    );
    const adapter = setup(fetchMock);
    const onSpeech = vi.fn();
    const session = adapter.listen();
    session.onSpeech(onSpeech);

    await tick();
    await session.stop();

    expect(onSpeech).not.toHaveBeenCalled(); // a numeric text coerces to '' → no insert
    expect(session.status).toEqual({ type: 'ended', reason: 'stopped' });
  });

  it('the on* subscriptions return working unsubscribes (the callback is removed)', async () => {
    const adapter = setup(mockSttFetch({ text: 'hello world' }));
    const start = vi.fn();
    const speech = vi.fn();
    const end = vi.fn();

    const session = adapter.listen();
    session.onSpeechStart(start)(); // subscribe, then immediately unsubscribe
    session.onSpeech(speech)();
    session.onSpeechEnd(end)();

    await tick();
    await session.stop();

    // A no-op unsubscribe would leave these subscribed and fire them — assert they did NOT.
    expect(start).not.toHaveBeenCalled();
    expect(speech).not.toHaveBeenCalled();
    expect(end).not.toHaveBeenCalled();
  });
});
