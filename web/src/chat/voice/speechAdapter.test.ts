import { afterEach, describe, expect, it, vi } from 'vitest';
import { createSpeechAdapter } from './speechAdapter';
import {
  stubAudio,
  stubObjectURL,
  mockTtsFetch,
  ttsResponse,
  type AudioStub,
  type ObjectUrlStub,
  type StubAudioOptions,
} from './voiceMocks';

// Drain the adapter's async speak() pipeline (fetch → blob → objectURL → play).
async function tick(): Promise<void> {
  await new Promise<void>((resolve) => {
    setTimeout(resolve, 0);
  });
}

describe('createSpeechAdapter', () => {
  let audioStub: AudioStub;
  let urlStub: ObjectUrlStub;

  afterEach(() => {
    vi.unstubAllGlobals();
    urlStub.restore();
  });

  function setup(fetchMock: typeof fetch, options?: StubAudioOptions) {
    audioStub = stubAudio(options);
    urlStub = stubObjectURL();
    vi.stubGlobal('fetch', fetchMock);
    return createSpeechAdapter();
  }

  it('speak → running, then ended(finished) after Audio.onended', async () => {
    const adapter = setup(mockTtsFetch());
    const utterance = adapter.speak('hello');
    const seen: string[] = [];
    const unsub = utterance.subscribe(() => seen.push(utterance.status.type));

    expect(utterance.status.type).toBe('running');
    await tick();
    expect(audioStub.last()?.play).toHaveBeenCalledTimes(1);

    audioStub.last()?.fireEnded();
    expect(utterance.status.type).toBe('ended');
    if (utterance.status.type === 'ended') expect(utterance.status.reason).toBe('finished');
    expect(seen.at(-1)).toBe('ended');
    unsub();
  });

  it('a repeat speak(sameText) issues no second fetch — per-text blob cache (D-03)', async () => {
    const fetchMock = mockTtsFetch();
    const adapter = setup(fetchMock);

    adapter.speak('hello');
    await tick();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(urlStub.createObjectURL).toHaveBeenCalledTimes(1);

    const second = adapter.speak('hello');
    await tick();
    expect(fetchMock).toHaveBeenCalledTimes(1); // served from cache
    expect(urlStub.createObjectURL).toHaveBeenCalledTimes(1);
    expect(second.status.type).toBe('running');
    expect(audioStub.instances).toHaveLength(2); // a fresh <audio> off the cached url
  });

  it('cancel() → ended(cancelled) and pauses the audio', async () => {
    const adapter = setup(mockTtsFetch());
    const utterance = adapter.speak('hello');
    await tick();

    utterance.cancel();
    expect(audioStub.last()?.pause).toHaveBeenCalledTimes(1);
    expect(utterance.status.type).toBe('ended');
    if (utterance.status.type === 'ended') expect(utterance.status.reason).toBe('cancelled');
  });

  it('a 4xx /api/tts → ended(error)', async () => {
    const adapter = setup(mockTtsFetch({ status: 400 }));
    const utterance = adapter.speak('hello');
    await tick();
    expect(utterance.status.type).toBe('ended');
    if (utterance.status.type === 'ended') expect(utterance.status.reason).toBe('error');
  });

  it('an Audio playback error → ended(error)', async () => {
    const adapter = setup(mockTtsFetch());
    const utterance = adapter.speak('hello');
    await tick();
    audioStub.last()?.fireError();
    expect(utterance.status.type).toBe('ended');
    if (utterance.status.type === 'ended') expect(utterance.status.reason).toBe('error');
  });

  it('a rejected Audio.play() (autoplay blocked) → ended(error)', async () => {
    const adapter = setup(mockTtsFetch(), { playError: new Error('NotAllowedError') });
    const utterance = adapter.speak('hi');
    await tick();
    expect(utterance.status.type).toBe('ended');
    if (utterance.status.type === 'ended') expect(utterance.status.reason).toBe('error');
  });

  it('exposes truncated===true when X-Aura-TTS-Truncated:true (D-05 flag)', async () => {
    const utterance = setup(mockTtsFetch({ truncated: true })).speak('a very long report');
    await tick();
    expect(utterance.truncated).toBe(true);
  });

  it('exposes truncated===false when the header is absent', async () => {
    const utterance = setup(mockTtsFetch()).speak('short');
    await tick();
    expect(utterance.truncated).toBe(false);
  });

  it('dispose() revokes every cached object URL and clears the cache (Landmine #5)', async () => {
    const fetchMock = mockTtsFetch();
    const adapter = setup(fetchMock);
    adapter.speak('a');
    await tick();
    adapter.speak('b');
    await tick();
    expect(urlStub.createObjectURL).toHaveBeenCalledTimes(2);

    adapter.dispose();
    expect(urlStub.revokeObjectURL).toHaveBeenCalledTimes(2);

    // Cache cleared → the same text re-fetches after dispose.
    adapter.speak('a');
    await tick();
    expect(fetchMock).toHaveBeenCalledTimes(3);
  });

  it('cancel() before the blob resolves ends cancelled and never plays', async () => {
    let resolveFetch: ((res: Response) => void) | undefined;
    const fetchMock = vi.fn<typeof fetch>(
      () =>
        new Promise<Response>((resolve) => {
          resolveFetch = resolve;
        }),
    );
    const adapter = setup(fetchMock);

    const utterance = adapter.speak('slow');
    utterance.cancel(); // audio not created yet
    expect(utterance.status.type).toBe('ended');
    if (utterance.status.type === 'ended') expect(utterance.status.reason).toBe('cancelled');

    resolveFetch?.(ttsResponse());
    await tick();
    // The blob is still cached (paid for) but no playback started after the cancel.
    expect(audioStub.instances).toHaveLength(0);
    expect(urlStub.createObjectURL).toHaveBeenCalledTimes(1);
  });

  it('POSTs the message text to /api/tts with same-origin credentials + Accept audio/mpeg', async () => {
    const fetchMock = mockTtsFetch();
    const adapter = setup(fetchMock);
    adapter.speak('read this back to me');
    await tick();

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('/api/tts');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
    expect(JSON.parse(init.body as string)).toEqual({ text: 'read this back to me' });
    const headers = init.headers as Record<string, string>;
    expect(headers.Accept).toBe('audio/mpeg');
    expect(headers['Content-Type']).toBe('application/json');
  });

  it('caches the truncated flag — a repeat speak reports truncated without re-fetching', async () => {
    const fetchMock = mockTtsFetch({ truncated: true });
    const adapter = setup(fetchMock);

    const first = adapter.speak('a very long report');
    await tick();
    expect(first.truncated).toBe(true);

    const second = adapter.speak('a very long report');
    await tick();
    expect(fetchMock).toHaveBeenCalledTimes(1); // served from cache, no second synth
    // The truncated flag must ride through the cache entry, not just the live fetch.
    expect(second.truncated).toBe(true);
    expect(second.status.type).toBe('running');
  });
});
