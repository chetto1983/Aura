import { afterEach, describe, expect, it, vi } from 'vitest';
import { STT_ROUTE, TTS_ROUTE, synthesizeSpeech, transcribeAudio } from './voiceApi';
import { stubObjectURL, ttsResponse, type ObjectUrlStub } from './voiceMocks';

let urls: ObjectUrlStub | undefined;

afterEach(() => {
  urls?.restore();
  urls = undefined;
  vi.unstubAllGlobals();
});

function stubFetch(response: Response | Error): ReturnType<typeof vi.fn<typeof fetch>> {
  const mock = vi.fn<typeof fetch>(() =>
    response instanceof Error ? Promise.reject(response) : Promise.resolve(response),
  );
  vi.stubGlobal('fetch', mock);
  return mock;
}

describe('transcribeAudio', () => {
  it('POSTs the clip as the `audio` multipart part the Go handler reads', async () => {
    const fetchMock = stubFetch(Response.json({ text: 'ciao mondo' }));

    await expect(
      transcribeAudio(new Blob(['x'], { type: 'audio/webm' }), 'voice-turn'),
    ).resolves.toBe('ciao mondo');

    const [route, init] = fetchMock.mock.calls[0] ?? [];
    expect(route).toBe(STT_ROUTE);
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    const form = init?.body as FormData;
    const part = form.get('audio');
    expect(part).toBeInstanceOf(File);
    expect((part as File).name).toBe('voice-turn');
  });

  it('a silent clip is a clean empty transcript, not a failure', async () => {
    stubFetch(Response.json({ text: '' }));
    await expect(transcribeAudio(new Blob(['x']))).resolves.toBe('');
  });

  it('a non-string text field reads as empty rather than leaking the shape', async () => {
    stubFetch(Response.json({ text: 42 }));
    await expect(transcribeAudio(new Blob(['x']))).resolves.toBe('');
  });

  it('throws on a non-2xx so the caller can show its own dead end', async () => {
    stubFetch(new Response('nope', { status: 502 }));
    await expect(transcribeAudio(new Blob(['x']))).rejects.toThrow('502');
  });
});

describe('synthesizeSpeech', () => {
  it('POSTs the text as JSON and wraps the mp3 body in an object URL', async () => {
    urls = stubObjectURL();
    const fetchMock = stubFetch(ttsResponse());

    const spoken = await synthesizeSpeech('buongiorno');

    const [route, init] = fetchMock.mock.calls[0] ?? [];
    expect(route).toBe(TTS_ROUTE);
    expect(init?.body).toBe(JSON.stringify({ text: 'buongiorno' }));
    expect(spoken.url).toMatch(/^blob:/);
    expect(spoken.truncated).toBe(false);
    expect(urls.createObjectURL).toHaveBeenCalledTimes(1);
  });

  it('reports the backend truncation header', async () => {
    urls = stubObjectURL();
    stubFetch(ttsResponse({ truncated: true }));
    await expect(synthesizeSpeech('a very long answer')).resolves.toMatchObject({
      truncated: true,
    });
  });

  it('throws on a non-2xx and creates no URL to leak', async () => {
    urls = stubObjectURL();
    stubFetch(new Response('nope', { status: 503 }));
    await expect(synthesizeSpeech('x')).rejects.toThrow('503');
    expect(urls.createObjectURL).not.toHaveBeenCalled();
  });
});
